# Agent icon sources

Checked 2026-11-08 against official sites and the [LobeHub Icons repository](https://github.com/lobehub/lobe-icons). The app uses recognizable product marks for its existing agents; this is not a quantitative popularity ranking. SVGs are bundled locally in `resources/agents`, with no runtime CDN or added dependency.

LobeHub assets are pinned to [`ea434b96e75813517b377825e01c0df61dee8836`](https://github.com/lobehub/lobe-icons/tree/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons). The following filenames refer to that revision's `packages/static-svg/icons/` directory unless an official URL is given.

| Agent | Local SVG | Source | Palette |
| --- | --- | --- | --- |
| Claude Code | `claudecode.svg` | [`claudecode-color.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/claudecode-color.svg) | Pixel mascot, `#D97757` |
| Claude Desktop | `claude.svg` | [`claude-color.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/claude-color.svg) | Claude spark, `#D97757` |
| Codex | `codex.svg` | [`codex-color.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/codex-color.svg) | Product symbol, `#3941FF`, `#7A9DFF`, `#B1A7FF` |
| Gemini CLI | `gemini.svg` | [`geminicli-color.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/geminicli-color.svg) | Terminal symbol, `#207CFE`, `#B381DD`, `#EE4D5D`, `#1E1E2E` |
| Cursor | `cursor.svg` | [`cursor.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/cursor.svg) | Monochrome cube |
| OpenCode | `opencode.svg` | [`opencode.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/opencode.svg) | Monochrome symbol |
| GitHub Copilot CLI | `copilot.svg` | [`githubcopilot.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/githubcopilot.svg) | Goggles/robot mark; neutral icon, subtle violet surface |
| Droid | `droid.svg` | [Official Factory favicon](https://factory.ai/favicon.svg) | Original `#FAFAFA` on `#020202`; orange surrounding tint |
| Pi | `pi.svg` | [Official Pi logo](https://pi.dev/logo-auto.svg), [press kit](https://pi.dev/press-kit) | `#F09082`, `#4D9ABF`, `#F1BE58`; cropped only the viewBox to an origin of (165, 165) and a size of 470 × 470 to remove padding |
| Grok | `grok.svg` | [`grok.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/grok.svg) | Monochrome symbol |
| Antigravity | `antigravity.svg` | [`antigravity-color.svg`](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/packages/static-svg/icons/antigravity-color.svg) | Original multicolor symbol; blue surrounding tint |
| Shared Skills directory (`agents`) | Installed Lucide `Folder` | Existing `lucide-react` dependency | Application neutral foreground/background; this target is a shared directory, not a separate agent brand |

## Source checks and theme choices

- Pi's [official repository](https://github.com/earendil-works/pi) establishes the coding-agent identity. Its three-color block mark differs from LobeHub's unrelated Pi asset; the latter is not used.
- GitHub Copilot uses the GitHub goggles mark, rather than Microsoft Copilot's multicolor loop. GitHub also publishes [an official Octicon](https://raw.githubusercontent.com/primer/octicons/main/icons/copilot-24.svg).
- Factory's [official site](https://factory.com/) uses orange `#EF6F2E` in its CSS; this is a website accent, not an orange fill in the favicon or a verified separate Droid palette. No separate Droid-specific SVG was established.
- Antigravity's [official press page](https://antigravity.google/press) distributes the same multicolor identity, including [this SVG](https://antigravity.google/assets/image/brand/antigravity-icon__full-color.svg).
- Cursor's [official brand page](https://cursor.com/brand) confirms its cube identity. Grok's official pages returned 403 during research; its compact symbol was verified through the pinned distributor.
- Light/dark avatar surfaces are application palette choices derived from the artwork, not claims of official brand specifications. Original multicolor artwork is preserved; only monochrome marks are inverted on dark surfaces. Visible agent names remain the accessible labels; images are decorative and non-draggable.

## Attribution

[LobeHub's MIT notice](https://raw.githubusercontent.com/lobehub/lobe-icons/ea434b96e75813517b377825e01c0df61dee8836/LICENSE) and [Pi's MIT notice](https://raw.githubusercontent.com/earendil-works/pi/main/LICENSE) are retained as `resources/agents/LICENSE.lobehub` and `LICENSE.pi`, and included by `tools/licenses.ts`. Factory's favicon remains an official brand asset; public availability does not establish an open-source logo license. Brand names and logos belong to their respective owners and are used to identify integrations.
