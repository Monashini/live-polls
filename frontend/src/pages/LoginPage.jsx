import { useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { ApiError } from '../api/client.js'
import { useAuth } from '../auth/useAuth.js'
import { Alert } from '../components/Alert.jsx'
import { Button } from '../components/Button.jsx'
import { Field } from '../components/Field.jsx'
import { validateEmail } from '../lib/validation.js'

export function LoginPage() {
  const { signIn } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [fieldErrors, setFieldErrors] = useState({})
  const [formError, setFormError] = useState(null)
  const [submitting, setSubmitting] = useState(false)

  // Where the user was heading before ProtectedRoute intercepted them.
  const redirectTo = location.state?.from ?? '/dashboard'

  // Clears an inline error as soon as the user starts fixing it. Leaving
  // "is required" under a field someone is actively typing into reads as the
  // form being broken.
  const clearError = (name) =>
    setFieldErrors((current) =>
      current[name] ? { ...current, [name]: undefined } : current,
    )

  async function handleSubmit(event) {
    event.preventDefault()

    // Client checks first, purely so the user is not made to wait on a round
    // trip to learn they left a field blank. The server checks again.
    const errors = {}
    const emailError = validateEmail(email)
    if (emailError) errors.email = emailError
    if (!password) errors.password = 'is required'

    setFieldErrors(errors)
    setFormError(null)
    if (Object.keys(errors).length) return

    setSubmitting(true)
    try {
      await signIn(email, password)
      navigate(redirectTo, { replace: true })
    } catch (error) {
      if (error instanceof ApiError) {
        // The backend returns one generic message for every login failure so
        // it cannot be used to discover which emails are registered. The UI
        // shows exactly that, without embellishing it.
        setFormError(error.message)
        if (error.fields) setFieldErrors(error.fields)
      } else {
        setFormError('Something went wrong. Please try again.')
      }
    } finally {
      // In finally, so the button re-enables even if navigation throws.
      setSubmitting(false)
    }
  }

  return (
    <div className="page stack" style={{ '--gap': 'var(--s5)' }}>
      <div className="stack" style={{ '--gap': 'var(--s2)' }}>
        <h1>Welcome back</h1>
        <p className="muted">Sign in to create and manage your polls.</p>
      </div>

      <form className="card stack" onSubmit={handleSubmit} noValidate>
        <Alert>{formError}</Alert>

        <Field label="Email" error={fieldErrors.email}>
          {(props) => (
            <input
              {...props}
              type="email"
              autoComplete="email"
              value={email}
              onChange={(e) => {
                setEmail(e.target.value)
                clearError('email')
              }}
              placeholder="you@example.com"
            />
          )}
        </Field>

        <Field label="Password" error={fieldErrors.password}>
          {(props) => (
            <input
              {...props}
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value)
                clearError('password')
              }}
            />
          )}
        </Field>

        <Button type="submit" block loading={submitting}>
          Sign in
        </Button>
      </form>

      <p className="muted">
        No account yet? <Link to="/signup">Create one</Link>.
      </p>
    </div>
  )
}
