import { request } from './client.js'

export function createPoll({ question, options, mode, expiresAt }) {
  return request('/api/polls', {
    method: 'POST',
    body: {
      question,
      options,
      mode,
      // Omitted entirely when absent: sending expiresAt: null would be a
      // different statement from not mentioning it.
      ...(expiresAt ? { expiresAt } : {}),
    },
  })
}

export function listMyPolls({ signal } = {}) {
  return request('/api/polls', { signal })
}

/** `key` is either the poll's id or its share slug; the backend accepts both. */
export function getPoll(key, { signal } = {}) {
  return request(`/api/polls/${encodeURIComponent(key)}`, { signal })
}

export function castVote(key, optionIndexes) {
  return request(`/api/polls/${encodeURIComponent(key)}/vote`, {
    method: 'POST',
    body: { optionIndexes },
  })
}

export function closePoll(key) {
  return request(`/api/polls/${encodeURIComponent(key)}/close`, {
    method: 'PATCH',
  })
}

export function deletePoll(key) {
  return request(`/api/polls/${encodeURIComponent(key)}`, { method: 'DELETE' })
}
