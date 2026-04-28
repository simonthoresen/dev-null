# Networking, Release & Distribution

## Release & Distribution

**Binaries are NOT checked into git.** They are built and published automatically by GitHub Actions on every push to `main`.

- **GitHub Actions** (`.github/workflows/release.yml`): builds binaries, generates `.bundle-manifest.json` (via `cmd/gen-manifest`), zips everything in `dist/` (excluding `logs/`, `Common/state/`, host keys, `.bundle-version`) into `DevNull.zip`, and publishes releases.
  - **`push to main`**: rolling `latest` release (dev channel).
  - **`v*` tags**: versioned release (e.g. `v0.1.0`) for stable distribution and winget.
- **Winget packaging** (`winget/`): manifest templates for Windows Package Manager. `InstallerType: zip` with `NestedInstallerType: portable` — winget extracts the zip, creates PATH symlinks for `DevNullServer` and `DevNullClient`. Submit to `microsoft/winget-pkgs` by updating the version, URL, and SHA256, then opening a PR.
- **`install.ps1`** (repo root): one-liner installer for operators -- downloads and extracts the latest release zip verbatim into `%USERPROFILE%\DevNull\`, creates desktop shortcuts. Usage: `irm https://github.com/simonthoresen/DevNull/raw/main/install.ps1 | iex`
- **`DevNullServer.ps1` / `DevNull.ps1`** (in `dist/` → install root): auto-updates on each launch -- checks the GitHub release for a newer version and downloads the full zip before starting.
- **Version tracking**: `dist/Common/.version` (i.e. `~/DevNull/Common/.version`) stores the commit SHA of the installed release. Not tracked in git.
- **Data directory bootstrap** (`internal/datadir`): on first run or version upgrade, bundled assets are copied from the install dir (the exe's dir, `~/DevNull/Common\`) to the data dir (same path in production); `.bundle-manifest.json` is used for diffing. Merge/update behavior is implemented in `internal/datadir/datadir.go` (`Bootstrap`).

## Connection Strategy

Startup order: UPnP -> Pinggy -> generate invite script.

The invite command is a raw PowerShell one-liner (paste into a PowerShell window): `$env:NS='<token>';irm <Join.ps1 URL>|iex`. The `NS` environment variable is a base64url-encoded binary token:

| Bytes | Field | Notes |
|-------|-------|-------|
| 0-1 | SSH port (uint16 BE) | Shared by localhost, LAN, UPnP |
| 2-5 | LAN IP (4 bytes) | `0.0.0.0` = absent |
| 6-9 | Public/UPnP IP (4 bytes) | `0.0.0.0` = absent |
| 10-11 | Pinggy port (uint16 BE) | `0` = no Pinggy |
| 12+ | Pinggy hostname (UTF-8) | Remaining bytes |

Variable-length: trailing absent fields are omitted. `Join.ps1` always tries `localhost` first (not encoded). Field presence is determined by token length: >=6 -> LAN, >=10 -> public IP, >=12 -> Pinggy.

Each attempt uses a short `ConnectTimeout`; falls through on failure.

`PinggyHelper.exe` stdout/stderr are redirected to the install-root `logs/pinggy-stdout.log` / `pinggy-stderr.log` by `DevNullServer.ps1` -- they must not pollute the boot sequence output.

## LAN Discovery (mDNS)

`DevNullServer` advertises `_devnull._tcp.local.` via mDNS so GUI clients can auto-list LAN servers in the startup menu.

- Dedicated servers advertise automatically.
- Client-owned local headless servers started from `DevNullClient --local` set `DEV_NULL_DISABLE_LAN_DISCOVERY=1` so they stay private unless explicitly shared.

## SSH Delta Rendering Bug on Windows (bubbletea patch)

**Problem**: bubbletea v2 hardcodes `mapNl=false` on Windows (`tea.go`, line with `runtime.GOOS != "windows"`). With `mapNl=false` the delta renderer tracks `\n` as "cursor col stays." But `gossh.EmulatePty()` unconditionally converts `\n` → `\r\n` via `PtyWriter`. The terminal cursor resets to col 0 after `\r`, but the renderer tracker doesn't know — causing catastrophic cursor position divergence on every newline. All delta writes land at wrong columns: garbled staircase output over SSH, but clean in `--no-ssh` mode (no PTY writer involved).

**Fix**: `bubbletea-v2-patched/` is a vendored copy of `charm.land/bubbletea/v2 v2.0.2` with one line changed in `tea.go`:
```go
// Before (broken on Windows SSH):
mapNl := runtime.GOOS != "windows" && p.ttyInput == nil
// After (same logic Linux already used for SSH sessions):
mapNl := p.ttyInput == nil
```
`go.mod` has `replace charm.land/bubbletea/v2 => ./bubbletea-v2-patched`. When the upstream bug is fixed, remove the replace directive and delete `bubbletea-v2-patched/`.

**Why mapNl=true works with PtyWriter**: PtyWriter converts `\n` → `\r\n`. With `mapNl=true`, the renderer emits `\r\n` itself (renderer now writes `\r\n`, tracker resets col to 0). PtyWriter then converts `\r\n` → `\r\r\n`, but its second pass normalizes `\r\r\n` → `\r\n`. Net result: renderer and terminal both see `\r\n`, both track col=0 after each line.

**EAW=A chars**: Some Unicode Math Operator characters (e.g. U+2295 ⊕, U+2299 ⊙) are `EAW=Ambiguous` — bubbletea counts them as 1-wide but terminals may render them 2-wide, causing 1-col within-row drift. Stick to verified EAW=N chars in any content rendered by the delta renderer.

## SSH Input Handling (Windows gotcha)

Use `ssh.EmulatePty()` -- **not** `ssh.AllocatePty()` -- in all three call sites in `internal/server/server.go`.

On Windows, `AllocatePty` creates a real ConPTY. The `charmbracelet/ssh` library then spawns `go io.Copy(sess.pty, sess)` internally. When Bubble Tea also reads from the session, two goroutines alternate consuming bytes and **every other keystroke is dropped**.

`EmulatePty` stores PTY metadata (term type, window size) without spawning a ConPTY, so there is only one reader. Search for `EmulatePty` in `internal/server/server.go` to find all three call sites.

## Init Files (`~/DevNull/Config/`)

Both files: one command per line; lines starting with `#` are comments. Dispatched on the first tick after the UI is running. Lives in `%USERPROFILE%\DevNull\Config\` (auto-resolved via `datadir.ConfigDir()`) — a peer of `Common\`, so prefs survive a reinstall/wipe of `Common\` and apply across `--data-dir` overrides.

**`~/DevNull/Config/server.txt`** -- commands run automatically when the server console starts. Useful for loading a default game, setting a theme, or loading server-side plugins.

**`~/DevNull/Config/client.txt`** -- commands run automatically when a player joins a server (or starts in `--local` mode). `Join.ps1` reads this file, base64-encodes it, and sends it via the `DEV_NULL_INIT` SSH environment variable.

Example `~/DevNull/Config/server.txt`:
```
# Server auto-setup
/theme dark
/game load invaders
```

Example `~/DevNull/Config/client.txt`:
```
# Client auto-setup
/theme dark
/plugin load greeter
```
