import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  // 子路径部署（如 /jizhang 与默认官网共存）：生产构建由 Dockerfile 注入 VITE_BASE=/jizhang/。
  // 本地 dev 未设置 → "/"，开发与 e2e 的访问路径保持不变。
  base: process.env.VITE_BASE || '/',
  plugins: [react()],
  server: {
    port: 5600,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
});
