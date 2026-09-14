# Novex Printer Agent — build with plain Go, no CGO, no dependencies.
#   make build        native binary
#   make build-all    windows + macOS + linux binaries (amd64 + arm64)
#   make test         full test suite (uses mock TCP printers + mock USB)
#   make package      release archives in dist/

APP      := novex-printer-agent
VERSION  ?= 1.0.0
LDFLAGS  := -s -w -X github.com/menuvex/novex-printer-agent/internal/platform.Version=$(VERSION)
GOFLAGS  := -trimpath
BUILD    := CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)"

.PHONY: build build-all test test-short vet fmt clean package sdk-build sdk-test

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(APP) ./cmd/agent

build-all:
	GOOS=windows GOARCH=amd64 $(BUILD) -o dist/$(APP)-windows-amd64.exe ./cmd/agent
	GOOS=windows GOARCH=arm64 $(BUILD) -o dist/$(APP)-windows-arm64.exe ./cmd/agent
	GOOS=darwin  GOARCH=amd64 $(BUILD) -o dist/$(APP)-darwin-amd64 ./cmd/agent
	GOOS=darwin  GOARCH=arm64 $(BUILD) -o dist/$(APP)-darwin-arm64 ./cmd/agent
	GOOS=linux   GOARCH=amd64 $(BUILD) -o dist/$(APP)-linux-amd64 ./cmd/agent
	GOOS=linux   GOARCH=arm64 $(BUILD) -o dist/$(APP)-linux-arm64 ./cmd/agent

test:
	go test ./...

test-short:
	go test -short ./...

vet:
	go vet ./...

fmt:
	gofmt -l cmd internal
	test -z "$$(gofmt -l cmd internal)"

clean:
	rm -rf dist $(APP) $(APP).exe
	rm -rf sdk/typescript/dist

package: build-all
	cd dist && \
	for f in $(APP)-windows-amd64.exe $(APP)-windows-arm64.exe; do \
		[ -f "$$f" ] && zip -j "$${f%.exe}-v$(VERSION).zip" "$$f" ; \
	done ; \
	for f in $(APP)-darwin-amd64 $(APP)-darwin-arm64 $(APP)-linux-amd64 $(APP)-linux-arm64; do \
		[ -f "$$f" ] && tar -czf "$$f-v$(VERSION).tar.gz" "$$f" ; \
	done
	ls -la dist

sdk-build:
	cd sdk/typescript && npm install --no-audit --no-fund && npm run build

sdk-test:
	cd sdk/typescript && (test -d node_modules || npm install --no-audit --no-fund) && npm test
