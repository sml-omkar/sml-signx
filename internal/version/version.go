package version

// These values are overridden at build time with -ldflags "-X ...".
var (
	Version   = "0.1.0-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)
