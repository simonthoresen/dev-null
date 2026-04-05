# Networking, Release & Distribution

## Release & Distribution

**Binaries are NOT checked into git.** They are built and published automatically by GitHub Actions on every push to `main`.

- **GitHub Actions** (`.github/workflows/release.yml`): builds `dev-null-server.exe` + `dev-null-client.exe` + `pinggy-helper.exe`, packages the full `dist/` folder into `dev-null.zip`, and publishes a rolling `latest` release.
- **`install.ps1`** (repo root): one-liner installer for operators -- downloads and extracts the latest release zip, creates desktop shortcuts. Usage: `irm https://github.com/simonthoresen/dev-null/raw/main/install.ps1 | iex`
- **`start.ps1`** (in `dist/`): auto-updates on each launch -- checks the GitHub release for a newer version and downloads the full zip (binaries, games, fonts) before starting.
- **Version tracking**: `dist/.version` stores the commit SHA of the installed release. Not tracked in git.

## Connection Strategy

Startup order: UPnP -> Pinggy -> generate invite script.

The invite command is a raw PowerShell one-liner (paste into a PowerShell window): `$env:NS='<token>';irm <join.ps1 URL>|iex`. The `NS` environment variable is a base64url-encoded binary token:

| Bytes | Field | Notes |
|-------|-------|-------|
| 0-1 | SSH port (uint16 BE) | Shared by localhost, LAN, UPnP |
| 2-5 | LAN IP (4 bytes) | `0.0.0.0` = absent |
| 6-9 | Public/UPnP IP (4 bytes) | `0.0.0.0` = absent |
| 10-11 | Pinggy port (uint16 BE) | `0` = no Pinggy |
| 12+ | Pinggy hostname (UTF-8) | Remaining bytes |

Variable-length: trailing absent fields are omitted. `join.ps1` always tries `localhost` first (not encoded). Field presence is determined by token length: >=6 -> LAN, >=10 -> public IP, >=12 -> Pinggy.

Each attempt uses a short `ConnectTimeout`; falls through on failure.

`pinggy-helper.exe` stdout/stderr are redirected to `dist/logs/pinggy-stdout.log` / `pinggy-stderr.log` by `start.ps1` -- they must not pollute the boot sequence output.

## SSH Input Handling (Windows gotcha)

Use `ssh.EmulatePty()` -- **not** `ssh.AllocatePty()` -- in all three call sites in `internal/server/server.go`.

On Windows, `AllocatePty` creates a real ConPTY. The `charmbracelet/ssh` library then spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and **every other keystroke is dropped**.

`EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader. Search for `EmulatePty` in `internal/server/server.go` to find all three call sites.

## Init Files (`~/.dev-null/`)

Both files: one command per line; lines starting with `#` are comments. Dispatched on the first tick after the UI is running. Lives in the home directory so they survive reinstalls.

**`~/.dev-null/server.txt`** -- commands run automatically when the server console starts. Useful for loading a default game, setting a theme, or loading server-side plugins.

**`~/.dev-null/client.txt`** -- commands run automatically when a player joins a server (or starts in `--local` mode). The join script reads this file, base64-encodes it, and sends it via the `DEV_NULL_INIT` SSH environment variable.

Example `~/.dev-null/server.txt`:
```
# Server auto-setup
/theme dark
/game load invaders
```

Example `~/.dev-null/client.txt`:
```
# Client auto-setup
/theme dark
/plugin load greeter
```
