import { defineConfig } from "mygo-cli";
import pkg from "./package.json" with { type: "json" };

export default defineConfig({
  name: "Siu",
  identifier: "io.github.nicholasxjy.siu",
  version: pkg.version,

  devUrl: "http://127.0.0.1:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",

  icon: "resources/icon.png",
});
