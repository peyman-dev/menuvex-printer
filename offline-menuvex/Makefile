# Cross-Platform Build System for Coffish Desktop (Windows .exe, macOS .app/binary, Linux binary)
# Uses native OS windowing (macOS Cocoa + WebKit WKWebView, Linux GTK/WebKit2GTK, Windows Forms)
# and NEVER launches Chrome or Chromium.

BINARY_NAME := coffish-desktop
DIST_DIR    := dist
PKG         := ./cmd/coffish-desktop
LDFLAGS     := -s -w
HOST_OS     := $(shell uname -s 2>/dev/null || echo Unknown)

ifeq ($(HOST_OS),Darwin)
DARWIN_CGO := 1
else
DARWIN_CGO := 0
endif

.PHONY: all test clean windows darwin linux macos-bundle

all: clean windows darwin linux macos-bundle

test:
	CGO_ENABLED=0 go test -v ./...

windows:
	@mkdir -p $(DIST_DIR)
	rm -f $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe $(DIST_DIR)/$(BINARY_NAME)-windows-arm64.exe
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -a -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -a -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-windows-arm64.exe $(PKG)

darwin:
	@mkdir -p $(DIST_DIR)
	rm -f $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64
	CGO_ENABLED=$(DARWIN_CGO) GOOS=darwin GOARCH=arm64 go build -a -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(PKG)
	CGO_ENABLED=$(DARWIN_CGO) GOOS=darwin GOARCH=amd64 go build -a -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 $(PKG)

linux:
	@mkdir -p $(DIST_DIR)
	rm -f $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(DIST_DIR)/$(BINARY_NAME)-linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(PKG)

macos-bundle: clean darwin
	@mkdir -p $(DIST_DIR)/CoffishDesktop.app/Contents/MacOS
	@mkdir -p $(DIST_DIR)/CoffishDesktop.app/Contents/Resources
	cp $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(DIST_DIR)/CoffishDesktop.app/Contents/MacOS/CoffishDesktop
	chmod +x $(DIST_DIR)/CoffishDesktop.app/Contents/MacOS/CoffishDesktop
	@printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0"><dict>' \
		'<key>CFBundleExecutable</key><string>CoffishDesktop</string>' \
		'<key>CFBundleIdentifier</key><string>ir.coffish.desktop</string>' \
		'<key>CFBundleName</key><string>Coffish Desktop</string>' \
		'<key>CFBundleDisplayName</key><string>کافیش دسکتاپ</string>' \
		'<key>CFBundlePackageType</key><string>APPL</string>' \
		'<key>CFBundleShortVersionString</key><string>2.1.0</string>' \
		'<key>NSHighResolutionCapable</key><true/>' \
		'</dict></plist>' > $(DIST_DIR)/CoffishDesktop.app/Contents/Info.plist

clean:
	rm -rf $(DIST_DIR) local coffish-desktop shell/build/bin
