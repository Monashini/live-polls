package redisstore

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// votersField holds the ballot count. Option counts live alongside it as
// "o:0", "o:1", ... in the same hash, so one round trip reads the whole tally.
const votersField = "voters"

func optionField(index int) string { return "o:" + strconv.Itoa(index) }

// Tally is the shape both Redis and MongoDB agree on.
type Tally struct {
	// Counts is selections per option, indexed to match the poll's options.
	Counts []int64
	// Voters is ballots. In multiple-choice mode it is less than the sum of
	// Counts, because one person can select several options.
	Voters int64
}

// incrScript adds one vote to an existing counter hash.
//
// It is a Lua script rather than a pipeline for one reason: it has to decide
// whether the key exists and act on that decision atomically. A pipeline of
// EXISTS-then-HINCRBY could have the key expire in between, and the HINCRBY
// would then create a fresh hash containing only this single vote -- silently
// resetting a poll's results to 1.
//
// Returning an empty table when the key is absent is the signal to the caller
// that it must reseed from MongoDB. An existing hash always has the voters
// field, so empty is never ambiguous.
//
// ARGV[1]   = number of option fields to bump
// ARGV[2..] = those field names, then the TTL in milliseconds
var incrScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {}
end

local n = tonumber(ARGV[1])
for i = 1, n do
  redis.call('HINCRBY', KEYS[1], ARGV[1 + i], 1)
end
redis.call('HINCRBY', KEYS[1], '` + votersField + `', 1)
redis.call('PEXPIRE', KEYS[1], ARGV[n + 2])

return redis.call('HGETALL', KEYS[1])
`)

// seedScript replaces a poll's counter hash wholesale.
//
// DEL before HSET rather than plain HSET, so that stale option fields cannot
// survive: if a poll's option list ever shrank, an HSET-only seed would leave
// counts for options that no longer exist. Both run inside the script, so no
// reader ever observes the empty window between them.
//
// ARGV[1]   = TTL in milliseconds
// ARGV[2..] = alternating field, value
var seedScript = redis.NewScript(`
redis.call('DEL', KEYS[1])
for i = 2, #ARGV, 2 do
  redis.call('HSET', KEYS[1], ARGV[i], ARGV[i + 1])
end
redis.call('PEXPIRE', KEYS[1], ARGV[1])
return 1
`)

// IncrementVote applies one ballot to the live counters.
//
// The second return value is false when the counter hash was not in Redis --
// evicted, expired, or the server restarted. The caller must then seed from
// MongoDB rather than trusting a partial count.
func (s *Store) IncrementVote(ctx context.Context, pollID string, optionIndexes []int, optionCount int) (*Tally, bool, error) {
	args := make([]any, 0, len(optionIndexes)+2)
	args = append(args, len(optionIndexes))
	for _, idx := range optionIndexes {
		args = append(args, optionField(idx))
	}
	args = append(args, s.countsTTL.Milliseconds())

	raw, err := incrScript.Run(ctx, s.client, []string{countsKey(pollID)}, args...).Result()
	if err != nil {
		return nil, false, fmt.Errorf("redis: increment vote: %w", err)
	}

	pairs, ok := raw.([]any)
	if !ok || len(pairs) == 0 {
		// Empty reply: the hash is gone and the caller must reseed.
		return nil, false, nil
	}

	return parseTally(pairs, optionCount), true, nil
}

// SeedTally writes an authoritative tally into Redis, replacing whatever was
// there. The values come from MongoDB, which is the source of truth.
func (s *Store) SeedTally(ctx context.Context, pollID string, tally Tally) error {
	args := make([]any, 0, len(tally.Counts)*2+3)
	args = append(args, s.countsTTL.Milliseconds())

	for i, count := range tally.Counts {
		args = append(args, optionField(i), count)
	}
	args = append(args, votersField, tally.Voters)

	if err := seedScript.Run(ctx, s.client, []string{countsKey(pollID)}, args...).Err(); err != nil {
		return fmt.Errorf("redis: seed tally: %w", err)
	}
	return nil
}

// GetTally reads the live counters. The boolean is false on a cache miss,
// which is not an error -- it just means the caller should fall back to
// MongoDB and reseed.
func (s *Store) GetTally(ctx context.Context, pollID string, optionCount int) (*Tally, bool, error) {
	values, err := s.client.HGetAll(ctx, countsKey(pollID)).Result()
	if err != nil {
		return nil, false, fmt.Errorf("redis: get tally: %w", err)
	}
	if len(values) == 0 {
		return nil, false, nil
	}

	tally := &Tally{Counts: make([]int64, optionCount)}
	for field, raw := range values {
		assignField(tally, field, raw, optionCount)
	}

	// Reading is the most common operation on a popular poll, so each read
	// pushes the expiry out. Polls nobody is watching fall out of memory;
	// polls under active use stay hot.
	s.client.PExpire(ctx, countsKey(pollID), s.countsTTL)

	return tally, true, nil
}

// DropTally removes a poll's counters, used when the poll itself is deleted so
// Redis does not hold results for something that no longer exists.
func (s *Store) DropTally(ctx context.Context, pollID string) error {
	return s.client.Del(ctx, countsKey(pollID)).Err()
}

// parseTally converts a flat HGETALL reply (field, value, field, value, ...)
// into a Tally.
func parseTally(pairs []any, optionCount int) *Tally {
	tally := &Tally{Counts: make([]int64, optionCount)}

	for i := 0; i+1 < len(pairs); i += 2 {
		field, ok := pairs[i].(string)
		if !ok {
			continue
		}
		value, ok := pairs[i+1].(string)
		if !ok {
			continue
		}
		assignField(tally, field, value, optionCount)
	}
	return tally
}

func assignField(tally *Tally, field, raw string, optionCount int) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return
	}

	if field == votersField {
		tally.Voters = n
		return
	}

	// "o:3" -> index 3. An unparseable or out-of-range field is ignored
	// rather than panicking: it can only come from an older schema, and a
	// slightly incomplete tally beats a crashed request.
	if len(field) > 2 && field[:2] == "o:" {
		idx, err := strconv.Atoi(field[2:])
		if err == nil && idx >= 0 && idx < optionCount {
			tally.Counts[idx] = n
		}
	}
}
