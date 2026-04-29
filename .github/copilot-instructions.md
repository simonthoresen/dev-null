# Copilot Instructions for DevNull

## Build, test, and lint commands

- Build all shipped binaries (`DevNullServer.exe`, `DevNullClient.exe`, `PinggyHelper.exe`):
  - `make build`
- Build individual binaries:
  - `make build-server`
  - `make build-client`
  - `make build-testbed`
- Run local dev flows:
  - `make run-server`
  - `make run-server-lan`
  - `make run-client`
  - `make run-client-local`
- Develop against the installed layout (mirror `dist\` into `%USERPROFILE%\DevNull\` and run from there with `--no-update`):
  - `make install`
  - `make start-server`
  - `make start-server-lan`
  - `make start-client`
  - `make start-solo`

- Full test suite:
  - `make test` (runs `go test -v ./...`)
- Run a single test:
  - `go test ./internal/server -run TestName -count=1`
  - (replace `TestName` with the specific test you are targeting)
- Golden/harness update commands used in this repo:
  - `go test ./internal/rendertest/ -update`
  - `go test ./internal/engine/ -run TestGameRenderHarness -update-engine`
  - `go test ./internal/widget/ -run TestWidgetHarness -update-widgets`

- Lint:
  - There is no root-level lint target/workflow in this repository. Do not assume `golangci-lint` is part of the main project pipeline.

## High-level architecture

- DevNull is an SSH-hosted multiplayer game framework. Players can join with plain `ssh`; the active game is a single shared runtime on the server.
- `cmd/dev-null-server/main.go` boots data/logging/network setup, then starts the SSH server plus the Bubble Tea console UI.
- `internal/server/server.go` owns session lifecycle and the central ticker. Each SSH player gets their own Bubble Tea program/model, but all models share one `state.CentralState`.
- `internal/server/lifecycle.go` coordinates game load/start/unload. JS `gameOver()` results are posted to chat and the game unloads back to lobby (no separate ending phase UI).
- JS games run through `internal/engine/runtime.go` (goja). Runtime, commands, plugins, shaders, and state snapshots are all wired through domain interfaces.
- Asset/script discovery is multi-root (`internal/engine/sources.go`): **Create > Shared > Core** with name shadowing.
- UI is built through the NC widget system in `internal/widget/` and rendered to terminal buffers; player chrome and server console share the same input-routing model.
- Graphical client flow lives in `cmd/dev-null-client/main.go` + `internal/client`/`internal/display`: SSH transport, terminal rendering, and optional local rendering (blocks/pixels) using pushed state snapshots.

## Key codebase conventions

- **Lock-order invariant:** never hold `CentralState.mu` while calling game runtime methods (`Load`, `Begin`, `Update`, `Render`, `OnInput`, etc.). `Runtime` must not acquire state locks.
- **No bespoke ANSI UI:** dialogs, overlays, and structured UI should be implemented with `internal/widget` controls and `RenderToBuf`, not ad-hoc string drawing.
- **Input routing is centralized:** key behavior changes should go through `internal/input/router.go` (used by both chrome and console). Esc/Enter follow `WantsEsc`/`WantsEnter` consumer semantics.
- **Reuse local command handlers:** `/theme*`, `/plugin*`, `/shader*` behavior is shared in `internal/localcmd`; console/chrome should call into those helpers rather than duplicate command logic.
- **Respect source precedence:** Create/Shared/Core order must stay consistent for listing and resolving games/plugins/shaders.
- **Time-based render contract:** for local rendering, game code should rely on `Game.state._gameTime` instead of local wall clock.
- **Render-path logging rule:** avoid `slog` in `View()`/`Render()` paths (console log routing can create render feedback loops).
- **Dev runs from the installed layout:** prefer `make install` + `make start-server` / `start-client` / `start-solo` so source-tree dev mirrors `%USERPROFILE%\DevNull\` (same `Core\`, `Shared\`, `Create\`, `Config\`, `Logs\` paths a packaged install uses). The legacy `run-*` targets still work for in-place runs from `dist\`.
