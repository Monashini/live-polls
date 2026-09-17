import { useContext } from 'react'

import { AuthContext } from './AuthContext.jsx'

/**
 * Lives in its own file so components import a hook rather than a context
 * object, and so the throw below guarantees a missing provider fails loudly
 * at the point of use instead of silently reading null.
 */
export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) {
    throw new Error('useAuth must be used inside <AuthProvider>')
  }
  return value
}
