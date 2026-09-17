import { createContext, useCallback, useEffect, useMemo, useState } from 'react'

import * as authApi from '../api/auth.js'
import { setAccessToken, setUnauthorizedHandler } from '../api/client.js'

const STORAGE_KEY = 'livepolls.token'

export const AuthContext = createContext(null)

/**
 * Why localStorage for the token, honestly:
 *
 * It is readable by any script on the page, so a successful XSS can steal it.
 * The airtight alternative is an HttpOnly cookie, which JavaScript cannot
 * read at all -- but a cookie is attached automatically to every request,
 * which reintroduces CSRF and needs its own defence. Given the API takes the
 * token in an Authorization header (so authenticated routes are CSRF-proof by
 * construction), localStorage is the deliberate trade: XSS is prevented by
 * React escaping output and by never using dangerouslySetInnerHTML.
 */
function readStoredToken() {
  try {
    return window.localStorage.getItem(STORAGE_KEY)
  } catch {
    // Private mode and blocked site data both throw here. A session that
    // lasts only as long as the tab is better than a crash on boot.
    return null
  }
}

function writeStoredToken(token) {
  try {
    if (token) window.localStorage.setItem(STORAGE_KEY, token)
    else window.localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Ignored for the same reason as above.
  }
}

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null)
  // Three states, not a boolean. "Not logged in" and "we don't know yet" look
  // identical to a boolean, and conflating them makes protected routes flash
  // the login page on every refresh before the token is verified.
  const [status, setStatus] = useState('loading')

  const signOut = useCallback(() => {
    setAccessToken(null)
    writeStoredToken(null)
    setUser(null)
    setStatus('anonymous')
  }, [])

  // The API client calls this when an authenticated request comes back 401,
  // which is how an expired token gets cleaned up no matter which page
  // discovered it.
  useEffect(() => {
    setUnauthorizedHandler(signOut)
    return () => setUnauthorizedHandler(null)
  }, [signOut])

  // On boot, a stored token is only a claim. It is verified against
  // /api/auth/me before the app treats anyone as signed in -- the token may
  // have expired while the tab was closed.
  useEffect(() => {
    const token = readStoredToken()
    if (!token) {
      setStatus('anonymous')
      return
    }

    setAccessToken(token)

    const controller = new AbortController()
    let cancelled = false

    authApi
      .me({ signal: controller.signal })
      .then((data) => {
        if (cancelled) return
        setUser(data.user)
        setStatus('authenticated')
      })
      .catch((error) => {
        if (cancelled || error?.name === 'AbortError') return
        // A 401 already triggered signOut through the handler above. Any other
        // failure (server down) also means we cannot claim to be signed in.
        setAccessToken(null)
        writeStoredToken(null)
        setUser(null)
        setStatus('anonymous')
      })

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [])

  const applySession = useCallback((data) => {
    setAccessToken(data.token)
    writeStoredToken(data.token)
    setUser(data.user)
    setStatus('authenticated')
    return data.user
  }, [])

  const signIn = useCallback(
    async (email, password) => applySession(await authApi.login(email, password)),
    [applySession],
  )

  const register = useCallback(
    async (email, password) => applySession(await authApi.signup(email, password)),
    [applySession],
  )

  const value = useMemo(
    () => ({
      user,
      status,
      isAuthenticated: status === 'authenticated',
      isLoading: status === 'loading',
      signIn,
      register,
      signOut,
    }),
    [user, status, signIn, register, signOut],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
