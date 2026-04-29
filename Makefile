.PHONY: build build-server build-client build-testbed run-server run-server-lan run-client run-client-local run-testbed run-testbed-onlcr test clean generate-manifest winres install start-server start-server-lan start-client start-solo

GIT_COMMIT  := $(shell git rev-parse --short HEAD)
BUILD_DATE  := $(shell git log -1 --format=%cI)
GIT_REMOTE  := $(shell git remote get-url origin)
INSTALL_DIR := $(USERPROFILE)/DevNull

# Build all binaries into dist/Core/ (strip debug info for smaller binaries)
build: winres build-server build-client

# Generate Windows resource files (icon + manifest) from winres/icon.ico
winres:
	cd cmd/dev-null-server && go-winres simply --icon winres/icon.ico
	cd cmd/dev-null-client && go-winres simply --icon winres/icon.ico

build-server:
	go build -ldflags="-s -w -X 'main.buildCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(BUILD_DATE)' -X 'main.buildRemote=$(GIT_REMOTE)'" -o dist/Core/DevNullServer.exe ./cmd/dev-null-server
	go build -ldflags="-s -w" -o dist/Core/PinggyHelper.exe ./cmd/pinggy-helper

build-client:
	go build -ldflags="-s -w -X 'main.buildCommit=$(GIT_COMMIT)' -X 'main.buildDate=$(BUILD_DATE)' -X 'main.buildRemote=$(GIT_REMOTE)'" -o dist/Core/DevNullClient.exe ./cmd/dev-null-client
	git rev-parse HEAD > dist/Core/.version

# Server: normal mode (SSH server + console TUI)
run-server: build-server
	powershell -ExecutionPolicy Bypass -File dist/DevNullServer.ps1 --no-update

# Server: LAN-only mode (no UPnP, no public IP, no Pinggy)
run-server-lan: build-server
	powershell -ExecutionPolicy Bypass -File dist/DevNullServer.ps1 --no-update --lan

# Client: connect to a running server
run-client: build-client
	powershell -ExecutionPolicy Bypass -File dist/DevNull.ps1 --no-update

# Client: local mode (headless SSH server + graphical client)
run-client-local: build-client
	powershell -ExecutionPolicy Bypass -File dist/DevNull.ps1 --no-update --local

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

# Generate bundle manifest for dist/Core/ assets
generate-manifest:
	go run ./cmd/gen-manifest dist/Core/ > dist/Core/.bundle-manifest.json

# Remove build outputs from dist/ (keeps Games/, Fonts/, etc.)
clean:
	rm -f dist/Core/DevNullServer.exe dist/Core/DevNullClient.exe dist/Core/PinggyHelper.exe dist/testbed.exe

# Install: build, generate manifest, then mirror dist/ into %USERPROFILE%/DevNull/
# so dev runs hit the same layout as a real installer. Strict-mirrors Core\
# (also deletes stale files); never touches Create\, Shared\, Config\, Logs\.
install: build generate-manifest
	powershell -ExecutionPolicy Bypass -File install-local.ps1

# Start targets: build + install + run from the user-profile install location
# with --no-update so locally-built binaries aren't overwritten by the latest
# GitHub release on each launch.
start-server: install
	powershell -ExecutionPolicy Bypass -File "$(INSTALL_DIR)/DevNullServer.ps1" --no-update

start-server-lan: install
	powershell -ExecutionPolicy Bypass -File "$(INSTALL_DIR)/DevNullServer.ps1" --no-update --lan

start-client: install
	powershell -ExecutionPolicy Bypass -File "$(INSTALL_DIR)/DevNull.ps1" --no-update

start-solo: install
	powershell -ExecutionPolicy Bypass -File "$(INSTALL_DIR)/DevNull.ps1" --no-update --local
