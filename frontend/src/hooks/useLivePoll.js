import { useCallback, useState } from 'react'

import { socketURL } from '../api/client.js'
import * as pollsApi from '../api/polls.js'
import { useAsyncData } from './useAsyncData.js'
import { useWebSocket } from './useWebSocket.js'

/**
 * One poll, kept current.
 *
 * Two sources, deliberately:
 *
 *  - REST for the initial load. A WebSocket takes a round trip to open, and
 *    rendering nothing until it does would make every page feel slow. It is
 *    also the fallback that keeps working if sockets are blocked entirely, by
 *    a corporate proxy for instance.
 *  - WebSocket for everything after that. No polling, no refresh.
 *
 * On reconnect it refetches over REST. While a socket is down, votes are still
 * being cast and this client hears none of them; trusting what is on screen
 * after a drop is how a "live" page ends up confidently showing stale numbers.
 */
export function useLivePoll(key) {
  const { data, error, loading, refetch, replace } = useAsyncData(
    (signal) => pollsApi.getPoll(key, { signal }),
    [key],
  )

  // Set when the owner deletes the poll while someone is watching it.
  const [deleted, setDeleted] = useState(false)

  const handleEvent = useCallback(
    (event) => {
      switch (event.type) {
        case 'results':
        case 'closed':
          // Every event carries the whole poll, not a delta, so applying one
          // is a straight replace. That is what makes a client correct on the
          // first message it receives after a reconnect, rather than needing
          // to have seen every earlier message in order.
          if (event.poll) replace({ poll: event.poll })
          break

        case 'deleted':
          setDeleted(true)
          break

        default:
          // Unknown event types are ignored rather than thrown on, so the
          // backend can add new ones without breaking deployed clients.
          break
      }
    },
    [replace],
  )

  const connection = useWebSocket(socketURL(`/ws/polls/${encodeURIComponent(key)}`), {
    onMessage: handleEvent,
    onReconnect: refetch,
    // No point holding a socket open for a poll that failed to load.
    enabled: Boolean(key) && !deleted,
  })

  return {
    poll: data?.poll ?? null,
    loading,
    error,
    connection,
    deleted,
    refetch,
    replace,
  }
}
