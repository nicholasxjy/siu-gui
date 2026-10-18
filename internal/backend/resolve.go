package backend

import (
	"fmt"
	"os"
)

// envOverrideVar lets tests and development point the client at an
// arbitrary executable (real or simulated), bypassing packaged resources
// and the embedded fallback entirely.
const envOverrideVar = "SIU_GUI_CORE"

// resolveCoreExecutable decides which siu-core executable to launch, in
// priority order:
//  1. SIU_GUI_CORE environment override (test/dev).
//  2. explicitPath, e.g. the packaged mygo resources bin supplied via
//     NewWithExecutable, so the platform-signed sidecar under
//     resources/<os>-<arch>/bin is preferred over the embedded fallback.
//  3. The embedded binary, extracted to the user cache directory.
func resolveCoreExecutable(explicitPath string) (string, error) {
	if override := os.Getenv(envOverrideVar); override != "" {
		return override, nil
	}
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return "", fmt.Errorf("siu-core executable not found at packaged resources path %q: %w; rebuild the platform resources bundle (resources/<os>-<arch>/bin) before packaging siu-gui", explicitPath, err)
		}
		return explicitPath, nil
	}
	return extractEmbeddedCore()
}
