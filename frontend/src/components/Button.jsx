/**
 * One button component so every button in the app shares a size, a radius and
 * a disabled treatment.
 *
 * `loading` disables the button as well as showing a spinner: a submit button
 * that stays clickable during the request is how duplicate records get
 * created.
 */
export function Button({
  variant = 'primary',
  size,
  block = false,
  loading = false,
  disabled = false,
  type = 'button',
  children,
  className = '',
  ...rest
}) {
  const classes = [
    'btn',
    `btn--${variant}`,
    size === 'sm' ? 'btn--sm' : '',
    block ? 'btn--block' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <button
      type={type}
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading && <span className="spinner" aria-hidden="true" />}
      {children}
    </button>
  )
}
