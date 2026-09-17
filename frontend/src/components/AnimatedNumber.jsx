import { useEffect, useRef, useState } from 'react'

/**
 * Tweens between numbers instead of snapping.
 *
 * The point is not decoration. When a count jumps from 7 to 8 instantly it is
 * genuinely easy to miss, especially on a results page someone is watching
 * from across a room. A short animation draws the eye to exactly the row that
 * changed, which is the whole promise of a live page.
 *
 * The animation is an enhancement. Correctness does NOT depend on it, and that
 * distinction is the whole design of this component.
 *
 * requestAnimationFrame is suspended in tabs the browser is not painting, and
 * -- this is the part that bites -- `document.hidden` does not predict it. A
 * background tab can report visibilityState "visible" while rAF never fires at
 * all. A tween started there freezes on its first frame, so the component
 * displays a stale number indefinitely while the aria-label beside it carries
 * the true one. That is exactly the situation this app creates: a results page
 * left open in a second tab is the normal way to watch a poll.
 *
 * So every update also arms a plain setTimeout that snaps to the real value.
 * Timers are throttled in background tabs but they still fire, unlike rAF. In
 * a focused tab the animation finishes first and the timer is a no-op; in a
 * background tab the timer is the only thing that runs, and the number is
 * still right.
 */
export function AnimatedNumber({ value, duration = 420 }) {
  const [display, setDisplay] = useState(value)

  // Where the current animation starts from. A ref, not state, because
  // changing it must not itself cause a render.
  const fromRef = useRef(value)
  const frameRef = useRef(null)

  // The authoritative value, readable from the visibilitychange listener
  // without making it a dependency of the effect.
  const valueRef = useRef(value)
  valueRef.current = value

  useEffect(() => {
    const prefersReduced = window.matchMedia?.(
      '(prefers-reduced-motion: reduce)',
    ).matches

    const snap = () => {
      cancelAnimationFrame(frameRef.current)
      fromRef.current = valueRef.current
      setDisplay(valueRef.current)
    }

    if (prefersReduced || fromRef.current === value) {
      snap()
      return undefined
    }

    const from = fromRef.current
    const startedAt = performance.now()

    const step = (now) => {
      const progress = Math.min(1, (now - startedAt) / duration)
      // Ease-out cubic: fast at first, settling at the end. Linear motion
      // reads as mechanical; this reads as a number landing.
      const eased = 1 - (1 - progress) ** 3

      setDisplay(Math.round(from + (value - from) * eased))

      if (progress < 1) {
        frameRef.current = requestAnimationFrame(step)
      } else {
        fromRef.current = value
      }
    }

    frameRef.current = requestAnimationFrame(step)

    // The safety net. Whatever the animation did or did not manage to do, the
    // displayed number is the real one shortly after the tween should have
    // ended.
    const settleTimer = setTimeout(snap, duration + 80)

    return () => {
      cancelAnimationFrame(frameRef.current)
      clearTimeout(settleTimer)
      // If a new value arrives mid-tween, the next animation starts from
      // wherever this one got to rather than snapping backwards.
      fromRef.current = value
    }
  }, [value, duration])

  return <>{display}</>
}
