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
}

func NewPollService(polls *db.PollRepo, votes *db.VoteRepo) *PollService {
	return &PollService{polls: polls, votes: votes}
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

	view := poll.View(time.Now().UTC())
	return &view, nil
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
		view := poll.View(now)
		return &view, nil
	}

	view := updated.View(now)
	return &view, nil
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
