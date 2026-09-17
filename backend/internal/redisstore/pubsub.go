package redisstore

import (
	"context"
	"encoding/json"
	"fmt"
)

// Event types carried on a poll's channel.
const (
	EventResults = "results" // the tally changed
	EventClosed  = "closed"  // the owner stopped accepting votes
	EventDeleted = "deleted" // the poll is gone
)

// Event is what travels over Pub/Sub and, unchanged, on to WebSocket clients.
//
// The full poll view is embedded rather than just a delta. A delta would be
// smaller, but it would require every client to have the previous state and to
// have received every earlier message in order -- neither of which survives a
// dropped connection. Sending the whole tally means a client that reconnects
// mid-stream is correct on the very first message it receives.
type Event struct {
	Type   string `json:"type"`
	PollID string `json:"pollId"`
	Poll   any    `json:"poll,omitempty"`
}

// Publish fans an event out to every backend instance subscribed to this poll.
//
// This is the piece that makes horizontal scaling work. A vote arrives at
// instance A; the WebSocket clients watching that poll may be connected to
// instance B. Without Redis in the middle, B never hears about the vote and
// half the audience watches a frozen screen. With it, A publishes and every
// instance -- including A itself -- receives the event through the same path.
//
// Publishing to a channel with no subscribers is a no-op in Redis, not an
// error, so the vote path does not care whether anyone is watching.
func (s *Store) PublishEvent(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("redis: marshal event: %w", err)
	}

	if err := s.client.Publish(ctx, EventsChannel(event.PollID), payload).Err(); err != nil {
		return fmt.Errorf("redis: publish: %w", err)
	}
	return nil
}
