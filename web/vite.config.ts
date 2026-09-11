import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    target: 'es2020',          // covers the older Android WebViews common in Kenya
    sourcemap: true,
    reportCompressedSize: true,
  },
  server: {
    port: 5173,
    strictPort: true,
  },
  preview: { port: 4173 },
});
