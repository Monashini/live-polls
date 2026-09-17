package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const CollectionVotes = "votes"

// Vote is one document per cast vote.
//
// The alternative -- pushing votes into an array on the poll document -- was
// rejected for three reasons:
//  1. MongoDB documents are capped at 16MB, so an array would put a hard
//     ceiling on how many votes a poll can receive.
//  2. Every vote would rewrite the same document, serialising writes on one
//     lock. Separate documents let concurrent votes proceed independently.
//  3. A unique index across two fields of a document cannot enforce
//     "one entry per voter" inside an array the way it can across documents.
type Vote struct {
	ID     bson.ObjectID `bson:"_id,omitempty" json:"id"`
	PollID bson.ObjectID `bson:"pollId"        json:"pollId"`

	// OptionIndexes is the ballot: every option this voter selected. Single
	// mode always stores exactly one entry, multiple mode stores one or more.
	//
	// Keeping one document per BALLOT rather than one per selection is what
	// lets the same unique index serve both modes. If each selection were its
	// own document, a single-choice poll would need an application-level
	// "have they already voted?" check, which reintroduces the exact race the
	// index exists to eliminate.
	OptionIndexes []int `bson:"optionIndexes" json:"optionIndexes"`

	// VoterKey is the anonymous identity this vote is attributed to. Together
	// with PollID it forms the unique index that blocks a second vote.
	// It is never returned to clients.
	VoterKey string `bson:"voterKey" json:"-"`

	// IPHash is a salted SHA-256 of the client IP. The raw address is never
	// written to the database. It exists for abuse analysis and for the
	// per-IP rate limiting planned for the Redis phase.
	IPHash string `bson:"ipHash,omitempty" json:"-"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}
