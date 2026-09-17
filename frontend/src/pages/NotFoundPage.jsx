import { Link } from 'react-router-dom'

import { StateMessage } from '../components/StateMessage.jsx'

export function NotFoundPage() {
  return (
    <div className="page">
      <StateMessage
        title="Page not found"
        body="That address does not match anything in this app."
      >
        <Link className="btn btn--secondary" to="/">
          Go home
        </Link>
      </StateMessage>
    </div>
  )
}
