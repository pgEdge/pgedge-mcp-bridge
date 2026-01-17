// Package version provides version information for the MCP bridge.
package version

import (
	"fmt"
	"runtime"
)

// Build information, set via ldflags
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// Info returns a formatted version string
func Info() string {
	return fmt.Sprintf("mcp-bridge %s (commit: %s, built: %s, %s/%s)",
		Version, GitCommit, BuildTime, runtime.GOOS, runtime.GOARCH)
}

// Short returns just the version number
func Short() string {
	return Version
}
