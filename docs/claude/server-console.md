# Server Console

`internal/console/console.go` is its own Bubble Tea program on the local terminal. Two phases:

## Phase 1 -- Boot sequence

Each step is printed in two passes:
1. **Before** the operation: `label ...................` (dots to fill line, no status, no newline)
2. **After** the operation: `\r` overwrites the line with `label ........ [ STATUS ]` right-aligned

Status tokens are always **8 chars wide** with the text centered:
```
[ DONE ]   (DONE = 4 chars, no padding)
[ FAIL ]   (FAIL = 4 chars, no padding)
[ SKIP ]   (SKIP = 4 chars, no padding)
```

Implementation: `startBootStep(label)` / `finishBootStep(status)` in `cmd/dev-null-server/main.go`. Terminal width via `github.com/charmbracelet/x/term`. The PS1 script has matching `Write-BootStepStart` / `Write-BootStepEnd` helpers.

Startup sequence (PS1 steps first, then Go binary):
```
Setting up network ......................................... [ DONE ]  <- PS1 header
Pinggy helper .............................................. [ DONE ]  <- PS1
SSH server ................................................. [ DONE ]  <- Go
UPnP port mapping .......................................... [ SKIP ]
Public IP detection ........................................ [ SKIP ]
Pinggy tunnel .............................................. [ DONE ]
Generating invite command .................................. [ DONE ]

  <invite command>

  (console UI runs)

Initiating shutdown ........................................ [ DONE ]  <- Go
Shutting down network ...................................... [ DONE ]  <- Go header
Stopping SSH server ........................................ [ DONE ]  <- Go
Stopping Pinggy helper ..................................... [ DONE ]  <- PS1
```

In `--local` mode, group headers show `[ SKIP ]` (yellow) and substeps are omitted:
```
Setting up network ......................................... [ SKIP ]  <- PS1
Generating invite command .................................. [ SKIP ]  <- Go
  (local TUI runs)
Initiating shutdown ........................................ [ DONE ]  <- Go
Shutting down network ...................................... [ SKIP ]  <- Go
```

## Phase 2 -- Console UI

Uses the same Screen layout as all views (MenuBar + Window + StatusBar):

```
| File  View  Help                    |  MenuBar (row 0)
+====================================+
|                                    |
| Log (scrollable, fills height)     |  NCTextView: slog lines + all chat
|                                    |  PgUp/PgDn to scroll
|                                    |
+------------------------------------+
| [.....]                            |  NCCommandInput: '/' = command; plain text = chat
+====================================+
| game: none | players: 0 | 15:04:05|  StatusBar (row 2)
```

The server console is always admin. The `/password` command is context-aware: from the console it sets the admin password; from a player session it authenticates as admin. Password can be pre-set via `--password` on the server. Clients can pass `--password` to auto-authenticate on connect.
