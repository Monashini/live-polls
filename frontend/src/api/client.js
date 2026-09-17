// The one place the app talks to the network.
//
// Everything that is easy to get wrong per-call lives here instead: attaching
// the token, sending the voter cookie, turning the backend's error envelope
// into a real Error, and noticing when a session has expired.

const BASE_URL = (
  import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'
).replace(/\/$/, '')

/**
 * Builds the WebSocket URL for a path, derived from the same base as REST.
 *
 * Derived rather than configured separately: two env vars that must always
 * agree is two chances to get a deployment wrong, and "the API works but the
 * live feed does not" is a confusing way to find out. http -> ws and
 * https -> wss, so TLS carries over automatically.
 */
export function socketURL(path) {
  return `${BASE_URL.replace(/^http/, 'ws')}${path}`
}

/**
 * ApiError carries the backend's structured error so a form can highlight the
 * exact field that failed. A plain Error would flatten all of that into a
 * string that the UI would then have to parse back out.
 */
export class ApiError extends Error {
  constructor(status, code, message, fields) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.fields = fields ?? null
  }

  /** True when the failure was the network itself, not an HTTP response. */
  get isNetwork() {
    return this.status === 0
  }
}

// Module-level rather than passed through every call: a component should never
// have to know a token exists in order to make a request.
let accessToken = null
let onUnauthorized = null

export function setAccessToken(token) {
  accessToken = token ?? null
}

/**
 * Registers what to do when an *authenticated* request is rejected. The auth
 * provider uses it to clear the session so the user lands on the login page
 * instead of staring at a screen that silently fails.
 */
export function setUnauthorizedHandler(handler) {
  onUnauthorized = handler
}

export async function request(path, { method = 'GET', body, signal } = {}) {
  const headers = {}

  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }

  // Captured before the await: if the token is cleared while this request is
  // in flight, we still need to know whether *this* request was authenticated.
  const sentToken = Boolean(accessToken)
  if (sentToken) {
    headers.Authorization = `Bearer ${accessToken}`
  }

  let response
  try {
    response = await fetch(`${BASE_URL}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      // Required for the anonymous voter cookie the backend sets on /vote.
      // Without it the browser neither stores nor returns that cookie, and
      // every visit would look like a brand new voter.
      credentials: 'include',
      signal,
    })
  } catch (cause) {
    // An aborted request is a normal part of cleanup, not a failure to report.
    if (cause?.name === 'AbortError') throw cause

    throw new ApiError(
      0,
      'NETWORK_ERROR',
      'Could not reach the server. Check your connection and try again.',
      null,
    )
  }

  // 204 No Content (used by DELETE) has no body to parse.
  if (response.status === 204) return null

  // Read as text first: an error page from a proxy is not JSON, and calling
  // response.json() on it throws something unhelpful.
  const raw = await response.text()
  let payload = null
  if (raw) {
    try {
      payload = JSON.parse(raw)
    } catch {
      payload = null
    }
  }

  if (!response.ok) {
    // Only an authenticated request getting a 401 means "your session died".
    // A 401 from the login form just means the password was wrong, and
    // treating that as a session expiry would bounce the user mid-login.
    if (response.status === 401 && sentToken && onUnauthorized) {
      onUnauthorized()
    }

    const envelope = payload?.error
    throw new ApiError(
      response.status,
      envelope?.code ?? 'UNKNOWN_ERROR',
      envelope?.message ?? 'Something went wrong. Please try again.',
      envelope?.fields,
    )
  }

  return payload
}
