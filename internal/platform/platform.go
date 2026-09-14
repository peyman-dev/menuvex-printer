// Package platform holds app metadata and OS integration helpers
// (start-on-login). Everything here is pure Go with no build tags so the
// agent cross-compiles with plain `GOOS=... go build`.
package platform

// AppName is the human-readable product name.
const AppName = "Novex Printer Agent"

// Version is the agent version reported by /health and /api/v1/info.
// Release builds may override it with:
//
//	go build -ldflags "-X github.com/menuvex/novex-printer-agent/internal/platform.Version=1.2.3"
var Version = "1.0.0"
