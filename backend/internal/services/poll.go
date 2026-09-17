package services

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"livepolls/internal/apperr"
	"livepolls/internal/db"
	"livepolls/internal/models"
	"livepolls/internal/redisstore"
)

const (
	// slugAlphabet omits 0/O/1/l/I so a slug read aloud or copied by hand is
	// unambiguous. It is exactly 32 characters, which matters: 256 divides
	// evenly by 32, so the modulo below introduces no bias toward early
	// letters the way a 62-character alphabet would.
	slugAlphabet = "23456789abcdefghijkmnpqrstuvwxyz"
	slugLength   = 10 // 32^10 is about 50 bits of entropy, far past guessing range

	// slugAttempts bounds retries when a generated slug happens to collide.
	slugAttempts = 4

	// maxPollsListed caps the owner's poll list so the response size is bounded.
	maxPollsListed = 100
)

type PollService struct {
	polls *db.PollRepo
	votes *db.VoteRepo

	// live is the Redis layer: counters, fan-out and rate limiting. It is
	// optional at the type level (nil is tolerated everywhere below) so that
	// a Redis outage degrades the app to Phase 2 behaviour -- votes still
	// persist and REST still answers -- instead of taking it down.
	live *redisstore.Store

	rateLimit  int64
	rateWindow time.Duration
}

func NewPollService(
	polls *db.PollRepo,
	votes *db.VoteRepo,
	live *redisstore.Store,
	rateLimit int64,
	rateWindow time.Duration,
) *PollService {
	return &PollService{
		polls:      polls,
		votes:      votes,
		live:       live,
		rateLimit:  rateLimit,
		rateWindow: rateWindow,
	}
}

// CreatePollInput is the shape the handler passes in. It uses plain types so
// the service has no knowledge of HTTP or JSON.
type CreatePollInput struct {
	Question  string
	Options   []string
	Mode      string
	ExpiresAt *time.Time
}

