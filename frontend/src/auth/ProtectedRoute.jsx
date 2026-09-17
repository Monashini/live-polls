import { Navigate, useLocation } from 'react-router-dom'

import { FullPageSpinner } from '../components/Spinner.jsx'
import { useAuth } from './useAuth.js'

/**
 * Gate for routes that need an account.
 *
 * The loading branch matters: without it, a refresh on /dashboard would show
 * the login page for a frame before the stored token finished verifying.
 *
 * The visited path is stashed in navigation state so signing in returns the
 * user where they were headed rather than dumping everyone on the dashboard.
 */
export function ProtectedRoute({ children }) {
  const { isLoading, isAuthenticated } = useAuth()
  const location = useLocation()

  if (isLoading) {
    return <FullPageSpinner label="Checking your session" />
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  return children
}
