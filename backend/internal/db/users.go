package db

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"livepolls/internal/models"
)

type UserRepo struct {
	coll *mongo.Collection
}

// Create inserts a user. The ID is assigned here rather than being read back
// from InsertedID, so the caller always has a populated struct.
// Returns ErrDuplicate if the email is taken.
func (r *UserRepo) Create(ctx context.Context, u *models.User) error {
	if u.ID.IsZero() {
		u.ID = bson.NewObjectID()
	}
	_, err := r.coll.InsertOne(ctx, u)
	return translate(err)
}

// FindByEmail expects an already-normalised (trimmed, lowercased) address.
// Returns ErrNotFound when no such user exists.
func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := r.coll.FindOne(ctx, bson.D{{Key: "email", Value: email}}).Decode(&u)
	if err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

func (r *UserRepo) FindByID(ctx context.Context, id bson.ObjectID) (*models.User, error) {
	var u models.User
	err := r.coll.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&u)
	if err != nil {
		return nil, translate(err)
	}
	return &u, nil
}
