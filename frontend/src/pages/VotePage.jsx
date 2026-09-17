import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { ApiError } from '../api/client.js'
import * as pollsApi from '../api/polls.js'
import { Alert } from '../components/Alert.jsx'
import { Button } from '../components/Button.jsx'
import { ModeBadge, PollStatus } from '../components/PollStatus.jsx'
import { ResultsBars } from '../components/ResultsBars.jsx'
import { StateMessage } from '../components/StateMessage.jsx'
import { useAsyncData } from '../hooks/useAsyncData.js'
import { pluralize } from '../lib/format.js'

const VOTED_KEY = 'livepolls.voted'

/**
 * Remembering a vote locally is a UI convenience only: it lets a returning
 * visitor see results immediately instead of being shown a form the server
 * will reject. The actual enforcement is the unique index in MongoDB, keyed on
 * the HttpOnly cookie -- clearing this list does not buy anyone a second vote.
 */
function readVoted() {
  try {
    return JSON.parse(window.localStorage.getItem(VOTED_KEY) ?? '{}')
  } catch {
    return {}
  }
}

function rememberVote(slug, optionIndexes) {
  try {
    const voted = readVoted()
    voted[slug] = optionIndexes
    window.localStorage.setItem(VOTED_KEY, JSON.stringify(voted))
  } catch {
    // Storage unavailable (private mode). The vote still counted; the user
    // just will not see their own selection highlighted on a revisit.
  }
}

export function VotePage() {
  const { key } = useParams()

  const { data, error, loading, refetch, replace } = useAsyncData(
    (signal) => pollsApi.getPoll(key, { signal }),
    [key],
  )

  const [selected, setSelected] = useState([])
  const [submitting, setSubmitting] = useState(false)
  const [voteError, setVoteError] = useState(null)
  // Initialised from storage so a refresh does not re-offer the form.
  const [myVote, setMyVote] = useState(() => readVoted()[key] ?? null)

  // How this visitor came to be looking at results, which decides what we tell
  // them. "Your vote was counted" is only true for 'new' -- saying it after a
  // rejected duplicate would be a straight lie about what the server did.
  //   new       - submitted successfully just now
  //   duplicate - the server rejected this attempt, they had already voted
  //   previous  - we knew before they submitted anything
  const [voteOutcome, setVoteOutcome] = useState(() =>
    readVoted()[key] === undefined ? null : 'previous',
  )

  const poll = data?.poll

  function toggleOption(index, isMultiple) {
    setVoteError(null)
    setSelected((current) => {
      if (!isMultiple) return [index]
      return current.includes(index)
        ? current.filter((i) => i !== index)
        : [...current, index]
    })
  }

  async function handleVote(event) {
    event.preventDefault()

    if (selected.length === 0) {
      setVoteError('Pick at least one choice.')
      return
    }

    setSubmitting(true)
    setVoteError(null)
    try {
      const response = await pollsApi.castVote(key, selected)
      // The vote response contains the updated tally, so the results below
      // are correct without a second request.
      replace(response)
      rememberVote(key, selected)
      setMyVote(selected)
      setVoteOutcome('new')
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'ALREADY_VOTED') {
          // The server knows better than local storage does. Record it and
          // switch to results instead of leaving a form that cannot succeed.
          // The empty array means "voted, but we do not know for what" -- the
          // earlier selection is not recoverable, so nothing gets highlighted.
          rememberVote(key, [])
          setMyVote([])
          setVoteOutcome('duplicate')
          refetch()
        } else if (err.code === 'POLL_CLOSED') {
          refetch()
        }
        setVoteError(err.message)
      } else {
        setVoteError('Something went wrong. Please try again.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="page stack">
        <div className="skeleton" style={{ height: '2rem', width: '70%' }} />
        <div className="card stack">
          <div className="skeleton" style={{ height: '3rem' }} />
          <div className="skeleton" style={{ height: '3rem' }} />
          <div className="skeleton" style={{ height: '3rem' }} />
        </div>
      </div>
    )
  }

  if (error) {
    const notFound = error instanceof ApiError && error.status === 404
    return (
      <div className="page">
        <StateMessage
          title={notFound ? 'Poll not found' : 'Could not load this poll'}
          body={
            notFound
              ? 'This link may be wrong, or the poll may have been deleted.'
              : error.message
          }
          action={notFound ? undefined : 'Try again'}
          onAction={notFound ? undefined : refetch}
        />
      </div>
    )
  }

  const isMultiple = poll.mode === 'multiple'
  const hasVoted = myVote !== null
  const showForm = poll.acceptsVotes && !hasVoted

  return (
    <div className="page stack" style={{ '--gap': 'var(--s5)' }}>
      <div className="stack" style={{ '--gap': 'var(--s3)' }}>
        <div className="row" style={{ gap: 'var(--s2)' }}>
          <PollStatus poll={poll} />
          <ModeBadge mode={poll.mode} />
        </div>
        <h1>{poll.question}</h1>
        <p className="meta">
          {poll.totalVotes} {pluralize(poll.totalVotes, 'person has', 'people have')}{' '}
          voted
        </p>
      </div>

      {showForm ? (
        <form className="card stack" onSubmit={handleVote}>
          <Alert>{voteError}</Alert>

          <fieldset
            className="stack"
            style={{ border: 0, padding: 0, margin: 0, '--gap': 'var(--s2)' }}
          >
            <legend className="visually-hidden">
              {isMultiple ? 'Select one or more choices' : 'Select one choice'}
            </legend>

            {poll.options.map((option) => (
              <label className="choice" key={option.index}>
                <input
                  // Radios for single choice, checkboxes for multiple. Using
                  // the right control means the browser enforces the rule and
                  // screen readers announce it correctly, for free.
                  type={isMultiple ? 'checkbox' : 'radio'}
                  name="option"
                  checked={selected.includes(option.index)}
                  onChange={() => toggleOption(option.index, isMultiple)}
                />
                <span className="choice__text">{option.text}</span>
              </label>
            ))}
          </fieldset>

          <Button
            type="submit"
            block
            loading={submitting}
            disabled={selected.length === 0}
          >
            {submitting ? 'Submitting' : 'Submit vote'}
          </Button>

          <p className="meta">
            {isMultiple
              ? 'You can pick more than one. You can only vote once.'
              : 'You can only vote once.'}
          </p>
        </form>
      ) : (
        <div className="card stack">
          {voteOutcome === 'new' && (
            <Alert variant="success">Thanks — your vote was counted.</Alert>
          )}
          {(voteOutcome === 'duplicate' || voteOutcome === 'previous') && (
            <Alert variant="info">
              You have already voted on this poll. Here are the current results.
            </Alert>
          )}
          {!hasVoted && !poll.acceptsVotes && (
            <Alert variant="info">
              {poll.closed
                ? 'Voting on this poll has been closed.'
                : 'This poll has expired.'}{' '}
              Here are the final results.
            </Alert>
          )}

          <ResultsBars
            options={poll.options}
            totalVotes={poll.totalVotes}
            highlight={myVote ?? []}
          />

          <div className="row row--between">
            <span className="meta">
              {poll.totalVotes} {pluralize(poll.totalVotes, 'voter')}
              {isMultiple && ` · ${poll.totalSelections} selections`}
            </span>
            <Button variant="ghost" size="sm" onClick={refetch}>
              Refresh
            </Button>
          </div>
        </div>
      )}

      <p className="meta">
        <Link to={`/polls/${poll.slug}/results`}>Open the full results page</Link>
      </p>
    </div>
  )
}
