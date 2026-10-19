//go:build !windows

package main

import "log"

// fatal reports an unrecoverable startup error. On non-Windows builds the
// process is normally started from a terminal or a .desktop launcher, so the
// log line is enough.
func fatal(msg string) { log.Fatal(msg) }
