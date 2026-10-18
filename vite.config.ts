import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  define: {
    __SIU_BROWSER_TEST__: JSON.stringify(
      process.env.SIU_GUI_BROWSER_TEST === "1",
    ),
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy:
      process.env.SIU_GUI_BROWSER_TEST === "1"
        ? { "/__test": "http://127.0.0.1:47832" }
        : undefined,
  },
  build: { target: ["chrome109", "safari15.4", "firefox115"] },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
