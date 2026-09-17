/**
 * Replaces alert() everywhere.
 *
 * role="alert" makes a screen reader announce errors the moment they appear,
 * which a plain coloured <div> does not. Success and info messages use
 * role="status" instead so they do not interrupt whatever is being read.
 */
export function Alert({ variant = 'error', children }) {
  if (!children) return null

  return (
    <div
      className={`alert alert--${variant}`}
      role={variant === 'error' ? 'alert' : 'status'}
    >
      <span>{children}</span>
    </div>
  )
}
