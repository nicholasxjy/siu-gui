package backend

import (
	"embed"
	"fmt"
)

// embeddedBundle holds the build-time staged siu-core sidecar (and this
// directory's README) via go:embed. It is the last-resort source for the
// core executable when neither SIU_GUI_CORE nor an explicit packaged
// resources path is available.
//
//go:embed bundle/*
var embeddedBundle embed.FS

const embeddedBinaryName = "siu-core.bin"

// embeddedCoreBytes returns the staged siu-core binary bytes, or a clear
// actionable error if the build script has not staged it yet.
func embeddedCoreBytes() ([]byte, error) {
	data, err := embeddedBundle.ReadFile("bundle/" + embeddedBinaryName)
	if err != nil {
		return nil, fmt.Errorf("embedded siu-core binary is missing (bundle/%s was not staged at build time); run `bun run prepare:core` to build and stage it, then rebuild siu-gui: %w", embeddedBinaryName, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("embedded siu-core binary (bundle/%s) is empty; run `bun run prepare:core` to rebuild it", embeddedBinaryName)
	}
	return data, nil
}
