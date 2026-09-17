import { useEffect, useRef, useState } from 'react'

import { Button } from './Button.jsx'

/**
 * Share URL with a copy button.
 *
 * navigator.clipboard is unavailable on insecure origins (anything that is not
 * https or localhost) and can be denied by permissions policy, so the failure
 * path is real, not theoretical. When it fails the URL is selected instead, so
 * the user can copy it manually -- better than an error that leaves them
 * stuck.
 */
export function CopyLink({ url }) {
  const [state, setState] = useState('idle') // idle | copied | failed
  const timerRef = useRef(null)
  const urlRef = useRef(null)

  // Clearing the timer on unmount stops setState firing on a component that no
  // longer exists, which is the classic React memory-leak warning.
  useEffect(() => () => clearTimeout(timerRef.current), [])

  const flash = (next) => {
    setState(next)
    clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => setState('idle'), 2000)
  }

  const handleCopy = async () => {
    try {
      if (!navigator.clipboard) throw new Error('Clipboard API unavailable')
      await navigator.clipboard.writeText(url)
      flash('copied')
    } catch {
      // Select the text so a manual copy is one keystroke away.
      const node = urlRef.current
      if (node) {
        const range = document.createRange()
        range.selectNodeContents(node)
        const selection = window.getSelection()
        selection?.removeAllRanges()
        selection?.addRange(range)
      }
      flash('failed')
    }
  }

  return (
    <div className="stack" style={{ '--gap': 'var(--s2)' }}>
      <div className="share">
        <code className="share__url" ref={urlRef}>
          {url}
        </code>
        <Button variant="secondary" onClick={handleCopy}>
          {state === 'copied' ? 'Copied' : 'Copy link'}
        </Button>
      </div>

      {/* aria-live so the confirmation is announced, not just shown. */}
      <span className="meta" role="status" aria-live="polite">
        {state === 'copied' && 'Link copied to your clipboard.'}
        {state === 'failed' && 'Could not copy automatically — the link is selected, press Ctrl+C.'}
      </span>
    </div>
  )
}
