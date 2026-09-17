/**
 * Percentage of voters who picked an option.
 *
 * The denominator is people, not selections. In a multiple-choice poll one
 * person can pick three options, so percentages legitimately sum past 100% --
 * "60% of voters chose Go" is the useful statement, and dividing by selections
 * instead would quietly turn it into something else.
 */
export function percentOf(votes, totalVoters) {
  if (!totalVoters) return 0
  return Math.round((votes / totalVoters) * 100)
}

export function pluralize(count, singular, plural) {
  return count === 1 ? singular : (plural ?? `${singular}s`)
}

const RELATIVE = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

const UNITS = [
  ['year', 31536000000],
  ['month', 2592000000],
  ['day', 86400000],
  ['hour', 3600000],
  ['minute', 60000],
]

/** "3 days ago", "in 2 hours". Falls back to a date past a year out. */
export function relativeTime(isoString) {
  const then = new Date(isoString)
  if (Number.isNaN(then.getTime())) return ''

  const diffMs = then.getTime() - Date.now()

  for (const [unit, ms] of UNITS) {
    if (Math.abs(diffMs) >= ms) {
      return RELATIVE.format(Math.round(diffMs / ms), unit)
    }
  }
  return 'just now'
}

export function shareUrlFor(slug) {
  return `${window.location.origin}/p/${slug}`
}
