// Package version exposes the build-time version string of Owl.
package version

// Version is overridden at build time via -ldflags "-X
// github.com/neverbot/owl/internal/version.Version=v0.1.0".
var Version = "dev"

// String returns the current version string.
func String() string {
	return Version
}
