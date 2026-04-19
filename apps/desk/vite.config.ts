import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Vite config for the Permafrost Trading Desk.
// Dev server proxies /v1/* to the local permafrostd on :8080 so the
// UI works against `make up` without operators having to configure CORS.
export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/v1': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  // Build into apps/desk/dist; the daemon's static-asset serve (a
  // future PR) will go:embed this directory and host it under /ui.
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
  },
});
