package db

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// EnsureIndexes creates every index the application depends on for either
// correctness or performance. It runs on each startup and is idempotent:
// creating an index that already exists with the same name and keys is a no-op.
//
// Two of these indexes are correctness constraints, not optimisations. They are
// the last line of defence that holds even if application code has a race:
//   - uniq_email  stops two accounts sharing an address
//   - uniq_poll_voter stops one voter being counted twice on a poll
func (s *Store) EnsureIndexes(ctx context.Context) error {
	specs := []struct {
		name  string
		coll  *mongo.Collection
		index []mongo.IndexModel
	}{
		{
			name: "users",
			coll: s.Users.coll,
			index: []mongo.IndexModel{
				{
					// Emails are normalised to lowercase before insert, so a
					// plain unique index is enough to make accounts unique.
					Keys:    bson.D{{Key: "email", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("uniq_email"),
				},
			},
		},
		{
			name: "polls",
			coll: s.Polls.coll,
			index: []mongo.IndexModel{
				{
					Keys:    bson.D{{Key: "slug", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("uniq_slug"),
				},
				{
					// Serves "my polls, newest first" as an index-only sort.
					// Without it Mongo loads every poll in the collection and
					// sorts in memory, which fails past 32MB of results.
					Keys:    bson.D{{Key: "ownerId", Value: 1}, {Key: "createdAt", Value: -1}},
					Options: options.Index().SetName("owner_created"),
				},
			},
		},
		{
			name: "votes",
			coll: s.Votes.coll,
			index: []mongo.IndexModel{
				{
					// The duplicate-vote constraint. A second insert with the
					// same (pollId, voterKey) pair fails at the database, which
					// is race-proof in a way an application-level check is not.
					// It also serves lookups filtered by pollId alone, because
					// pollId is the index's leading field.
					Keys:    bson.D{{Key: "pollId", Value: 1}, {Key: "voterKey", Value: 1}},
					Options: options.Index().SetUnique(true).SetName("uniq_poll_voter"),
				},
			},
		},
	}

	for _, spec := range specs {
		if _, err := spec.coll.Indexes().CreateMany(ctx, spec.index); err != nil {
			return fmt.Errorf("create indexes on %s: %w", spec.name, err)
		}
	}
	return nil
}
