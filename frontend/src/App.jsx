import { Navigate, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './auth/AuthContext.jsx'
import { ProtectedRoute } from './auth/ProtectedRoute.jsx'
import { Layout } from './components/Layout.jsx'
import { CreatePollPage } from './pages/CreatePollPage.jsx'
import { DashboardPage } from './pages/DashboardPage.jsx'
import { LoginPage } from './pages/LoginPage.jsx'
import { NotFoundPage } from './pages/NotFoundPage.jsx'
import { ResultsPage } from './pages/ResultsPage.jsx'
import { SignupPage } from './pages/SignupPage.jsx'
import { VotePage } from './pages/VotePage.jsx'

/**
 * The whole route table in one place, so "is this page public?" is answered by
 * reading twenty lines rather than by hunting for guards inside components.
 *
 * Note which routes are NOT wrapped in ProtectedRoute: /p/:key and
 * /polls/:key/results. Voting and reading results have to work for someone who
 * has never signed in, which is the core of the brief.
 *
 * Share links use the short /p/:key form because those get pasted into chat
 * messages and read aloud.
 */
export function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<Navigate to="/dashboard" replace />} />

          <Route path="/login" element={<LoginPage />} />
          <Route path="/signup" element={<SignupPage />} />

          <Route path="/p/:key" element={<VotePage />} />
          <Route path="/polls/:key/results" element={<ResultsPage />} />

          <Route
            path="/dashboard"
            element={
              <ProtectedRoute>
                <DashboardPage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/polls/new"
            element={
              <ProtectedRoute>
                <CreatePollPage />
              </ProtectedRoute>
            }
          />

          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
