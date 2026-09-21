import path from "path"
import tailwindcss from "@tailwindcss/vite"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [/* your react plugin */, tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
  server: { proxy: { "/api": "http://localhost:8080" } },
})