// Create validates everything before a single byte reaches MongoDB.
func (s *PollService) Create(ctx context.Context, ownerID bson.ObjectID, in CreatePollInput) (*models.Poll, error) {
	now := time.Now().UTC()

	question, fieldErrs := CleanQuestion(in.Question)
	if fieldErrs != nil {
		return nil, apperr.Validation("Please check the highlighted fields.", fieldErrs)
	}

	optionTexts, fieldErrs := CleanOptions(in.Options)
	if fieldErrs != nil {
		return nil, apperr.Validation("Please check the highlighted fields.", fieldErrs)
	}

	mode, fieldErrs := CleanMode(in.Mode)
	if fieldErrs != nil {
		return nil, apperr.Validation("Please check the highlighted fields.", fieldErrs)
	}

	if expiryErrs := ValidateExpiry(in.ExpiresAt, now); expiryErrs != nil {
		return nil, apperr.Validation("Please check the highlighted fields.", expiryErrs)
	}

	options := make([]models.Option, len(optionTexts))
	for i, text := range optionTexts {
		options[i] = models.Option{Index: i, Text: text}
	}

	poll := &models.Poll{
		OwnerID:  ownerID,
		Question: question,
		Options:  options,
		Mode:     mode,
		// Counts is initialised to the right length up front so the $inc on
		// "counts.N" always targets an existing slot.
		Counts:    make([]int64, len(options)),
		Ballots:   0,
		Closed:    false,
		ExpiresAt: in.ExpiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Retry on the very unlikely slug collision rather than failing the
	// request. The unique index is what actually decides a collision.
	for attempt := 0; attempt < slugAttempts; attempt++ {
		slug, err := generateSlug()
		if err != nil {
			return nil, apperr.Internal(err)
		}
		poll.Slug = slug

		// A fresh ID per attempt; reusing one after a failed insert risks a
		// duplicate _id error masquerading as a slug collision.
		poll.ID = bson.NewObjectID()

		err = s.polls.Create(ctx, poll)
		if err == nil {
			return poll, nil
		}
		if !errors.Is(err, db.ErrDuplicate) {
			return nil, apperr.Internal(err)
		}
		slog.Warn("slug collision, retrying", "attempt", attempt+1)
	}

	return nil, apperr.Internal(errors.New("could not allocate a unique poll slug"))
}

// Results loads a poll by ID or slug and returns its public view.
func (s *PollService) Results(ctx context.Context, key string) (*models.PollView, error) {
	poll, err := s.load(ctx, key)
	if err != nil {
		return nil, err
	}

	poll = s.repairCountsIfNeeded(ctx, poll)
	s.applyLiveTally(ctx, poll)

	view := poll.View(time.Now().UTC())
	return &view, nil
}

// applyLiveTally serves the counts from Redis when they are there, and seeds
// Redis from MongoDB when they are not. Classic cache-aside.
//
// This is the "hot read path does not hit MongoDB" part of the design. The
// poll's own document -- question, options, mode -- still comes from MongoDB,
// because it is small, indexed, and immutable in practice. What Redis removes
// is recomputing a tally, which is the part that grows with traffic.
//
// It mutates the poll in place rather than returning a new value so that every
// caller building a view gets the same numbers without having to remember to
// ask for them.
func (s *PollService) applyLiveTally(ctx context.Context, poll *models.Poll) {
	if s.live == nil {
		return
	}

	pollID := poll.ID.Hex()

	tally, found, err := s.live.GetTally(ctx, pollID, len(poll.Options))
	if err != nil {
		// A Redis read failure is not worth failing the request over: the
		// MongoDB numbers already loaded are correct, just not as hot.
		slog.Error("redis tally read failed, serving from mongodb", "pollId", pollID, "err", err)
		return
	}

	if !found {
		// Cache miss: populate it from the durable numbers so the next read is
		// served from Redis. Detached, because a reader who navigated away
		// mid-request should still leave the cache warm for the next one.
		seedCtx, cancel := detached(ctx)
		defer cancel()

		seed := redisstore.Tally{Counts: poll.Counts, Voters: poll.Ballots}
		if seedErr := s.live.SeedTally(seedCtx, pollID, seed); seedErr != nil {
			slog.Error("redis tally seed failed", "pollId", pollID, "err", seedErr)
		}
		return
	}

	poll.Counts = tally.Counts
	poll.Ballots = tally.Voters
}

// ListByOwner returns the caller's own polls.
func (s *PollService) ListByOwner(ctx context.Context, ownerID bson.ObjectID) ([]models.PollView, error) {
	polls, err := s.polls.ListByOwner(ctx, ownerID, maxPollsListed)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	now := time.Now().UTC()
	views := make([]models.PollView, 0, len(polls))
	for i := range polls {
		views = append(views, polls[i].View(now))
	}
	return views, nil
}

// Close marks a poll as no longer accepting votes. Only the owner may do it.
func (s *PollService) Close(ctx context.Context, key string, ownerID bson.ObjectID) (*models.PollView, error) {
	poll, err := s.load(ctx, key)
	if err != nil {
		return nil, err
	}
	if poll.OwnerID != ownerID {
		// Checked here so the caller gets an honest 403 rather than a 404.
		// ownerID is ALSO part of the update filter below, so the database
		// enforces ownership even if this check were removed.
		return nil, apperr.Forbidden("You can only manage polls you created.")
	}
	if poll.Closed {
		return nil, apperr.Conflict(apperr.CodePollClosed, "This poll is already closed.")
	}

	updated, err := s.polls.SetClosed(ctx, poll.ID, ownerID, true, time.Now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, apperr.NotFound("Poll not found.")
		}
		return nil, apperr.Internal(err)
	}

	view := updated.View(time.Now().UTC())

	// Viewers watching this poll need to know it stopped accepting votes, or
	// they would keep a vote form on screen that the server will now reject.
	s.publish(ctx, redisstore.EventClosed, poll.ID.Hex(), view)

	return &view, nil
}

// Delete removes a poll. Vote documents are intentionally left in place for
// now; cleaning them up is a background concern, and orphaned votes are
// harmless because every read path starts from the poll.
func (s *PollService) Delete(ctx context.Context, key string, ownerID bson.ObjectID) error {
	poll, err := s.load(ctx, key)
	if err != nil {
		return err
	}
	if poll.OwnerID != ownerID {
		return apperr.Forbidden("You can only manage polls you created.")
	}

	if err := s.polls.Delete(ctx, poll.ID, ownerID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return apperr.NotFound("Poll not found.")
		}
		return apperr.Internal(err)
	}

	// Drop the cached counters so Redis is not holding results for a poll
	// that no longer exists, then tell any open viewers it is gone.
	if s.live != nil {
		if err := s.live.DropTally(ctx, poll.ID.Hex()); err != nil {
			slog.Error("dropping redis tally failed", "pollId", poll.ID.Hex(), "err", err)
		}
	}
	s.publish(ctx, redisstore.EventDeleted, poll.ID.Hex(), nil)

	return nil
}

