// Package ws holds the WebSocket layer: a hub that tracks which clients are
// watching which poll, and bridges Redis Pub/Sub to those clients.
//
// The division of labour matters. The hub is per-process and only knows about
// the sockets this instance is holding. Redis is what makes a vote received by
// one instance reach a viewer connected to another. Take Redis out and the app
// still "works" on a single machine, then quietly breaks the moment it is
// scaled to two -- which is exactly the bug that is hard to notice in
// development and impossible to miss in production.
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/redis/go-redis/v9"

	"livepolls/internal/redisstore"
)

// Hub fans Redis events out to the local WebSocket clients.
type Hub struct {
	store *redisstore.Store

	// pubsub is a single long-lived subscriber connection whose channel set
	// changes as rooms come and go. One connection with a dynamic set beats
	// one connection per poll: a server watching 500 polls would otherwise
	// hold 500 Redis connections, and free tiers cap connections well below
	// that.
	pubsub *redis.PubSub

	mu sync.RWMutex
	// rooms maps a poll ID to the clients on this instance watching it.
	rooms map[string]map[*Client]struct{}

	closeOnce sync.Once
	done      chan struct{}
}

func NewHub(store *redisstore.Store) *Hub {
	return &Hub{
		store: store,
		// Subscribing with no channels yet; they are added as clients arrive.
		pubsub: store.Client().Subscribe(context.Background()),
		rooms:  make(map[string]map[*Client]struct{}),
		done:   make(chan struct{}),
	}
}

// Run pumps messages from Redis to local clients until Close is called.
// It is expected to run in its own goroutine for the life of the process.
func (h *Hub) Run(ctx context.Context) {
	// Channel() starts go-redis's own receive goroutine and reconnects
	// internally if the Redis connection drops, so a blip does not require
	// tearing down every WebSocket.
	incoming := h.pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			h.Close()
			return

		case <-h.done:
			return

		case msg, ok := <-incoming:
			if !ok {
				return
			}
			h.dispatch(msg.Payload)
		}
	}
}

// dispatch routes one Redis message to the room it belongs to.
//
// The poll ID is read from the payload rather than parsed back out of the
// channel name: the event already carries it, and trusting the payload keeps
// the channel naming scheme free to change without breaking routing.
func (h *Hub) dispatch(payload string) {
	var event redisstore.Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		slog.Error("ws: undecodable event from redis", "err", err)
		return
	}
	if event.PollID == "" {
		return
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.rooms[event.PollID]))
	for client := range h.rooms[event.PollID] {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	// The snapshot above is deliberate: sending happens outside the lock, so
	// one slow or blocked client cannot stall every other room on this
	// instance.
	raw := []byte(payload)
	for _, client := range clients {
		client.Enqueue(raw)
	}
}

// Join registers a client and, if this is the first client for the poll on
// this instance, subscribes to the poll's Redis channel.
func (h *Hub) Join(ctx context.Context, pollID string, client *Client) error {
	h.mu.Lock()
	room, exists := h.rooms[pollID]
	if !exists {
		room = make(map[*Client]struct{})
		h.rooms[pollID] = room
	}
	room[client] = struct{}{}
	isFirst := !exists
	h.mu.Unlock()

	if isFirst {
		if err := h.pubsub.Subscribe(ctx, redisstore.EventsChannel(pollID)); err != nil {
			// Roll the membership back rather than leaving a client in a room
			// that will never receive anything.
			h.Leave(ctx, pollID, client)
			return err
		}
		slog.Debug("ws: subscribed to poll channel", "pollId", pollID)
	}

	return nil
}

// Leave removes a client and unsubscribes from Redis once the last client for
// that poll on this instance has gone. Without the unsubscribe, an instance
// that had briefly served a popular poll would keep receiving its traffic
// forever.
func (h *Hub) Leave(ctx context.Context, pollID string, client *Client) {
	h.mu.Lock()
	room, exists := h.rooms[pollID]
	if !exists {
		h.mu.Unlock()
		return
	}

	delete(room, client)
	isEmpty := len(room) == 0
	if isEmpty {
		delete(h.rooms, pollID)
	}
	h.mu.Unlock()

	if isEmpty {
		if err := h.pubsub.Unsubscribe(ctx, redisstore.EventsChannel(pollID)); err != nil {
			slog.Warn("ws: unsubscribe failed", "pollId", pollID, "err", err)
		}
		slog.Debug("ws: unsubscribed from poll channel", "pollId", pollID)
	}
}

// Stats backs the health endpoint and makes "is anyone actually connected?"
// answerable without attaching a debugger.
func (h *Hub) Stats() (rooms int, clients int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, room := range h.rooms {
		rooms++
		clients += len(room)
	}
	return rooms, clients
}

// Close shuts the hub down and disconnects every client. Safe to call twice,
// which matters because both the context cancellation in Run and the explicit
// shutdown path can reach it.
func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		close(h.done)

		h.mu.Lock()
		rooms := h.rooms
		h.rooms = make(map[string]map[*Client]struct{})
		h.mu.Unlock()

		for _, room := range rooms {
			for client := range room {
				client.Close()
			}
		}

		if err := h.pubsub.Close(); err != nil {
			slog.Warn("ws: closing pubsub", "err", err)
		}
	})
}
