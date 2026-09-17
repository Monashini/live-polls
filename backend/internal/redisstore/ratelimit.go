package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateScript is a fixed-window counter.
//
// INCR and EXPIRE have to be atomic together. Done as two commands, a crash or
// a network blip between them leaves a key with no expiry -- which means that
// IP is rate limited forever. Setting the TTL only when the counter is created
// (== 1) is what makes it a window rather than a sliding-forward block.
//
// A fixed window is chosen over a sliding log because it costs one integer per
// IP instead of one entry per request, and the failure mode is mild: at a
// window boundary someone can briefly get up to twice the limit. For vote
// spam that is irrelevant; for something like login throttling it would not
// be.
//
// Returns {current count, milliseconds until the window resets}.
var rateScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return {current, redis.call('PTTL', KEYS[1])}
`)

// RateLimitResult describes one rate-limit decision.
type RateLimitResult struct {
	Allowed    bool
	Current    int64
	Limit      int64
	RetryAfter time.Duration
}

// AllowVote applies a per-identity vote rate limit.
//
// The identity is a hashed IP, never a raw address -- the same salted digest
// stored on vote documents, so nothing here can be reversed into a real
// address.
//
// This is a different control from the duplicate-vote check. That one stops a
// person voting twice on one poll; this one stops a script hammering many
// polls from one source. Neither substitutes for the other.
func (s *Store) AllowVote(ctx context.Context, identity string, limit int64, window time.Duration) (RateLimitResult, error) {
	result := RateLimitResult{Allowed: true, Limit: limit}

	if identity == "" || limit <= 0 {
		// No identity means we cannot attribute the request to anyone, so
		// there is nothing to count. Failing open here is deliberate: the
		// duplicate-vote index is the real protection.
		return result, nil
	}

	raw, err := rateScript.Run(ctx, s.client,
		[]string{rateKey("vote", identity)},
		window.Milliseconds(),
	).Result()
	if err != nil {
		return result, fmt.Errorf("redis: rate limit: %w", err)
	}

	values, ok := raw.([]any)
	if !ok || len(values) < 2 {
		return result, nil
	}

	current, _ := values[0].(int64)
	ttlMs, _ := values[1].(int64)

	result.Current = current
	result.Allowed = current <= limit
	if ttlMs > 0 {
		result.RetryAfter = time.Duration(ttlMs) * time.Millisecond
	}

	return result, nil
}
