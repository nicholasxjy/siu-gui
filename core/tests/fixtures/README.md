# siu backup compatibility fixture

`siu-backup-v1.json` was exported by unmodified upstream siu at
`1a3a7ef32a3c4dd9115da2e0b31fdc6680b79fdf`, using `backup::export`
against a temporary home. The source home was normalized to `/reference/home`,
`exportedAt` to `1`, and `platform` to `linux`; all portable state and file
contents are retained. The fixture contains synthetic credentials only.

It covers enabled and paused MCP entries, Agent assignments, a local Skill
source, embedded text/binary/executable files, proxy settings, and the CLI
export behavior of omitting the GUI theme. Protocol tests import it, validate
the destination state, and re-export it with a desktop theme.

When updating the vendored backup implementation, regenerate this fixture
through the reference CLI implementation and check both directions: CLI
export → GUI restore and GUI export → CLI restore. Also check that a default
restore by either side leaves a receipt the other side accepts, preserving
subsequent local edits without another restore. Never generate compatibility
fixtures from the GUI implementation being tested.
