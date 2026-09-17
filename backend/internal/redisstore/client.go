// Package redisstore owns every interaction with Redis, the same way the db
// package owns MongoDB. Nothing else imports the Redis client.
//
// Redis has three distinct jobs in this application, none of them decorative:
//
//  1. Live vote counters. The read path serves results from a Redis hash
//     instead of querying MongoDB on every page load.
//  2. Pub/Sub fan-out. A vote received by one backend instance has to reach
//     WebSocket clients connected to a *different* instance. An in-process
//     hub alone cannot do that; the moment you run two replicas, half your
//     viewers stop seeing updates.
//  3. Rate limiting. Per-IP vote throttling needs a counter shared across
//     instances, which is exactly what Redis is good at.
package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client *redis.Client

	// countsTTL bounds how long a poll's counter hash survives without being
	// touched. Redis is a cache here, not storage: letting old polls expire
	// keeps memory bounded, and a miss is cheap because the counters can
	// always be rebuilt from MongoDB.
	countsTTL time.Duration
}

// Connect parses a Redis URL and verifies the connection with a real PING.
//
// The URL form carries everything including TLS: "rediss://" (two esses)
// switches on TLS, which every hosted provider requires. Parsing it rather
// than accepting host/port/password separately means one env var instead of
// four, and no chance of setting the host for production and forgetting the
// TLS flag.
func Connect(ctx context.Context, rawURL string, countsTTL time.Duration) (*Store, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("redis: invalid REDIS_URL: %w", err)
	}

	// Bounded waits, for the same reason as the Mongo client: an unreachable
	// server should fail fast and loudly rather than hang the request.
	opts.DialTimeout = 10 * time.Second
	opts.ReadTimeout = 5 * time.Second
	opts.WriteTimeout = 5 * time.Second
	opts.PoolSize = 20

	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping failed (check REDIS_URL and whether the host needs rediss://): %w", err)
	}

	return &Store{client: client, countsTTL: countsTTL}, nil
}

// Ping backs the health endpoint.
func (s *Store) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.client.Ping(pingCtx).Err()
}

func (s *Store) Close() error { return s.client.Close() }

// Client exposes the raw client to the WebSocket hub, which needs its own
// long-lived Pub/Sub connection. Nothing else should use it.
func (s *Store) Client() *redis.Client { return s.client }

// ---------------------------------------------------------------------------
// Key naming
//
// Every key is namespaced so a shared Redis instance (which is what free tiers
// are) cannot collide with another application's keys.
// ---------------------------------------------------------------------------

func countsKey(pollID string) string { return "livepolls:poll:" + pollID + ":counts" }

// EventsChannel is the Pub/Sub channel for one poll. Exported because the hub
// subscribes to it by name.
func EventsChannel(pollID string) string { return "livepolls:poll:" + pollID + ":events" }

func rateKey(scope, identity string) string {
	return "livepolls:rate:" + scope + ":" + identity
}
