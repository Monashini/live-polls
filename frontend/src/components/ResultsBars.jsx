import { percentOf, pluralize } from '../lib/format.js'
import { AnimatedNumber } from './AnimatedNumber.jsx'

/**
 * The results breakdown.
 *
 * Design notes, since this is the one real piece of data visualisation:
 *
 * - One series (this poll), so every bar wears the same accent. Giving each
 *   option its own hue would imply the colours carry meaning; they would not,
 *   and it makes the chart harder to read, not easier.
 * - Identity comes from the text label above each bar, never from colour, so
 *   the chart survives greyscale and colour-vision differences.
 * - Every bar is directly labelled with its count and percentage. With ten
 *   rows at most, the numbers *are* the content -- hiding them behind a hover
 *   tooltip would make the primary information unreachable on touch.
 * - The leading option is emphasised with font weight rather than a different
 *   colour, for the same reason.
 * - Counts tween rather than snap. A number jumping from 7 to 8 is genuinely
 *   easy to miss; the motion points the eye at the row that changed, which is
 *   the entire promise of a live page.
 * - Percentages are of *voters*, not selections, so a multiple-choice poll can
 *   legitimately total more than 100%.
 */
export function ResultsBars({ options, totalVotes, highlight = [] }) {
  const leadingVotes = options.reduce((max, o) => Math.max(max, o.votes), 0)

  return (
    <div className="results">
      {options.map((option) => {
        const percent = percentOf(option.votes, totalVotes)
        // A leading bar only counts as leading once someone has actually
        // voted; otherwise every option "leads" with zero.
        const isLeading = leadingVotes > 0 && option.votes === leadingVotes
        const isYours = highlight.includes(option.index)

        return (
          <div
            key={option.index}
            className={isLeading ? 'bar bar--leading' : 'bar'}
          >
            <div className="bar__head">
              <span className="bar__label">
                {option.text}
                {isYours && (
                  <>
                    {' '}
                    <span className="badge">your vote</span>
                  </>
                )}
              </span>
              <span className="bar__value">
                <AnimatedNumber value={percent} />% ·{' '}
                <AnimatedNumber value={option.votes} />{' '}
                {pluralize(option.votes, 'vote')}
              </span>
            </div>

            {/*
              The track is the accessible element, not the fill: a screen
              reader gets the number directly instead of trying to interpret a
              decorative div. aria-label repeats what sighted users read above.
            */}
            <div
              className="bar__track"
              role="img"
              aria-label={`${option.text}: ${option.votes} ${pluralize(
                option.votes,
                'vote',
              )}, ${percent} percent`}
            >
              <div
                className="bar__fill"
                data-zero={option.votes === 0 ? 'true' : 'false'}
                style={{ width: `${percent}%` }}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}
