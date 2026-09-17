import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { ApiError } from '../api/client.js'
import * as pollsApi from '../api/polls.js'
import { Alert } from '../components/Alert.jsx'
import { Button } from '../components/Button.jsx'
import { CopyLink } from '../components/CopyLink.jsx'
import { Field } from '../components/Field.jsx'
import { shareUrlFor } from '../lib/format.js'
import {
  LIMITS,
  cleanOptions,
  validateOptions,
  validateQuestion,
} from '../lib/validation.js'

const BLANK_OPTIONS = ['', '']

export function CreatePollPage() {
  const navigate = useNavigate()

  const [question, setQuestion] = useState('')
  const [options, setOptions] = useState(BLANK_OPTIONS)
  const [mode, setMode] = useState('single')

  const [fieldErrors, setFieldErrors] = useState({})
  const [optionErrors, setOptionErrors] = useState({})
  const [formError, setFormError] = useState(null)
  const [submitting, setSubmitting] = useState(false)

  // Set once the poll exists. The form is replaced by the share panel rather
  // than navigating away, because the link is the whole point of creating a
  // poll and losing it behind a redirect would be hostile.
  const [created, setCreated] = useState(null)

  const questionLength = [...question.trim()].length

  function updateOption(index, value) {
    setOptions((current) => current.map((o, i) => (i === index ? value : o)))
    // Drop this row's error the moment it is edited; re-validated on submit.
    setOptionErrors((current) =>
      current[index] ? { ...current, [index]: undefined } : current,
    )
  }

  function addOption() {
    setOptions((current) =>
      current.length >= LIMITS.maxOptions ? current : [...current, ''],
    )
  }

  function removeOption(index) {
    setOptions((current) =>
      current.length <= LIMITS.minOptions
        ? current
        : current.filter((_, i) => i !== index),
    )
  }

  async function handleSubmit(event) {
    event.preventDefault()

    const errors = {}
    const questionError = validateQuestion(question)
    if (questionError) errors.question = questionError

    const { error: optionsError, fieldErrors: perOption } =
      validateOptions(options)

    setFieldErrors(errors)
    setOptionErrors(perOption)
    setFormError(optionsError)

    if (Object.keys(errors).length || optionsError) return

    setSubmitting(true)
    try {
      const response = await pollsApi.createPoll({
        question: question.trim(),
        options: cleanOptions(options),
        mode,
      })
      setCreated(response.poll)
    } catch (error) {
      if (error instanceof ApiError) {
        setFormError(error.message)
        if (error.fields) {
          // The server keys option problems as "options.2"; split those back
          // out so they land on the right input instead of all showing as one
          // form-level message.
          const serverFieldErrors = {}
          const serverOptionErrors = {}
          for (const [key, message] of Object.entries(error.fields)) {
            const match = key.match(/^options\.(\d+)$/)
            if (match) serverOptionErrors[Number(match[1])] = message
            else serverFieldErrors[key] = message
          }
          setFieldErrors(serverFieldErrors)
          setOptionErrors(serverOptionErrors)
        }
      } else {
        setFormError('Something went wrong. Please try again.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (created) {
    return (
      <div className="page stack" style={{ '--gap': 'var(--s5)' }}>
        <div className="stack" style={{ '--gap': 'var(--s2)' }}>
          <h1>Your poll is live</h1>
          <p className="muted">
            Share this link. Anyone who opens it can vote — no account needed.
          </p>
        </div>

        <div className="card stack">
          <h2>{created.question}</h2>
          <CopyLink url={shareUrlFor(created.slug)} />
        </div>

        <div className="row">
          <Link
            className="btn btn--primary"
            to={`/polls/${created.slug}/results`}
          >
            View results
          </Link>
          <Button
            variant="secondary"
            onClick={() => {
              // Reset every piece of form state, so "create another" starts
              // genuinely blank rather than inheriting the last poll.
              setCreated(null)
              setQuestion('')
              setOptions(BLANK_OPTIONS)
              setMode('single')
              setFieldErrors({})
              setOptionErrors({})
              setFormError(null)
            }}
          >
            Create another
          </Button>
          <Button variant="ghost" onClick={() => navigate('/dashboard')}>
            Back to dashboard
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="page stack" style={{ '--gap': 'var(--s5)' }}>
      <h1>New poll</h1>

      <form className="card stack" onSubmit={handleSubmit} noValidate>
        <Alert>{formError}</Alert>

        <Field
          label="Question"
          error={fieldErrors.question}
          hint={`${questionLength} / ${LIMITS.questionMax}`}
        >
          {(props) => (
            <textarea
              {...props}
              rows={2}
              value={question}
              onChange={(e) => {
                setQuestion(e.target.value)
                setFieldErrors((c) =>
                  c.question ? { ...c, question: undefined } : c,
                )
              }}
              placeholder="What should we build next?"
            />
          )}
        </Field>

        <fieldset className="stack" style={{ border: 0, padding: 0, margin: 0 }}>
          <legend className="field__label" style={{ paddingInline: 0 }}>
            Choices
          </legend>

          {options.map((option, index) => (
            <Field
              key={index}
              label={`Choice ${index + 1}`}
              error={optionErrors[index]}
            >
              {(props) => (
                <div className="row" style={{ gap: 'var(--s2)', flexWrap: 'nowrap' }}>
                  <input
                    {...props}
                    value={option}
                    onChange={(e) => updateOption(index, e.target.value)}
                    placeholder={index === 0 ? 'Dark mode' : 'CSV export'}
                    maxLength={LIMITS.optionMax}
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => removeOption(index)}
                    disabled={options.length <= LIMITS.minOptions}
                    aria-label={`Remove choice ${index + 1}`}
                  >
                    Remove
                  </Button>
                </div>
              )}
            </Field>
          ))}

          <div>
            <Button
              variant="secondary"
              size="sm"
              onClick={addOption}
              disabled={options.length >= LIMITS.maxOptions}
            >
              Add choice
            </Button>
            <span className="field__hint" style={{ marginLeft: 'var(--s3)' }}>
              {options.length} of {LIMITS.maxOptions}
            </span>
          </div>
        </fieldset>

        <fieldset className="stack" style={{ border: 0, padding: 0, margin: 0, '--gap': 'var(--s2)' }}>
          <legend className="field__label" style={{ paddingInline: 0 }}>
            How many choices can each person pick?
          </legend>

          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="single"
              checked={mode === 'single'}
              onChange={() => setMode('single')}
            />
            <span className="choice__text">
              One choice
              <br />
              <span className="meta">Voters pick exactly one option.</span>
            </span>
          </label>

          <label className="choice">
            <input
              type="radio"
              name="mode"
              value="multiple"
              checked={mode === 'multiple'}
              onChange={() => setMode('multiple')}
            />
            <span className="choice__text">
              Multiple choices
              <br />
              <span className="meta">
                Voters can pick several. Percentages are of voters, so they may
                add up to more than 100%.
              </span>
            </span>
          </label>
        </fieldset>

        <Button type="submit" block loading={submitting}>
          Create poll
        </Button>
      </form>
    </div>
  )
}
