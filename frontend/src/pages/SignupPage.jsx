import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { ApiError } from '../api/client.js'
import { useAuth } from '../auth/useAuth.js'
import { Alert } from '../components/Alert.jsx'
import { Button } from '../components/Button.jsx'
import { Field } from '../components/Field.jsx'
import { LIMITS, validateEmail, validatePassword } from '../lib/validation.js'

export function SignupPage() {
  const { register } = useAuth()
  const navigate = useNavigate()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [fieldErrors, setFieldErrors] = useState({})
  const [formError, setFormError] = useState(null)
  const [submitting, setSubmitting] = useState(false)

  // Clears an inline error as soon as the user starts fixing it.
  const clearError = (name) =>
    setFieldErrors((current) =>
      current[name] ? { ...current, [name]: undefined } : current,
    )

  async function handleSubmit(event) {
    event.preventDefault()

    const errors = {}
    const emailError = validateEmail(email)
    const passwordError = validatePassword(password)
    if (emailError) errors.email = emailError
    if (passwordError) errors.password = passwordError

    setFieldErrors(errors)
    setFormError(null)
    if (Object.keys(errors).length) return

    setSubmitting(true)
    try {
      await register(email, password)
      navigate('/dashboard', { replace: true })
    } catch (error) {
      if (error instanceof ApiError) {
        setFormError(error.message)
        // The backend's `fields` map is keyed by field name, which lines up
        // with the state here, so a server-side rule lands on the right input
        // without any per-field translation.
        if (error.fields) setFieldErrors(error.fields)
      } else {
        setFormError('Something went wrong. Please try again.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="page stack" style={{ '--gap': 'var(--s5)' }}>
      <div className="stack" style={{ '--gap': 'var(--s2)' }}>
        <h1>Create an account</h1>
        <p className="muted">
          You need an account to create polls. Voting never requires one.
        </p>
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

        <Field
          label="Password"
          error={fieldErrors.password}
          hint={`${LIMITS.passwordMin}–${LIMITS.passwordMax} characters`}
        >
          {(props) => (
            <input
              {...props}
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value)
                clearError('password')
              }}
            />
          )}
        </Field>

        <Button type="submit" block loading={submitting}>
          Create account
        </Button>
      </form>

      <p className="muted">
        Already have an account? <Link to="/login">Sign in</Link>.
      </p>
    </div>
  )
}
