package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"livepolls/internal/models"
)

type PollRepo struct {
	coll *mongo.Collection
}

func (r *PollRepo) Create(ctx context.Context, p *models.Poll) error {
	if p.ID.IsZero() {
		p.ID = bson.NewObjectID()
	}
	_, err := r.coll.InsertOne(ctx, p)
	return translate(err)
}

// FindByKey accepts either a 24-character ObjectID hex string or a slug, so a
// share link and an API call can use the same route. Trying the ObjectID form
// first means the common case costs one query.
func (r *PollRepo) FindByKey(ctx context.Context, key string) (*models.Poll, error) {
	filter := bson.D{{Key: "slug", Value: key}}
	if id, err := bson.ObjectIDFromHex(key); err == nil {
		filter = bson.D{{Key: "_id", Value: id}}
	}

	var p models.Poll
	if err := r.coll.FindOne(ctx, filter).Decode(&p); err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// ListByOwner returns the caller's polls, newest first. The limit is applied
// server-side so a user with thousands of polls cannot force an unbounded
// response.
func (r *PollRepo) ListByOwner(ctx context.Context, ownerID bson.ObjectID, limit int64) ([]models.Poll, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(limit)

	cur, err := r.coll.Find(ctx, bson.D{{Key: "ownerId", Value: ownerID}}, opts)
	if err != nil {
		return nil, translate(err)
	}
	defer cur.Close(ctx)

	// Decoding into a non-nil slice means an owner with no polls serialises as
	// [] rather than null, which saves the frontend a null check.
	polls := make([]models.Poll, 0)
	if err := cur.All(ctx, &polls); err != nil {
		return nil, translate(err)
	}
	return polls, nil
}

// SetClosed flips the closed flag and returns the updated document.
// ownerID is part of the filter, not checked afterwards in Go: the database
// itself refuses to update a poll the caller does not own, so there is no
// window between the ownership check and the write.
func (r *PollRepo) SetClosed(ctx context.Context, id, ownerID bson.ObjectID, closed bool, now time.Time) (*models.Poll, error) {
	filter := bson.D{{Key: "_id", Value: id}, {Key: "ownerId", Value: ownerID}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "closed", Value: closed},
		{Key: "updatedAt", Value: now},
	}}}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var p models.Poll
	if err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&p); err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// Delete removes a poll the caller owns. Returns ErrNotFound when nothing
// matched, which covers both "no such poll" and "not yours".
func (r *PollRepo) Delete(ctx context.Context, id, ownerID bson.ObjectID) error {
	res, err := r.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "ownerId", Value: ownerID}})
	if err != nil {
		return translate(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementCounts bumps one entry of the denormalised counts array per
// selected option, plus the ballot counter, and returns the poll as it now
// stands.
//
// All of it is one $inc document, so a three-option ballot is a single atomic
// update rather than three round trips that could half-apply. Using $inc
// rather than read-modify-write means concurrent votes cannot lose an update:
// the increment happens inside the database, under the document lock.
func (r *PollRepo) IncrementCounts(ctx context.Context, id bson.ObjectID, optionIndexes []int) (*models.Poll, error) {
	incs := make(bson.D, 0, len(optionIndexes)+1)
	for _, idx := range optionIndexes {
		// The dotted path targets a single array slot, e.g. "counts.2".
		incs = append(incs, bson.E{Key: "counts." + itoa(idx), Value: 1})
	}
	incs = append(incs, bson.E{Key: "ballots", Value: 1})

	update := bson.D{{Key: "$inc", Value: incs}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var p models.Poll
	if err := r.coll.FindOneAndUpdate(ctx, bson.D{{Key: "_id", Value: id}}, update, opts).Decode(&p); err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// ReplaceCounts overwrites the cached tally, used after a recount repairs drift.
func (r *PollRepo) ReplaceCounts(ctx context.Context, id bson.ObjectID, counts []int64, ballots int64) error {
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "counts", Value: counts},
		{Key: "ballots", Value: ballots},
	}}}
	_, err := r.coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}}, update)
	return translate(err)
}

// itoa avoids pulling strconv in just for index paths.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
