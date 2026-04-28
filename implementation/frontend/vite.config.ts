import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 6006,
    strictPort: true,
    host: '0.0.0.0',
    // Dev is bound to 0.0.0.0 for tunnel / cloud URLs; per-host lists miss new hostnames. Allow any Host.
    // Set VITE_STRICT_HOSTS=1 to only allow SeetaCloud-style suffixes + VITE_ALLOWED_HOSTS (comma-separated).
    allowedHosts:
      process.env.VITE_STRICT_HOSTS === '1'
        ? [
            '.bjb1.seetacloud.com',
            '.seetacloud.com',
            ...(process.env.VITE_ALLOWED_HOSTS?.split(',')
              .map((h) => h.trim())
              .filter(Boolean) ?? []),
          ]
        : true,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  preview: {
    host: '0.0.0.0',
    allowedHosts:
      process.env.VITE_STRICT_HOSTS === '1'
        ? [
            '.bjb1.seetacloud.com',
            '.seetacloud.com',
            ...(process.env.VITE_ALLOWED_HOSTS?.split(',')
              .map((h) => h.trim())
              .filter(Boolean) ?? []),
          ]
        : true,
  },
})
