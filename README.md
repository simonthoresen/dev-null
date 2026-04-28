# DevNull

DevNull is a Windows server for hosting real-time multiplayer terminal games over SSH. You run it on your machine, share an invite command, and anyone can join instantly with a plain `ssh` command — no client software required. Games and plugins are single JavaScript files that you drop in a folder (or load directly from a URL), so creating and sharing new games is as simple as writing a `.js` file and pasting a GitHub link. The server handles everything else: player connections, a shared chat channel, synchronized rendering at 60 fps, and automatic tunnel setup via Pinggy so you can host from anywhere without touching your router.

## Install

Paste this into a PowerShell window:

```powershell
iwr -useb https://raw.githubusercontent.com/simonthoresen/DevNull/main/install.ps1 | iex
```

This creates a `DevNull` folder in `%USERPROFILE%` containing everything you need. No other dependencies required.

## Start the server

```powershell
cd $env:USERPROFILE\DevNull
.\DevNullServer.ps1 --password yourpassword
```

The server prints an invite command that others can paste into any terminal to join.

## Start the client

```powershell
cd $env:USERPROFILE\DevNull
.\DevNull.ps1
```

The GUI now starts in a disconnected main menu. It shows localhost, known servers from `~/DevNull/Config/servers.txt` (`name=host:port` or `host:port`), and LAN servers discovered automatically.  
Use **Create server** to start a local headless server tied to the client lifecycle, and **Open tunnel** to start Pinggy on demand for that local server.

Direct modes are still supported:

```powershell
.\DevNull.ps1 --host 192.168.1.40 --port 23234
.\DevNull.ps1 --local
.\DevNull.ps1 --no-gui --host 192.168.1.40 --port 23234
.\DevNull.ps1 --no-gui --local
```

## Load a game

Once the server is running, type into the server console:

```
/game load example
/game load https://github.com/someone/repo/blob/main/mygame.js
```

## Auto-run commands on startup

Create files in `~/DevNull/Config/` to run commands automatically. One command per line; lines starting with `#` are comments.

### `~/DevNull/Config/server.txt`

Runs when the server console starts. Useful for loading a default game, setting a theme, or loading server-side plugins.

```
# My server setup
/theme dark
/plugin load greeter
/game load invaders
```

### `~/DevNull/Config/client.txt`

Runs when you join a server (or start in `--local` mode). The join script reads this file and sends it to the server via SSH.

```
# My client setup
/theme dark
/plugin load greeter
```

These files live next to (not inside) the `Common\` runtime install, so they survive a reinstall.

## Write your own game

Click **DevNull Create Games** on your desktop. First run installs the
GitHub CLI + Copilot CLI, forks a [starter template](https://github.com/simonthoresen/DevNullCreateTemplate)
to your account as `<you>/DevNullCreate`, clones it to
`~/DevNull/Create/`, and opens Copilot CLI in that folder. Run
`.\DevNullTest.ps1` in the create folder to test locally; push to
GitHub and hand the raw URL to any DevNull server admin who can paste
it into Games > Add.

Full workflow: [AUTHORING.md](AUTHORING.md).
JavaScript API surface: [API-REFERENCE.md](API-REFERENCE.md).
