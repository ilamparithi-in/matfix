package version

import "fmt"

// # Build metadata

// These variables are overridden at build time via -ldflags.
var (
	Version = "0.0.0"
	Commit  = "none"
	Date    = "unknown"
)

// Full returns a human-readable build string for CLI/version output.
func Full() string {
	return fmt.Sprintf("version=%s commit=%s built=%s", Version, Commit, Date)
}
