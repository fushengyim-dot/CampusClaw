import { defineConfig } from 'vite';

const apiTarget = `http://127.0.0.1:${process.env.API_PORT || '18080'}`;

export default defineConfig({
  server: { proxy: { '/api': apiTarget, '/health': apiTarget } },
  preview: { proxy: { '/api': apiTarget, '/health': apiTarget } },
});
