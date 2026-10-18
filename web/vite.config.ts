import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 开发时把接口代理到 cmd/devgw（默认 127.0.0.1:8088），浏览器同源访问，不需要 CORS。
const gateway = process.env.WB_GATEWAY ?? 'http://127.0.0.1:8088';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/byai_platform': gateway,
      '/dev': gateway,
    },
  },
});