// Vote records one anonymous vote and returns the updated results.
//
// Ordering matters here. The vote document is written FIRST, because its
// unique index is what decides whether this voter is allowed through. Only
// once that insert succeeds do we touch the cached counter. Incrementing
// first would double-count anyone who retried a rejected request.
func (s *PollService) Vote(ctx context.Context, key, voterKey, ipHash string, optionIndexes []int) (*models.PollView, error) {
	now := time.Now().UTC()

	// Rate limit first, before any database work. The whole point of a limit
	// is to make abusive traffic cheap to reject; checking it after two Mongo
	// round trips would mean an attacker still gets to consume them.
	if err := s.checkRateLimit(ctx, ipHash); err != nil {
		return nil, err
	}

	poll, err := s.load(ctx, key)
	if err != nil {
		return nil, err
	}

	if poll.Closed {
		return nil, apperr.Conflict(apperr.CodePollClosed, "This poll has been closed.")
	}
	if poll.IsExpired(now) {
		return nil, apperr.Conflict(apperr.CodePollClosed, "This poll has expired.")
	}

	// The poll's own mode decides what a legal ballot looks like, so a client
	// cannot widen a single-choice poll by claiming it is multiple-choice.
	selections, fieldErrs := CleanSelections(optionIndexes, poll.Mode, len(poll.Options))
	if fieldErrs != nil {
		return nil, apperr.Validation("Please check the highlighted fields.", fieldErrs)
	}

	if voterKey == "" {
		// Should be impossible: the handler always supplies one. Guarding
		// anyway, because an empty key would collapse every anonymous voter
		// into a single identity under the unique index.
		return nil, apperr.Internal(errors.New("vote attempted without a voter key"))
	}

	vote := &models.Vote{
		PollID:        poll.ID,
		OptionIndexes: selections,
		VoterKey:      voterKey,
		IPHash:        ipHash,
		CreatedAt:     now,
	}

	switch err := s.votes.Create(ctx, vote); {
	case errors.Is(err, db.ErrDuplicate):
		return nil, apperr.Conflict(apperr.CodeAlreadyVoted, "You have already voted on this poll.")
	case err != nil:
		return nil, apperr.Internal(err)
	}

	// Step 2: the durable tally. MongoDB's $inc is atomic and is what makes
	// concurrent votes correct; the number it returns is authoritative.
	updated, err := s.polls.IncrementCounts(ctx, poll.ID, selections)
	if err != nil {
		// The vote is durably recorded; only the cached tally failed.
		// Reporting a 500 here would be a lie, and would push the client into
		// a retry that gets rejected as a duplicate. Recount from the source
		// of truth instead and return a correct result.
		slog.Error("count increment failed, falling back to recount",
			"pollId", poll.ID.Hex(), "err", err)

		counts, ballots, recountErr := s.votes.RecountByPoll(ctx, poll.ID, len(poll.Options))
		if recountErr != nil {
			return nil, apperr.Internal(recountErr)
		}
		poll.Counts = counts
		poll.Ballots = ballots
		updated = poll
	}

	view := updated.View(now)

	// Step 3 and 4: the live layer. Both are best-effort by design -- see
	// applyLive. The vote is already safe at this point, so nothing below is
	// allowed to turn a successful vote into an error response.
	s.applyLive(ctx, updated, selections, view)

	return &view, nil
}

// detachedTimeout bounds work that outlives the request that triggered it.
const detachedTimeout = 5 * time.Second

// detached returns a context that keeps the parent's values but drops its
// cancellation, with a deadline of its own.
//
// This exists because of a genuine bug. The Redis work after a vote -- updating
// the counters and publishing the fan-out event -- was using the request
// context. When a voter submitted and immediately closed the tab, that context
// cancelled, the publish was aborted, and every other person watching the poll
// silently missed the update. The vote was durable and correct; the live layer
// just never heard about it.
//
// Cache seeding has the same shape: an abandoned read should still leave the
// cache warm for the next reader, not log an error and give up.
//
// WithoutCancel keeps any values on the context (request IDs, tracing) so the
// logs still correlate, while the timeout stops a detached goroutine hanging
// on an unreachable Redis.
func detached(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), detachedTimeout)
}

// checkRateLimit rejects a voter who is submitting too fast.
//
// Failing open on a Redis error is deliberate. The duplicate-vote unique index
// is the control that actually protects results; this limit only protects the
// server from load. Refusing every vote because the rate limiter is unreachable
// would convert a Redis hiccup into a total outage of the app's core feature.
func (s *PollService) checkRateLimit(ctx context.Context, ipHash string) error {
	if s.live == nil || ipHash == "" {
		return nil
	}

	result, err := s.live.AllowVote(ctx, ipHash, s.rateLimit, s.rateWindow)
	if err != nil {
		slog.Error("rate limit check failed, allowing the vote", "err", err)
		return nil
	}
	if result.Allowed {
		return nil
	}

	slog.Warn("vote rate limited", "current", result.Current, "limit", result.Limit)
	return apperr.TooManyRequests(
		"You are voting too quickly. Please wait a moment and try again.",
		result.RetryAfter,
	)
}

