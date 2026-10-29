import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import path from 'path'

const server = process.env.TRELLIS_SERVER ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': { target: server, changeOrigin: true },
      '/login': { target: server, changeOrigin: true, bypass: (req) => (req.method === 'POST' ? undefined : '/index.html') },
    },
  },
})
