import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      "/api": "http://127.0.0.1:8081",
      "/livez": "http://127.0.0.1:8081",
      "/readyz": "http://127.0.0.1:8081",
    },
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 650,
  },
});
