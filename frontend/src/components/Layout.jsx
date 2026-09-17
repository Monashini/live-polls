import { Link, Outlet } from 'react-router-dom'

import { useAuth } from '../auth/useAuth.js'
import { Button } from './Button.jsx'

/**
 * App shell. Rendered as a layout route so the header does not unmount and
 * remount between pages.
 *
 * The header deliberately shows nothing auth-related while the session is
 * still being verified -- flashing "Sign in" at someone who is signed in is a
 * small thing that makes an app feel broken.
 */
export function Layout() {
  const { isAuthenticated, isLoading, user, signOut } = useAuth()

  return (
    <>
      <header className="appbar">
        <div className="appbar__inner">
          <Link to="/" className="brand">
            <span className="brand__dot" aria-hidden="true" />
            Live Polls
          </Link>

          {!isLoading && (
            <nav className="row" style={{ gap: 'var(--s2)' }}>
              {isAuthenticated ? (
                <>
                  <span className="meta" title={user?.email}>
                    {user?.email}
                  </span>
                  <Button variant="ghost" size="sm" onClick={signOut}>
                    Sign out
                  </Button>
                </>
              ) : (
                <>
                  <Link className="btn btn--ghost btn--sm" to="/login">
                    Sign in
                  </Link>
                  <Link className="btn btn--primary btn--sm" to="/signup">
                    Sign up
                  </Link>
                </>
              )}
            </nav>
          )}
        </div>
      </header>

      <main>
        <Outlet />
      </main>
    </>
  )
}
