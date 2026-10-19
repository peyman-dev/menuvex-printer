# Root Makefile — Unified MenuVex Offline + Native Printer System
# Builds and runs the single merged application (no Tauri required).

.PHONY: run run-web test build windows darwin linux package-linux

run:
	cd offline-menuvex && go run ./cmd/coffish-desktop

run-web:
	cd offline-menuvex && go run ./cmd/local

test:
	cd offline-menuvex && go test -v ./...

build:
	cd offline-menuvex && $(MAKE) all

windows:
	cd offline-menuvex && $(MAKE) windows

darwin:
	cd offline-menuvex && $(MAKE) darwin macos-bundle

linux:
	cd offline-menuvex && $(MAKE) linux

package-linux:
	cd offline-menuvex && $(MAKE) package-linux
