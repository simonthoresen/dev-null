.PHONY: build run run-lan run-local test clean

# Build the server binary into dist/ (strip debug info for smaller binaries)
build:
	go build -ldflags="-s -w" -o dist/null-space-server.exe ./cmd/null-space-server
	go build -ldflags="-s -w" -o dist/null-space-client.exe ./cmd/null-space-client
	go build -ldflags="-s -w" -o dist/pinggy-helper.exe ./cmd/pinggy-helper

# Run directly from source, using dist/ as the data directory
run:
	go run ./cmd/null-space-server --data-dir dist

# Run in LAN-only mode (no UPnP, no public IP, no Pinggy)
run-lan:
	go run ./cmd/null-space-server --data-dir dist --lan

# Run in local mode (no SSH, single-player TUI)
run-local:
	go run ./cmd/null-space-server --data-dir dist --local

# Run all tests
test:
	go test -v ./...

# Remove build outputs from dist/ (keeps games/, fonts/, logs/)
clean:
	rm -f dist/null-space-server.exe dist/null-space-client.exe dist/pinggy-helper.exe
