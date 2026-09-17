import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { App } from './App.jsx'
import './index.css'

// StrictMode double-invokes effects in development on purpose. It is left on
// because it surfaces exactly the bug this app is prone to: a fetch that does
// not clean up after itself. If a screen misbehaves only in dev, that is the
// warning working, not a false positive.
createRoot(document.getElementById('root')).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
)
