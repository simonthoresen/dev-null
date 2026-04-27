# dev-null

dev-null is a Windows server for hosting real-time multiplayer terminal games over SSH. You run it on your machine, share an invite command, and anyone can join instantly with a plain `ssh` command — no client software required. Games and plugins are single JavaScript files that you drop in a folder (or load directly from a URL), so creating and sharing new games is as simple as writing a `.js` file and pasting a GitHub link. The server handles everything else: player connections, a shared chat channel, synchronized rendering at 60 fps, and automatic tunnel setup via Pinggy so you can host from anywhere without touching your router.

## Install

Paste this into a PowerShell window:

```powershell
iwr -useb https://raw.githubusercontent.com/simonthoresen/dev-null/main/install.ps1 | iex
```

This creates a `DevNull` folder in your current directory containing everything you need. No other dependencies required.

## Start the server

```powershell
cd DevNull
.\start-server.ps1 --password yourpassword
```

The server prints an invite command that others can paste into any terminal to join.

## Load a game

Once the server is running, type into the server console:

```
/game load example
/game load https://github.com/someone/repo/blob/main/mygame.js
```

## Auto-run commands on startup

Create files in `~/dev-null/config/` to run commands automatically. One command per line; lines starting with `#` are comments.

### `~/dev-null/config/server.txt`

Runs when the server console starts. Useful for loading a default game, setting a theme, or loading server-side plugins.

```
# My server setup
/theme dark
/plugin load greeter
/game load invaders
```

### `~/dev-null/config/client.txt`

Runs when you join a server (or start in `--local` mode). The join script reads this file and sends it to the server via SSH.

```
# My client setup
/theme dark
/plugin load greeter
```

These files live next to (not inside) the `play\` runtime install, so they survive a reinstall. Legacy `~/.dev-null/server.txt` and `~/.dev-null/client.txt` are still read on first start and copied forward to the new location.

## Write your own game

Click **dev-null Create Games** on your desktop. First run installs the
GitHub CLI + Copilot CLI, forks a [starter template](https://github.com/simonthoresen/dev-null-starter)
to your account, clones it to `~/dev-null/create/`, and opens Copilot
CLI in that folder. Run `.\play.ps1` in the create folder to test
locally; push to GitHub and hand the raw URL to any dev-null server
admin who can paste it into Games > Add.

Full workflow: [AUTHORING.md](AUTHORING.md).
JavaScript API surface: [API-REFERENCE.md](API-REFERENCE.md).
