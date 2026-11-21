//go:build !darwin && !windows

package core

func readSystemProxy() (*systemProxySnapshot, error) {
	// On Linux, standard proxy environment variables are already handled before
	// calling readSystemProxy; fall back to direct when none are configured.
	return &systemProxySnapshot{}, nil
}
