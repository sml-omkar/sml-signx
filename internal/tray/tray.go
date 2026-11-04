// Package tray provides a Windows system-tray shell around the signing
// service. On other platforms it is a no-op; the service runs headless.
package tray

// Options configures the tray icon and its menu actions.
type Options struct {
	Tooltip   string
	StatusURL string
	DataDir   string
	Quit      func()
}
