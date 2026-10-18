# Changelog

## 0.0.1

Initial desktop release, built with mygo, React, and the siu Rust core.

- Manage MCP services and Skills in a Chinese desktop interface with light and dark themes.
- Detect installed Agents and assign resources through a resource × Agent matrix.
- Add and import MCP configurations, preserve individual arguments and credentials, and preview redacted configuration.
- Scan local folders and GitHub repositories for Skills; install, update, pause, restore, and remove them.
- Discover MCP servers through the official Registry and Skills through skills.sh, with persistent MCP Registry cache fallback.
- Preserve unrelated Agent settings, back up existing configuration, and report actual synchronization failures for retry.
- Package macOS, Windows, and Linux builds for x64 and ARM64, with platform-specific asset names and SHA-256 checksums.

See [verification.md](docs/verification.md) for local validation and [GitHub Actions](https://github.com/nicholasxjy/siu-gui/actions/workflows/build.yml) for release build results.
