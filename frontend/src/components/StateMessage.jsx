import { Button } from './Button.jsx'

/**
 * The shared shape for "nothing here", "that failed" and "not found".
 *
 * Having one component means an empty list and a failed request cannot drift
 * into looking like unrelated parts of the app, and every error state gets a
 * retry affordance by construction rather than by remembering to add one.
 */
export function StateMessage({ title, body, action, onAction, children }) {
  return (
    <div className="state">
      <p className="state__title">{title}</p>
      {body && <p className="state__body">{body}</p>}
      {action && onAction && (
        <Button variant="secondary" size="sm" onClick={onAction}>
          {action}
        </Button>
      )}
      {children}
    </div>
  )
}
