import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import themeTokens from './src/theme/tokens.json' with { type: 'json' };
const tokenRule = (selector: string, mode: 'light' | 'dark') => ({
  selector,
  nodes: [
    { prop: 'color-scheme', value: mode },
    ...Object.entries(themeTokens[mode]).map(([prop, value]) => ({
      prop,
      value,
    })),
  ],
});

export default defineConfig({
  // 子路径部署（如 /jizhang 与默认官网共存）：生产构建由 Dockerfile 注入 VITE_BASE=/jizhang/。
  // 本地 dev 未设置 → "/"，开发与 e2e 的访问路径保持不变。
  base: process.env.VITE_BASE || '/',
  plugins: [react()],
  css: {
    postcss: {
      plugins: [
        {
          postcssPlugin: 'theme-tokens',
          Once(root) {
            root.walkComments((comment) => {
              const mode =
                comment.text.trim() === '@theme-light'
                  ? 'light'
                  : comment.text.trim() === '@theme-dark'
                    ? 'dark'
                    : undefined;
              if (!mode) return;
              const rule = tokenRule(
                mode === 'light' ? ':root' : "[data-theme='dark']",
                mode
              );
              comment.replaceWith({
                ...rule,
                source: comment.source,
                nodes: rule.nodes.map((node) => ({
                  ...node,
                  source: comment.source,
                })),
              });
            });
          },
        },
      ],
    },
  },
  server: {
    host: true, // 绑定 0.0.0.0：局域网内其他设备（如手机调试移动端）可通过本机 IP 访问
    port: 5600,
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY || 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
});
