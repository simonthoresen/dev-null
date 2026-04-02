.PHONY: build run run-lan clean

# Build the server binary into dist/ (strip debug info for smaller binaries)
build:
	go build -ldflags="-s -w" -o dist/null-space.exe ./cmd/null-space
	go build -ldflags="-s -w" -o dist/pinggy-helper.exe ./cmd/pinggy-helper

# Run directly from source, using dist/ as the data directory
run:
	go run ./cmd/null-space --data-dir dist

# Run in LAN-only mode (no UPnP, no public IP, no Pinggy)
run-lan:
	go run ./cmd/null-space --data-dir dist --lan

# Remove build outputs from dist/ (keeps games/, fonts/, logs/)
clean:
	rm -f dist/null-space.exe dist/pinggy-helper.exe
