// Package db owns every interaction with MongoDB. No other package imports the
// mongo driver, so the rest of the codebase cannot accidentally run a query
// from a handler or leak a driver error to a client.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"livepolls/internal/models"
)

// Sentinel errors returned by the repositories. Services translate these into
// apperr values; the raw driver error stays inside this package.
var (
	ErrNotFound  = errors.New("db: document not found")
	ErrDuplicate = errors.New("db: duplicate key")
)

// Store holds the client plus one repository per collection. Handing services
// a *Store rather than a *mongo.Database means a service can only reach the
// collections through methods that already handle errors correctly.
type Store struct {
	client *mongo.Client
	db     *mongo.Database

	Users *UserRepo
	Polls *PollRepo
	Votes *VoteRepo
}

// Connect dials MongoDB and verifies the connection with a real ping before
// returning. mongo.Connect on its own is lazy -- it does not contact the
// server -- so without the ping a bad URI would only surface on first query.
func Connect(ctx context.Context, uri, dbName string) (*Store, error) {
	opts := options.Client().
		ApplyURI(uri).
		// Bounded waits. Without these the driver blocks for 30s on an
		// unreachable cluster, which makes startup failures look like hangs.
		SetServerSelectionTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetMaxPoolSize(50).
		SetRetryWrites(true)

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("mongo: build client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		// Tidy up the half-open client so we do not leak goroutines when the
		// caller decides to exit.
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo: ping failed (check MONGODB_URI, credentials and IP allowlist): %w", err)
	}

	database := client.Database(dbName)

	return &Store{
		client: client,
		db:     database,
		Users:  &UserRepo{coll: database.Collection(models.CollectionUsers)},
		Polls:  &PollRepo{coll: database.Collection(models.CollectionPolls)},
		Votes:  &VoteRepo{coll: database.Collection(models.CollectionVotes)},
	}, nil
}

// Ping is used by the health endpoint. It takes its own short timeout so a
// stalled database cannot hold an HTTP request open.
func (s *Store) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.client.Ping(pingCtx, readpref.Primary())
}

func (s *Store) Disconnect(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// translate converts a driver error into one of this package's sentinels.
// Every repository funnels writes through it so duplicate-key handling is
// defined once instead of being re-derived at each call site.
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mongo.ErrNoDocuments):
		return ErrNotFound
	case mongo.IsDuplicateKeyError(err):
		return ErrDuplicate
	default:
		return err
	}
}
