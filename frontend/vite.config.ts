import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8999',
        changeOrigin: true,
      }
    }
  },
  build: {
    outDir: 'dist',
    target: 'es2022',
  }
})
