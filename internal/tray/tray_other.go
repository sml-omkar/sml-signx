//go:build !windows

package tray

// Run is a no-op on platforms without a tray shell. The service runs
// headless and is stopped via SIGINT/SIGTERM.
func Run(Options) error { return nil }
