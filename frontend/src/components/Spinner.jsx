/**
 * Inline spinner. aria-hidden because the visible label beside it already
 * announces what is happening; a second announcement is noise.
 */
export function Spinner({ large = false }) {
  return <span className={large ? 'spinner spinner--lg' : 'spinner'} aria-hidden="true" />
}

/** Used while something blocks the whole screen, e.g. verifying a session. */
export function FullPageSpinner({ label = 'Loading' }) {
  return (
    <div className="state" role="status">
      <Spinner large />
      <p className="muted">{label}…</p>
    </div>
  )
}
