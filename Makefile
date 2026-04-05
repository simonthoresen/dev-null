.PHONY: build build-server build-client run-server run-server-lan run-server-local run-client run-client-local test clean

GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_DATE  := $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)

# Build all binaries into dist/ (strip debug info for smaller binaries)
build: build-server build-client

build-server:
	go build -ldflags="-s -w -X 'main.buildCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(BUILD_DATE)'" -o dist/null-space-server.exe ./cmd/null-space-server
	go build -ldflags="-s -w" -o dist/pinggy-helper.exe ./cmd/pinggy-helper

build-client:
	go build -ldflags="-s -w -X 'main.buildCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(BUILD_DATE)'" -o dist/null-space-client.exe ./cmd/null-space-client

# Server: normal mode (SSH server + console TUI)
run-server: build-server
	./dist/null-space-server.exe --data-dir dist

# Server: LAN-only mode (no UPnP, no public IP, no Pinggy)
run-server-lan: build-server
	./dist/null-space-server.exe --data-dir dist --lan

# Server: local mode (headless SSH server + terminal client)
run-server-local: build-server
	./dist/null-space-server.exe --data-dir dist --local

# Client: connect to a running server
run-client: build-client
	./dist/null-space-client.exe

# Client: local mode (headless SSH server + graphical client)
run-client-local: build-client
	./dist/null-space-client.exe --data-dir dist --local

# Run all tests
test:
	go test -v ./...

# Remove build outputs from dist/ (keeps games/, fonts/, logs/)
clean:
	rm -f dist/null-space-server.exe dist/null-space-client.exe dist/pinggy-helper.exe
