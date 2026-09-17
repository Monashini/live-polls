package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const CollectionPolls = "polls"

// Voting modes. The creator picks one when the poll is made; it cannot change
// afterwards, because switching mid-poll would make already-cast ballots mean
// something different from the ones cast after the switch.
const (
	// ModeSingle: a ballot names exactly one option.
	ModeSingle = "single"
	// ModeMultiple: a ballot names one or more options.
	ModeMultiple = "multiple"
)

// Option is one choice on a poll. Index is stored explicitly rather than being
// inferred from array position, so a vote document can reference a stable
// number that stays correct even if the array is ever reordered in future.
type Option struct {
	Index int    `bson:"index" json:"index"`
	Text  string `bson:"text"  json:"text"`
}

type Poll struct {
	ID bson.ObjectID `bson:"_id,omitempty" json:"id"`

	// Slug is a short random string used in share links. ObjectIDs embed a
	// timestamp and a counter, which makes them partly guessable; a random
	// slug means nobody can walk the poll list by incrementing a URL.
	Slug string `bson:"slug" json:"slug"`

	OwnerID  bson.ObjectID `bson:"ownerId"  json:"ownerId"`
	Question string        `bson:"question" json:"question"`
	Options  []Option      `bson:"options"  json:"options"`

	// Mode is ModeSingle or ModeMultiple. Stored per poll rather than being a
	// global setting, so one account can run both kinds.
	Mode string `bson:"mode" json:"mode"`

	// Counts is a denormalised tally, one entry per option, kept in the same
	// order as Options. It is a cache, not the source of truth: the votes
	// collection is authoritative and Counts can always be rebuilt from it.
	// See RecountByPoll in the votes repository.
	Counts []int64 `bson:"counts" json:"-"`

	// Ballots is how many people voted, which in multiple-choice mode is not
	// the same as the sum of Counts: one person selecting three options adds
	// three to Counts but one to Ballots. Percentages are meaningless without
	// it -- "60% chose CSV export" has to mean 60% of voters, not 60% of
	// selections.
	Ballots int64 `bson:"ballots" json:"-"`

	Closed bool `bson:"closed" json:"closed"`

	// ExpiresAt is optional. A nil value means the poll never expires.
	// Expired polls are kept, not deleted, so results stay readable.
	ExpiresAt *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

func (p *Poll) IsExpired(now time.Time) bool {
	return p.ExpiresAt != nil && now.After(*p.ExpiresAt)
}

// AcceptsVotes is the single definition of "can someone still vote on this".
// Both the vote path and the view layer call it, so the API can never report
// acceptsVotes:true while the vote endpoint rejects the request.
func (p *Poll) AcceptsVotes(now time.Time) bool {
	return !p.Closed && !p.IsExpired(now)
}

type OptionResult struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	Votes int64  `json:"votes"`
}

// PollView is the public result payload. Note it omits OwnerID: anyone with
// the link can read a poll, and they have no business learning who made it.
type PollView struct {
	ID       string         `json:"id"`
	Slug     string         `json:"slug"`
	Question string         `json:"question"`
	Mode     string         `json:"mode"`
	Options  []OptionResult `json:"options"`

	// TotalVotes counts people, and is the denominator for every percentage.
	// TotalSelections counts choices, and only differs in multiple mode.
	TotalVotes      int64 `json:"totalVotes"`
	TotalSelections int64 `json:"totalSelections"`

	Closed       bool       `json:"closed"`
	Expired      bool       `json:"expired"`
	AcceptsVotes bool       `json:"acceptsVotes"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// View projects the stored poll into its API shape, pairing each option with
// its count. A short or nil Counts slice is treated as zeroes rather than
// panicking, so a poll written before a future schema change still renders.
func (p *Poll) View(now time.Time) PollView {
	options := make([]OptionResult, 0, len(p.Options))
	var selections int64

	for i, opt := range p.Options {
		var votes int64
		if i < len(p.Counts) && p.Counts[i] > 0 {
			votes = p.Counts[i]
		}
		selections += votes
		options = append(options, OptionResult{Index: opt.Index, Text: opt.Text, Votes: votes})
	}

	// In single mode every ballot is exactly one selection, so the two agree.
	// Falling back to selections keeps percentages sane for any document
	// written before Ballots existed rather than dividing by zero.
	ballots := p.Ballots
	if ballots == 0 {
		ballots = selections
	}

	mode := p.Mode
	if mode == "" {
		mode = ModeSingle
	}

	return PollView{
		ID:              p.ID.Hex(),
		Slug:            p.Slug,
		Question:        p.Question,
		Mode:            mode,
		Options:         options,
		TotalVotes:      ballots,
		TotalSelections: selections,
		Closed:          p.Closed,
		Expired:         p.IsExpired(now),
		AcceptsVotes:    p.AcceptsVotes(now),
		ExpiresAt:       p.ExpiresAt,
		CreatedAt:       p.CreatedAt,
	}
}
