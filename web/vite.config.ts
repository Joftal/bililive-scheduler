import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: '/scheduler/',
  server: {
    proxy: {
      '/scheduler/api': {
        target: 'http://localhost:9090',
        rewrite: (path) => path.replace(/^\/scheduler/, ''),
      },
    },
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
});
