import { useId } from 'react'

/**
 * Label + control + error, wired together.
 *
 * useId generates the htmlFor/id pair, so the label is always clickable and
 * the error is always announced via aria-describedby. Doing this by hand per
 * form is exactly where accessibility quietly rots.
 */
export function Field({ label, error, hint, children }) {
  const id = useId()
  const errorId = `${id}-error`
  const hintId = `${id}-hint`

  const describedBy = [error ? errorId : null, hint ? hintId : null]
    .filter(Boolean)
    .join(' ')

  return (
    <div className="field">
      <label className="field__label" htmlFor={id}>
        {label}
      </label>

      {children({
        id,
        'aria-invalid': error ? true : undefined,
        'aria-describedby': describedBy || undefined,
        className: error ? 'input input--invalid' : 'input',
      })}

      {hint && (
        <span className="field__hint" id={hintId}>
          {hint}
        </span>
      )}
      {error && (
        <span className="field__error" id={errorId}>
          {error}
        </span>
      )}
    </div>
  )
}
