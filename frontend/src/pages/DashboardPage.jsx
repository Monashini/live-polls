import { useCallback, useState } from 'react'
import { Link } from 'react-router-dom'

import * as pollsApi from '../api/polls.js'
import { Alert } from '../components/Alert.jsx'
import { PollCard } from '../components/PollCard.jsx'
import { StateMessage } from '../components/StateMessage.jsx'
import { useAsyncData } from '../hooks/useAsyncData.js'

function SkeletonList() {
  return (
    <ul className="poll-list" aria-hidden="true">
      {[0, 1, 2].map((i) => (
        <li key={i} className="card stack">
          <div className="skeleton" style={{ height: '0.875rem', width: '30%' }} />
          <div className="skeleton" style={{ height: '1.25rem', width: '75%' }} />
          <div className="skeleton" style={{ height: '0.875rem', width: '45%' }} />
        </li>
      ))}
    </ul>
  )
}

export function DashboardPage() {
  // All fetching happens here, at page level. PollCard receives data and
  // callbacks, so it never reaches for the network itself.
  const { data, error, loading, refetch } = useAsyncData(
    (signal) => pollsApi.listMyPolls({ signal }),
    [],
  )

  // Tracks which row has an action in flight, so only that card's button shows
  // a spinner rather than the whole list locking up.
  const [busy, setBusy] = useState({ id: null, action: null })
  const [actionError, setActionError] = useState(null)

  const handleClose = useCallback(
    async (poll) => {
      setBusy({ id: poll.id, action: 'close' })
      setActionError(null)
      try {
        await pollsApi.closePoll(poll.slug)
        refetch()
      } catch (err) {
        setActionError(err.message ?? 'Could not close that poll.')
      } finally {
        setBusy({ id: null, action: null })
      }
    },
    [refetch],
  )

  const handleDelete = useCallback(
    async (poll) => {
      // window.confirm rather than a custom modal: it is a genuine
      // irreversible action, and a hand-rolled dialog here would be more code
      // for less accessibility. Not alert() -- this one asks a question and
      // uses the answer.
      const confirmed = window.confirm(
        `Delete "${poll.question}"? This cannot be undone.`,
      )
      if (!confirmed) return

      setBusy({ id: poll.id, action: 'delete' })
      setActionError(null)
      try {
        await pollsApi.deletePoll(poll.slug)
        refetch()
      } catch (err) {
        setActionError(err.message ?? 'Could not delete that poll.')
      } finally {
        setBusy({ id: null, action: null })
      }
    },
    [refetch],
  )

  return (
    <div className="page page--wide stack" style={{ '--gap': 'var(--s5)' }}>
      <div className="row row--between">
        <h1>Your polls</h1>
        <Link className="btn btn--primary" to="/polls/new">
          New poll
        </Link>
      </div>

      <Alert>{actionError}</Alert>

      {loading && <SkeletonList />}

      {!loading && error && (
        <StateMessage
          title="Could not load your polls"
          body={error.message}
          action="Try again"
          onAction={refetch}
        />
      )}

      {!loading && !error && data?.polls.length === 0 && (
        <StateMessage
          title="No polls yet"
          body="Create your first poll, share the link, and watch the results come in."
        >
          <Link className="btn btn--primary" to="/polls/new">
            Create a poll
          </Link>
        </StateMessage>
      )}

      {!loading && !error && data?.polls.length > 0 && (
        <ul className="poll-list">
          {data.polls.map((poll) => (
            <PollCard
              key={poll.id}
              poll={poll}
              onClose={handleClose}
              onDelete={handleDelete}
              busyAction={busy.id === poll.id ? busy.action : null}
            />
          ))}
        </ul>
      )}
    </div>
  )
}
