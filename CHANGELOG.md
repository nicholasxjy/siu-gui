# Changelog

## 0.0.3

- Redesign the application icon around an S-shaped route connecting a resource module to an Agent, with an iris-blue tile, mint accent and transparent corners.
- Use the same SVG design for the desktop icon, in-app brand and browser favicon; package native macOS, Windows and Linux icon sizes from the 1024px PNG.
- Generate the PNG directly from the SVG with `bun run generate:icon`, keeping future icon changes consistent across all surfaces.

## 0.0.2

- Follow native system proxies by default, including proxy environment variables and native PAC/WPAD resolution on macOS and Windows.
- Add persistent network settings for system, manual HTTP/HTTPS/SOCKS5, and direct connections, with authentication and hostname/IP/CIDR bypass rules. Saved changes apply to subsequent requests without restarting.
- Apply the same proxy policy to GitHub Skill downloads, MCP Registry, skills.sh, npm, and PyPI. Re-evaluate routing after redirects and separate connection pools when proxies change.
- Bound stalled downloads with a complete-request deadline and limited worker capacity, so a blocked response body cannot leave a Skill update loading indefinitely.
- Release failed update dialogs promptly while refreshing actual state in the background, preserve error details, and reject stale recovery responses.
- Extend local proxy, protocol, UI, and live-source coverage, and update bundled desktop dependency licenses. Docker image transfers still require Docker engine proxy configuration.

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
