module github.com/menuvex/novex-printer-agent

go 1.21

// NOTE: zero external dependencies by design.
// The agent must build with a plain `go build` on Windows, macOS and
// Linux, with CGO disabled, and install as a single static binary.
