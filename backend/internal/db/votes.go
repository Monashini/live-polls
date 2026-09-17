package db

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"livepolls/internal/models"
)

type VoteRepo struct {
	coll *mongo.Collection
}

// Create records a vote. Returns ErrDuplicate when the uniq_poll_voter index
// rejects a second vote from the same voter on the same poll.
func (r *VoteRepo) Create(ctx context.Context, v *models.Vote) error {
	if v.ID.IsZero() {
		v.ID = bson.NewObjectID()
	}
	_, err := r.coll.InsertOne(ctx, v)
	return translate(err)
}

// RecountByPoll rebuilds the tally from the vote documents themselves. This is
// what makes the counts array on the poll safe to treat as a cache: if it ever
// drifts, the authoritative number is one aggregation away.
//
// optionCount sizes the result so every option gets an entry, including
// options nobody voted for. It returns the per-option tally and the number of
// ballots, which differ in multiple-choice mode.
func (r *VoteRepo) RecountByPoll(ctx context.Context, pollID bson.ObjectID, optionCount int) ([]int64, int64, error) {
	match := bson.D{{Key: "pollId", Value: pollID}}

	// $unwind turns one ballot naming three options into three rows, so the
	// group below counts selections. Ballots are counted separately, because
	// after the unwind that information is gone.
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$unwind", Value: "$optionIndexes"}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$optionIndexes"},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, translate(err)
	}
	defer cur.Close(ctx)

	var rows []struct {
		OptionIndex int   `bson:"_id"`
		N           int64 `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, 0, translate(err)
	}

	counts := make([]int64, optionCount)
	for _, row := range rows {
		// Guard against a stray vote referencing an option that no longer
		// exists rather than panicking on an out-of-range write.
		if row.OptionIndex >= 0 && row.OptionIndex < optionCount {
			counts[row.OptionIndex] = row.N
		}
	}

	ballots, err := r.coll.CountDocuments(ctx, match)
	if err != nil {
		return nil, 0, translate(err)
	}

	return counts, ballots, nil
}
