import { request } from './client.js'

// One function per endpoint. Pages call these, never `request` directly, so
// the set of things the app can ask the server is enumerable in one file.

export function signup(email, password) {
  return request('/api/auth/signup', {
    method: 'POST',
    body: { email, password },
  })
}

export function login(email, password) {
  return request('/api/auth/login', {
    method: 'POST',
    body: { email, password },
  })
}

export function me({ signal } = {}) {
  return request('/api/auth/me', { signal })
}
