// Package version exposes build-time information for DockPulse.
//
// The variables are intended to be set via -ldflags at build time:
//
//	-X github.com/TheRealChickenlegs/DockPulse/go/internal/version.Version=v1.2.3
//	-X github.com/TheRealChickenlegs/DockPulse/go/internal/version.Commit=abc1234
//	-X github.com/TheRealChickenlegs/DockPulse/go/internal/version.BuildDate=2026-08-12
package version

import (
	"fmt"
	"runtime"
)

// These variables are set at build time. Defaults are useful for `go run`.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// UserAgent is the value used in HTTP User-Agent headers for outbound calls.
func UserAgent() string {
	return fmt.Sprintf("DockPulse/%s (%s; %s)", Version, Commit, runtime.GOOS)
}

// String returns a printable version summary, suitable for --version.
func String() string {
	return fmt.Sprintf("DockPulse %s (commit %s, built %s, %s)", Version, Commit, BuildDate, runtime.GOOS)
}