import { useEffect, useRef, useState } from 'react'

// Backoff bounds. The first retry is fast because most drops are a blip
// (a laptop waking up, a wifi handover) and recovering in half a second feels
// instant. The cap stops a server that is genuinely down from being hammered
// by every open tab at once.
const BASE_DELAY_MS = 500
const MAX_DELAY_MS = 15000

/**
 * Computes the delay before retry number `attempt`, with jitter.
 *
 * The jitter is the part people skip and regret. Without it, every client that
 * dropped when a server restarted reconnects at exactly the same millisecond,
 * and the stampede knocks the server over again -- a thundering herd that
 * turns one restart into an outage. Spreading each client randomly across its
 * backoff window avoids that for the cost of one Math.random().
 */
function backoffDelay(attempt) {
  const exponential = Math.min(BASE_DELAY_MS * 2 ** attempt, MAX_DELAY_MS)
  const jitter = exponential * 0.3 * (Math.random() * 2 - 1)
  return Math.max(BASE_DELAY_MS, Math.round(exponential + jitter))
}

/**
 * A WebSocket that reconnects itself.
 *
 * Returns a status string the UI can show: 'connecting' | 'open' |
 * 'reconnecting' | 'closed'.
 *
 * `onReconnect` fires after a *re*connection, never the first one. That is the
 * hook's most important guarantee: while the socket was down, votes may have
 * been cast that this client never heard about, so the page must refetch
 * rather than trust what is on screen. Without it, a five-second network blip
 * leaves stale results displayed indefinitely and nothing looks wrong.
 */
export function useWebSocket(url, { onMessage, onReconnect, enabled = true } = {}) {
  const [status, setStatus] = useState(enabled ? 'connecting' : 'closed')

  // Callbacks live in refs so that a parent re-render with a new inline
  // function does not tear down and rebuild the socket. Only `url` and
  // `enabled` are allowed to do that.
  const onMessageRef = useRef(onMessage)
  const onReconnectRef = useRef(onReconnect)
  onMessageRef.current = onMessage
  onReconnectRef.current = onReconnect

  useEffect(() => {
    if (!enabled || !url) {
      setStatus('closed')
      return undefined
    }

    let socket = null
    let retryTimer = null
    let attempt = 0
    let hasConnectedBefore = false
    // Set by cleanup. Every async path checks it, because a reconnect timer
    // that fires after unmount would otherwise open a socket nobody closes.
    let disposed = false

    function scheduleReconnect() {
      if (disposed) return

      const delay = backoffDelay(attempt)
      attempt += 1
      setStatus('reconnecting')

      retryTimer = setTimeout(() => {
        if (!disposed) connect()
      }, delay)
    }

    function connect() {
      if (disposed) return

      setStatus(hasConnectedBefore ? 'reconnecting' : 'connecting')

      try {
        socket = new WebSocket(url)
      } catch {
        // Constructing a WebSocket throws synchronously on a malformed URL.
        scheduleReconnect()
        return
      }

      socket.onopen = () => {
        if (disposed) return

        // Reset the backoff only on a *successful* open. Resetting on attempt
        // instead would turn a server that accepts then immediately drops
        // connections into a tight reconnect loop.
        attempt = 0
        setStatus('open')

        if (hasConnectedBefore) {
          onReconnectRef.current?.()
        }
        hasConnectedBefore = true
      }

      socket.onmessage = (event) => {
        if (disposed) return
        try {
          onMessageRef.current?.(JSON.parse(event.data))
        } catch {
          // A frame we cannot parse is not worth killing the connection over;
          // the next event carries a full snapshot anyway.
        }
      }

      socket.onerror = () => {
        // Deliberately empty. The browser fires error and then close for the
        // same failure, and handling both would double-count the attempt and
        // double the backoff.
      }

      socket.onclose = () => {
        if (disposed) return
        socket = null
        scheduleReconnect()
      }
    }

    connect()

    return () => {
      disposed = true
      clearTimeout(retryTimer)

      if (socket) {
        // Detach onclose first, or closing during cleanup would schedule a
        // reconnect for a component that is going away.
        socket.onclose = null
        socket.onerror = null
        socket.onmessage = null
        socket.onopen = null
        socket.close(1000, 'client navigating away')
      }
    }
  }, [url, enabled])

  return status
}
