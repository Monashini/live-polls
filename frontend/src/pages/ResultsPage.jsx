import { Link, useParams } from 'react-router-dom'

import { ApiError } from '../api/client.js'
import { Button } from '../components/Button.jsx'
import { CopyLink } from '../components/CopyLink.jsx'
import { LiveStatus } from '../components/LiveStatus.jsx'
import { ModeBadge, PollStatus } from '../components/PollStatus.jsx'
import { ResultsBars } from '../components/ResultsBars.jsx'
import { StateMessage } from '../components/StateMessage.jsx'
import { useLivePoll } from '../hooks/useLivePoll.js'
import { pluralize, relativeTime, shareUrlFor } from '../lib/format.js'

/**
 * Read-only results, public to anyone with the link.
 *
 * There is intentionally no Close or Delete button here. The public poll
 * payload omits ownerId -- anyone with a share link can read a poll, and they
 * have no business learning who created it -- so this page cannot tell whether
 * the viewer is the owner. Management lives on the dashboard, where ownership
 * is established by the endpoint itself.
 *
 * Results arrive over a WebSocket, so nothing here polls and nothing needs a
 * refresh. The manual refresh button only appears when the socket is down,
 * which is the one moment it is actually useful.
 */
export function ResultsPage() {
  const { key } = useParams()

  const { poll, error, loading, connection, deleted, refetch } = useLivePoll(key)

  if (loading) {
    return (
      <div className="page stack">
        <div className="skeleton" style={{ height: '2rem', width: '70%' }} />
        <div className="card stack">
          <div className="skeleton" style={{ height: '1rem' }} />
          <div className="skeleton" style={{ height: '1rem' }} />
          <div className="skeleton" style={{ height: '1rem' }} />
        </div>
      </div>
    )
  }

  if (error) {
    const notFound = error instanceof ApiError && error.status === 404
    return (
      <div className="page">
        <StateMessage
          title={notFound ? 'Poll not found' : 'Could not load these results'}
          body={
            notFound
              ? 'This link may be wrong, or the poll may have been deleted.'
              : error.message
          }
          action={notFound ? undefined : 'Try again'}
          onAction={notFound ? undefined : refetch}
        >
          {notFound && (
            <Link className="btn btn--secondary" to="/">
              Go home
            </Link>
          )}
        </StateMessage>
      </div>
    )
  }

  if (deleted) {
    return (
      <div className="page">
        <StateMessage
          title="This poll was deleted"
          body="The person who created it removed it while you were watching."
        >
          <Link className="btn btn--secondary" to="/">
            Go home
          </Link>
        </StateMessage>
      </div>
    )
  }

  const noVotesYet = poll.totalVotes === 0

  return (
    <div className="page stack" style={{ '--gap': 'var(--s5)' }}>
      <div className="stack" style={{ '--gap': 'var(--s3)' }}>
        <div className="row" style={{ gap: 'var(--s2)' }}>
          <PollStatus poll={poll} />
          <ModeBadge mode={poll.mode} />
          <LiveStatus status={connection} />
          <span className="meta">created {relativeTime(poll.createdAt)}</span>
        </div>
        <h1>{poll.question}</h1>
      </div>

      <div className="card stack" style={{ '--gap': 'var(--s5)' }}>
        {noVotesYet ? (
          <StateMessage
            title="No votes yet"
            body="Share the link below — results appear here as soon as someone votes."
          />
        ) : (
          <>
            <ResultsBars options={poll.options} totalVotes={poll.totalVotes} />

            <div className="row row--between">
              <span className="meta">
                {poll.totalVotes} {pluralize(poll.totalVotes, 'voter')}
                {poll.mode === 'multiple' &&
                  ` · ${poll.totalSelections} total selections`}
              </span>

              {/* Only offered when the live feed is not doing its job. */}
              {connection !== 'open' && (
                <Button variant="ghost" size="sm" onClick={refetch}>
                  Refresh
                </Button>
              )}
            </div>
          </>
        )}
      </div>

      <div className="card stack" style={{ '--gap': 'var(--s3)' }}>
        <h2>Share this poll</h2>
        <CopyLink url={shareUrlFor(poll.slug)} />
      </div>

      {poll.acceptsVotes && (
        <p className="meta">
          <Link to={`/p/${poll.slug}`}>Go to the vote page</Link>
        </p>
      )}
    </div>
  )
}
