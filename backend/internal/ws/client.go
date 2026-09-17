package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeWait bounds a single write. Without a deadline, a client that stops
	// reading (a laptop that went to sleep, a phone that lost signal) leaves a
	// goroutine blocked in Write forever.
	writeWait = 10 * time.Second

	// pongWait is how long we tolerate silence before assuming the peer is
	// gone. TCP alone will not tell us: a connection through a NAT that has
	// dropped the mapping looks perfectly healthy until you try to write.
	pongWait = 60 * time.Second

	// pingPeriod must be shorter than pongWait, or we would time the client
	// out before we had given it a chance to answer. 90% leaves room for one
	// round trip on a slow link.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize is small on purpose. This feed is one-way; a client has
	// nothing legitimate to send, so anything large is either a bug or an
	// attempt to exhaust memory.
	maxMessageSize = 512

	// sendBuffer absorbs a burst of votes without blocking the hub. A client
	// that cannot keep up with even this much is disconnected rather than
	// allowed to apply back-pressure to everyone else.
	sendBuffer = 32
)

// Client is one WebSocket connection watching one poll.
//
// It follows the standard two-goroutine pattern: exactly one goroutine writes
// and exactly one reads. A gorilla connection supports one concurrent reader
// and one concurrent writer and no more, so funnelling all writes through a
// single channel is what makes concurrent broadcast safe.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	pollID string

	send chan []byte

	// closing is closed exactly once to signal shutdown. A dedicated channel
	// rather than closing `send`: the hub may be mid-enqueue on another
	// goroutine, and sending on a closed channel panics.
	closing   chan struct{}
	closeOnce sync.Once
}

func NewClient(hub *Hub, conn *websocket.Conn, pollID string) *Client {
	return &Client{
		hub:     hub,
		conn:    conn,
		pollID:  pollID,
		send:    make(chan []byte, sendBuffer),
		closing: make(chan struct{}),
	}
}

// Enqueue hands a payload to this client's writer.
//
// Never blocks. If the buffer is full the client is disconnected instead:
// one viewer on a bad connection must not be able to slow down the broadcast
// to everyone else, and a client this far behind would be showing stale
// results anyway. It will reconnect and refetch.
func (c *Client) Enqueue(payload []byte) {
	select {
	case c.send <- payload:
	case <-c.closing:
	default:
		slog.Debug("ws: dropping slow client", "pollId", c.pollID)
		c.Close()
	}
}

// Close disconnects the client. Safe to call more than once and from any
// goroutine, which matters because the hub, the read pump and shutdown can all
// reach it.
func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.closing) })
}

// Start runs the read and write pumps. It returns immediately; the connection
// is cleaned up when either pump exits.
//
// It deliberately takes no context from the caller. The HTTP request context
// is cancelled the moment the handler returns, and for a hijacked connection
// that is immediately -- using it would tear the socket down on creation. The
// connection's lifetime is governed by the read/write deadlines instead.
func (c *Client) Start() {
	go c.writePump()
	go c.readPump()
}

// readPump exists even though clients never send anything useful.
//
// Gorilla only processes control frames -- pong, close -- as a side effect of
// a read call. Without a reader, keepalive would silently not work and dead
// connections would accumulate.
func (c *Client) readPump() {
	defer func() {
		// Background context: this cleanup has to complete (it unsubscribes
		// from Redis) even though the originating request finished long ago.
		c.hub.Leave(context.Background(), c.pollID, c)
		c.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)

	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}

	// Each pong pushes the deadline out. Stop hearing pongs and the next read
	// fails, which is how a half-open connection gets noticed.
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived,
			) {
				slog.Debug("ws: connection ended unexpectedly", "pollId", c.pollID, "err", err)
			}
			return
		}
		// Payload deliberately discarded. Accepting commands over this socket
		// would mean another input surface to validate; votes go over the
		// authenticated, validated, rate-limited REST endpoint instead.
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case payload := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.closing:
			// Send a proper close frame so the browser reports a clean
			// closure rather than a network error, which keeps the client's
			// reconnect logic from treating a planned shutdown as a fault.
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}
