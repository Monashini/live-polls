import { useCallback, useEffect, useState } from 'react'

/**
 * Fetch-on-mount with loading, error, refetch and cancellation.
 *
 * Every page that loads data needs the same four things, and writing them by
 * hand each time is how a stray setState-after-unmount or an unhandled
 * rejection sneaks in. Centralising it means those bugs are fixed once.
 *
 * `loader` receives an AbortSignal and must pass it to the API call, so
 * navigating away mid-request actually cancels the request rather than just
 * ignoring its result.
 *
 * Deliberately not a data-fetching library. TanStack Query would give caching
 * and deduplication, but this app has three read screens and no shared cache
 * to speak of -- the dependency would cost more to explain than it saves.
 */
export function useAsyncData(loader, deps = []) {
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(true)
  // Bumping this re-runs the effect, which is how refetch() works without
  // duplicating the request logic outside the effect.
  const [reloadToken, setReloadToken] = useState(0)

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const runLoader = useCallback(loader, deps)

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false

    setLoading(true)
    setError(null)

    runLoader(controller.signal)
      .then((result) => {
        if (cancelled) return
        setData(result)
      })
      .catch((err) => {
        // An abort is us cancelling on purpose, not a failure to surface.
        if (cancelled || err?.name === 'AbortError') return
        setError(err)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [runLoader, reloadToken])

  const refetch = useCallback(() => setReloadToken((n) => n + 1), [])

  // Lets a page apply a server response it already has (e.g. the poll returned
  // by a successful vote) without a second round trip.
  const replace = useCallback((next) => setData(next), [])

  return { data, error, loading, refetch, replace }
}
