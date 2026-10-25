import { defineConfig } from 'vite';
import solidPlugin from 'vite-plugin-solid';

export default defineConfig({
  plugins: [solidPlugin()],
  base: '/webui/',
  server: {
    port: 3000,
    proxy: {
      '/graphql': {
        target: process.env.BITMAGNET_URL || 'http://localhost:3333',
        changeOrigin: true,
      },
    },
  },
  build: {
    target: 'esnext',
  },
});