// applyLive updates the Redis counters and publishes the event that drives
// every connected WebSocket client.
//
// Everything here is best-effort and logged rather than returned. Consider
// what each failure actually means:
//
//   - Counter update fails: the durable tally in MongoDB is still correct, and
//     the next read that misses the cache rebuilds it from MongoDB.
//   - Publish fails: live viewers miss this one update. They are not stranded,
//     because the next vote's event carries the full tally, and a client that
//     reconnects refetches over REST.
//
// In neither case has the voter done anything wrong, so neither may produce an
// error response. Turning a successful, durably-recorded vote into a 500
// because a cache blinked would be the worst possible trade.
func (s *PollService) applyLive(ctx context.Context, poll *models.Poll, selections []int, view models.PollView) {
	if s.live == nil {
		return
	}

	// Detached: the vote is already durable, and this work must complete even
	// if the voter closed the tab the instant they submitted.
	ctx, cancel := detached(ctx)
	defer cancel()

	pollID := poll.ID.Hex()

	// Atomic HINCRBY on the live counters. A false second return means the
	// key was not there -- evicted, expired, or Redis restarted -- in which
	// case incrementing would have invented a tally starting from this single
	// vote. Seed the authoritative numbers from MongoDB instead.
	_, existed, err := s.live.IncrementVote(ctx, pollID, selections, len(poll.Options))
	switch {
	case err != nil:
		slog.Error("redis counter increment failed", "pollId", pollID, "err", err)
	case !existed:
		tally := redisstore.Tally{Counts: poll.Counts, Voters: poll.Ballots}
		if seedErr := s.live.SeedTally(ctx, pollID, tally); seedErr != nil {
			slog.Error("redis counter seed failed", "pollId", pollID, "err", seedErr)
		} else {
			slog.Info("redis counters rebuilt from mongodb", "pollId", pollID)
		}
	}

	// The event carries MongoDB's authoritative view, not Redis's. If the two
	// ever disagree, what people see on screen is the durable number.
	s.publish(ctx, redisstore.EventResults, pollID, view)
}

// publish sends one event to every backend instance watching this poll.
func (s *PollService) publish(ctx context.Context, eventType, pollID string, payload any) {
	if s.live == nil {
		return
	}

	ctx, cancel := detached(ctx)
	defer cancel()

	event := redisstore.Event{Type: eventType, PollID: pollID, Poll: payload}
	if err := s.live.PublishEvent(ctx, event); err != nil {
		slog.Error("publishing live event failed", "pollId", pollID, "type", eventType, "err", err)
	}
}

// load centralises "fetch a poll by id-or-slug and turn a miss into a 404".
func (s *PollService) load(ctx context.Context, key string) (*models.Poll, error) {
	if key == "" {
		return nil, apperr.NotFound("Poll not found.")
	}
	poll, err := s.polls.FindByKey(ctx, key)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, apperr.NotFound("Poll not found.")
		}
		return nil, apperr.Internal(err)
	}
	return poll, nil
}

// repairCountsIfNeeded rebuilds the cached tally when its length no longer
// matches the option list. That is the one situation where the cache is
// structurally wrong rather than merely stale, and it is cheap to detect.
// Failure to repair is logged and ignored: a slightly wrong count is better
// than a failed read.
func (s *PollService) repairCountsIfNeeded(ctx context.Context, poll *models.Poll) *models.Poll {
	if len(poll.Counts) == len(poll.Options) {
		return poll
	}

	counts, ballots, err := s.votes.RecountByPoll(ctx, poll.ID, len(poll.Options))
	if err != nil {
		slog.Error("recount failed", "pollId", poll.ID.Hex(), "err", err)
		return poll
	}
	if err := s.polls.ReplaceCounts(ctx, poll.ID, counts, ballots); err != nil {
		slog.Error("persisting recount failed", "pollId", poll.ID.Hex(), "err", err)
	}
	poll.Counts = counts
	poll.Ballots = ballots
	return poll
}

// generateSlug produces a URL-safe random identifier using crypto/rand.
// math/rand would be seeded predictably and would make share links guessable.
func generateSlug() (string, error) {
	buf := make([]byte, slugLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = slugAlphabet[int(b)%len(slugAlphabet)]
	}
	return string(buf), nil
}
