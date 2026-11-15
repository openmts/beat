import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:9099",
      "/healthz": "http://127.0.0.1:9099",
      "/readyz": "http://127.0.0.1:9099",
    },
  },
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
});

