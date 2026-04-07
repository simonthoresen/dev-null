.PHONY: build build-server build-client build-testbed run-server run-server-lan run-client run-client-local run-testbed run-testbed-onlcr test clean generate-manifest

GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_DATE  := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
GIT_REMOTE  := $(shell git remote get-url origin 2>/dev/null || echo "")

# Build all binaries into dist/ (strip debug info for smaller binaries)
build: build-server build-client

build-server:
	go build -ldflags="-s -w -X 'main.buildCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(BUILD_DATE)' -X 'main.buildRemote=$(GIT_REMOTE)'" -o dist/dev-null-server.exe ./cmd/dev-null-server
	go build -ldflags="-s -w" -o dist/pinggy-helper.exe ./cmd/pinggy-helper

build-client:
	go build -ldflags="-s -w -X 'main.buildCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(BUILD_DATE)' -X 'main.buildRemote=$(GIT_REMOTE)'" -o dist/dev-null-client.exe ./cmd/dev-null-client

# Server: normal mode (SSH server + console TUI)
run-server: build-server
	./dist/dev-null-server.exe --data-dir dist

# Server: LAN-only mode (no UPnP, no public IP, no Pinggy)
run-server-lan: build-server
	./dist/dev-null-server.exe --data-dir dist --lan

# Client: connect to a running server
run-client: build-client
	./dist/dev-null-client.exe

# Client: local mode (headless SSH server + graphical client)
run-client-local: build-client
	./dist/dev-null-client.exe --data-dir dist --local

# Run all tests
test:
	go test -v ./...

# Testbed: minimal wish+bubbletea repro binary (no product code, SSH artifact isolation)
build-testbed:
	go build -o dist/testbed.exe ./testbed

# Testbed: SSH mode without ONLCR fix (expect staircase on non-Windows)
run-testbed: build-testbed
	./dist/testbed.exe

# Testbed: SSH mode with ONLCR fix applied
run-testbed-onlcr: build-testbed
	./dist/testbed.exe --onlcr

# Generate bundle manifest for dist/ assets
generate-manifest:
	go run ./cmd/gen-manifest dist/ > dist/.bundle-manifest.json

# Remove build outputs from dist/ (keeps games/, fonts/, logs/)
clean:
	rm -f dist/dev-null-server.exe dist/dev-null-client.exe dist/pinggy-helper.exe dist/testbed.exe
