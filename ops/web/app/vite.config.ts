import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The SPA is served by the Go panel under /app/. In dev, Vite proxies the API,
// SSE and auth routes to the running panel on :8080 so cookies + CSRF work.
export default defineConfig({
  plugins: [react()],
  base: "/app/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/login": { target: "http://localhost:8080", changeOrigin: false },
      "/logout": { target: "http://localhost:8080", changeOrigin: false },
      "/jobs": { target: "http://localhost:8080", changeOrigin: false },
    },
  },
});
