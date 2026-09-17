/**
 * Client-side mirrors of the server's rules.
 *
 * These exist to give instant feedback while typing. They are NOT the
 * validation -- `backend/internal/services/validate.go` is, and it re-checks
 * everything on arrival. Anyone can skip this file entirely with curl, so it
 * is treated as a convenience, never as a guarantee.
 *
 * The bounds below are duplicated from the Go constants on purpose: a shared
 * schema would be nice, but it would mean a build step and a code generator
 * for nine numbers. The tradeoff is that changing a bound means changing it in
 * two places, which is why both files name the same constants.
 */

export const LIMITS = {
  passwordMin: 8,
  passwordMax: 72, // bcrypt ignores anything past 72 bytes
  questionMin: 3,
  questionMax: 280,
  optionMax: 120,
  minOptions: 2,
  maxOptions: 10,
}

/** Matches the server: collapse internal whitespace runs, trim the ends. */
export function collapseSpace(value) {
  return value.trim().replace(/\s+/g, ' ')
}

export function validateEmail(email) {
  const value = email.trim()
  if (!value) return 'is required'
  // Deliberately loose. The server does the real parse; over-strict regexes
  // in browsers are famous for rejecting valid addresses.
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) {
    return 'is not a valid email address'
  }
  return null
}

export function validatePassword(password) {
  if (!password) return 'is required'
  if (password.length < LIMITS.passwordMin) {
    return `must be at least ${LIMITS.passwordMin} characters`
  }
  if (password.length > LIMITS.passwordMax) {
    return `must be ${LIMITS.passwordMax} characters or fewer`
  }
  return null
}

export function validateQuestion(question) {
  const value = collapseSpace(question)
  if (!value) return 'is required'
  // [...value] counts characters, not UTF-16 code units, so an emoji or a
  // Devanagari cluster costs one -- the same way the Go side counts runes.
  const length = [...value].length
  if (length < LIMITS.questionMin) {
    return `must be at least ${LIMITS.questionMin} characters`
  }
  if (length > LIMITS.questionMax) {
    return `must be ${LIMITS.questionMax} characters or fewer`
  }
  return null
}

/**
 * Validates the whole option list at once, because the interesting rules
 * (count, duplicates) are about the list rather than any single entry.
 *
 * Returns { error, fieldErrors } where fieldErrors is keyed by option index.
 */
export function validateOptions(options) {
  const fieldErrors = {}
  const filled = options.filter((o) => collapseSpace(o) !== '')

  if (filled.length < LIMITS.minOptions) {
    return {
      error: `Add at least ${LIMITS.minOptions} choices.`,
      fieldErrors,
    }
  }
  if (options.length > LIMITS.maxOptions) {
    return {
      error: `A poll can have at most ${LIMITS.maxOptions} choices.`,
      fieldErrors,
    }
  }

  const seen = new Map()

  options.forEach((raw, index) => {
    const value = collapseSpace(raw)
    if (!value) return // blank rows are dropped, not errors

    if ([...value].length > LIMITS.optionMax) {
      fieldErrors[index] = `must be ${LIMITS.optionMax} characters or fewer`
      return
    }

    // Case-insensitive, matching the server, so "Yes" and "yes" collide.
    const key = value.toLowerCase()
    if (seen.has(key)) {
      fieldErrors[index] = 'duplicates another choice'
    } else {
      seen.set(key, index)
    }
  })

  return {
    error: Object.keys(fieldErrors).length ? 'Fix the highlighted choices.' : null,
    fieldErrors,
  }
}

/** Drops blank rows and normalises whitespace, ready to send. */
export function cleanOptions(options) {
  return options.map(collapseSpace).filter(Boolean)
}
