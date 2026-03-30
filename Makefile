.PHONY: build run clean

# Build the server binary into dist/
build:
	go build -o dist/null-space.exe ./cmd/null-space
	go build -o dist/pinggy-helper.exe ./cmd/pinggy-helper

# Run directly from source, using dist/ as the data directory
run:
	go run ./cmd/null-space --data-dir dist

# Remove build outputs from dist/ (keeps apps/, plugins/, logs/)
clean:
	rm -f dist/null-space.exe dist/pinggy-helper.exe
