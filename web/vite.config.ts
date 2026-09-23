import { fileURLToPath, URL } from 'node:url'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// https://vite.dev/config/
// Stamped into the bundle so a stored chat thread can tell whether it was
// written by this build. Without it, a conversation survives a rebuild in
// sessionStorage and old answers sit beside new ones with nothing to say they
// came from different code — which reads as "the fix did not work".
const BUILD_ID = JSON.stringify(String(Date.now()))

export default defineConfig({
  define: { __BUILD_ID__: BUILD_ID },
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // The Go agent serves the whole API under /v1 on :8090.
      '/v1': {
        target: 'http://localhost:8090',
        changeOrigin: true,
        // SSE must not be buffered or the run detail page arrives all at once.
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            if (proxyRes.headers['content-type']?.includes('text/event-stream')) {
              proxyRes.headers['cache-control'] = 'no-cache, no-transform'
            }
          })
        },
      },
    },
  },
})
