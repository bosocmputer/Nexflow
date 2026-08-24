import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8090',
        changeOrigin: true,
      },
      '/webhook': {
        target: 'http://localhost:8090',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (id.includes('/recharts/') || id.includes('/d3-')) return 'charts-vendor'
          if (id.includes('/@radix-ui/') || id.includes('/cmdk/') || id.includes('/vaul/')) return 'ui-vendor'
          if (id.includes('/lucide-react/')) return 'icons-vendor'
          if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/react-router') || id.includes('/zustand/') ||
              id.includes('/scheduler/') || id.includes('/use-sync-external-store/') || id.includes('/@remix-run/')) return 'react-vendor'
          // Let Rollup place the remaining dependencies. Forcing every other
          // package into one vendor chunk creates a vendor <-> react-vendor
          // cycle for packages which re-export React-based entry points.
          return undefined
        },
      },
    },
  },
})
