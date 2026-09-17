/**
 * The one place that turns a poll's flags into a human label, so "closed",
 * "expired" and "open" can never be described differently on two screens.
 */
export function PollStatus({ poll }) {
  if (poll.closed) return <span className="badge">Closed</span>
  if (poll.expired) return <span className="badge">Expired</span>
  return <span className="badge badge--live">Open</span>
}

export function ModeBadge({ mode }) {
  return (
    <span className="badge">
      {mode === 'multiple' ? 'Multiple choice' : 'Single choice'}
    </span>
  )
}
