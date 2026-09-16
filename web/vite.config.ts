import react from '@vitejs/plugin-react'
import { defineConfig, type ProxyOptions } from 'vite'
import path from 'path'

// The dev server serves only the bundle, so every /api and /events call has to
// reach the running daemon or `pnpm dev` renders an app with no data. Point
// TRELLIS_DAEMON at a different address when the daemon is not on its default port.
const daemon = process.env.TRELLIS_DAEMON ?? 'http://127.0.0.1:7788'

// The daemon hands out its session cookie only on GET / with a token, which the
// dev server never serves. Set TRELLIS_TOKEN (the value `trellis daemon status`
// prints) and the proxy authenticates each call with the header instead.
const token = process.env.TRELLIS_TOKEN

// The daemon refuses any write whose Origin is not its own, which a browser on
// the dev server's port never is. changeOrigin only rewrites Host, so the
// proxy restates Origin as the daemon's before forwarding.
const daemonOrigin = new URL(daemon).origin

const proxy = (extra: ProxyOptions = {}): ProxyOptions => ({
  target: daemon,
  changeOrigin: true,
  configure: (instance) => {
    instance.on('proxyReq', (request) => {
      if (request.getHeader('origin')) request.setHeader('origin', daemonOrigin)
      if (token) request.setHeader('X-Trellis-Token', token)
    })
  },
  ...extra,
})

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': proxy(),
      '/events': proxy({ ws: true }),
    },
  },
})
