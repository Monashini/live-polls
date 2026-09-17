/**
 * The connection indicator.
 *
 * A live page that has silently stopped being live is worse than one that was
 * never live at all: the numbers look authoritative and are wrong. This makes
 * the socket's state something the viewer can actually see.
 *
 * Wording is chosen for someone who is not a developer -- "Live" and
 * "Reconnecting", not "OPEN" and "CLOSED" -- and the state is carried by text
 * as well as colour, so it does not rely on telling green from amber.
 */
const LABELS = {
  connecting: { text: 'Connecting…', tone: 'idle' },
  open: { text: 'Live', tone: 'live' },
  reconnecting: { text: 'Reconnecting…', tone: 'idle' },
  closed: { text: 'Offline', tone: 'off' },
}

export function LiveStatus({ status }) {
  const { text, tone } = LABELS[status] ?? LABELS.closed

  return (
    <span
      className={`livedot livedot--${tone}`}
      role="status"
      aria-live="polite"
      title={
        tone === 'live'
          ? 'Results update automatically as votes come in.'
          : 'Not receiving live updates right now.'
      }
    >
      <span className="livedot__dot" aria-hidden="true" />
      {text}
    </span>
  )
}
