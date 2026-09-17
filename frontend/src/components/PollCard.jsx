import { Link } from 'react-router-dom'

import { pluralize, relativeTime } from '../lib/format.js'
import { Button } from './Button.jsx'
import { ModeBadge, PollStatus } from './PollStatus.jsx'

/**
 * A row in the dashboard list.
 *
 * Purely presentational: it receives callbacks and a `busy` flag rather than
 * calling the API itself. Data fetching stays at page level, so this component
 * can be rendered anywhere without dragging a network dependency along.
 */
export function PollCard({ poll, onClose, onDelete, busyAction }) {
  return (
    <li className="card stack" style={{ '--gap': 'var(--s3)' }}>
      <div className="row row--between">
        <div className="row" style={{ gap: 'var(--s2)' }}>
          <PollStatus poll={poll} />
          <ModeBadge mode={poll.mode} />
        </div>
        <span className="meta">{relativeTime(poll.createdAt)}</span>
      </div>

      <h2>
        <Link to={`/polls/${poll.slug}/results`}>{poll.question}</Link>
      </h2>

      <p className="meta">
        {poll.totalVotes} {pluralize(poll.totalVotes, 'vote')} ·{' '}
        {poll.options.length} choices
      </p>

      <div className="row">
        <Link className="btn btn--secondary btn--sm" to={`/p/${poll.slug}`}>
          Open vote page
        </Link>

        {!poll.closed && (
          <Button
            variant="secondary"
            size="sm"
            loading={busyAction === 'close'}
            onClick={() => onClose(poll)}
          >
            Close voting
          </Button>
        )}

        <Button
          variant="danger"
          size="sm"
          loading={busyAction === 'delete'}
          onClick={() => onDelete(poll)}
        >
          Delete
        </Button>
      </div>
    </li>
  )
}
