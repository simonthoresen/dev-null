# Authoring games, plugins, and shaders

This guide covers writing your own content for DevNull and sharing it
with anyone running a DevNull server. The full JS surface is in
[API-REFERENCE.md](API-REFERENCE.md); this doc is about the workflow.

## Set up authoring

1. Make sure DevNull is installed on your machine. If not, run the
   installer one-liner:
   ```powershell
   irm https://github.com/simonthoresen/DevNullCore/raw/main/install.ps1 | iex
   ```
2. Click **DevNull Create Games** on your desktop.

   On first run this:
   - installs the GitHub CLI (`gh`) if it's missing,
   - installs the GitHub Copilot CLI extension,
   - asks you to sign in to GitHub,
   - forks the [DevNullCreateTemplate](https://github.com/simonthoresen/DevNullCoreCreateTemplate)
     template to your account as `<you>/DevNullCreate` and clones it
     to `%USERPROFILE%\DevNull\Create\`,
   - opens Copilot CLI in that folder.

   Subsequent runs skip the install/auth/clone and just open Copilot CLI
   in the existing folder.

The shortcut is the only on-ramp for the dev stack — the regular install
doesn't pull `gh` or Copilot CLI, so players and hosts who don't author
aren't asked to set up GitHub.

## Local test loop

In `%USERPROFILE%\DevNull\Create\`:

```powershell
.\DevNullTest.ps1
```

This launches a local server and the GUI client. Your work in `Games\`,
`Plugins\`, and `Shaders\` is auto-discovered by the server and appears
in the relevant sub-menus under a `── Create ──` section, ahead of
bundled (`── Core ──`) items.

Naming collisions resolve in priority order **Create > Shared > Core**,
so an in-progress `snake.js` in your create folder shadows a bundled
`snake.js` of the same name.

Edit a file, then either:
- restart the server and the new content is picked up, or
- in the server console, `/game-unload` then `/game-load <name>` to swap
  in the new version. (Live-reload during a running game isn't
  supported; the runtime would have to throw away its in-flight state.)

## Push & share your URL

After committing and pushing, your file is live at:

```
https://raw.githubusercontent.com/<owner>/<repo>/<branch>/<path>
```

If you're using Copilot CLI with the starter's
`.github/copilot-instructions.md`, it computes and prints this URL after
each push. Otherwise run `.\print-raw-urls.ps1` in the create folder to
list URLs for every `.js` under `Games\`, `Plugins\`, `Shaders\`.

GitHub `blob/` URLs are auto-converted server-side, so either form
works when pasting.

## Hand the URL to a server admin

The admin of any DevNull server can load your file by either of:

- Opening **File > Games > Add...** (or Plugins > Add / Shaders > Add),
  pasting your raw URL, and clicking Load.
- Running `/game-load <url>` (or `/plugin-load`, `/shader-load`) in the
  server console.

Loading happens once per URL — the server downloads the file into
`%USERPROFILE%\DevNull\Shared\<Kind>\` and it appears in the relevant
sub-menu under the `── Shared ──` section. Re-loading the same URL
overwrites the local copy.

The `Add...` items are admin-only. If you're not the admin, ask the
person hosting to load it for you.

## Limits and gotchas

- Single `.js` files are capped at **1 MB**.
- `.zip` packages are capped at **10 MB** and must contain `main.js`
  at the root (or in a single top-level directory).
- URL ingestion requires HTTPS (HTTP is rejected) and is admin-only.
- No live-reload — `/game-unload` then re-load to swap a running game.
- Resolution priority is **Create > Shared > Core**: an item present
  in your create folder always wins over Shared and Core of the same
  name.
- One create folder per machine. If you want multiple author repos,
  symlink in/out of `%USERPROFILE%\DevNull\Create\` for now.
