import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// No dev proxy on purpose.
//
// Proxying /api through Vite would make the frontend and backend same-origin
// in development, which hides CORS and cookie problems until deployment --
// exactly when they are most expensive to discover. Talking to the real
// backend origin from the start means dev and production exercise the same
// code path.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
  },
})
