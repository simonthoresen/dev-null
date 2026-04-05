# UI Layout & Themes

## UI Layout

All three views (console, lobby, playing) share a unified `Screen` layout:

```
Row 0: MenuBar      (fixed 1)   <- File Edit View Help -- navigation only
Row 1: Window        (fill)      <- bordered NCWindow, content varies per view
Row 2: StatusBar    (fixed 1)   <- left text + right-aligned time
```

`Screen` (`internal/widget/screen.go`) renders the MenuBar at the secondary theme layer (depth 1), the Window at the primary layer (depth 0), and the StatusBar at the secondary layer. Focus management and cursor position are delegated to the content Window.

**Lobby:**
```
| File  Edit  View  Help              |  MenuBar (row 0)
+=====================+==============+
|                     | ## Unassigned|  NCWindow with grid:
|  [chat messages]    |   alice      |    Row 0: NCTextView(chat) | NCVDivider | NCTeamPanel
|                     | ## Red Team  |    Row 1: NCHDivider (connected)
|                     |    bob       |    Row 2: NCCommandInput
|                     | ## Blue Team |
|                     |    charlie   |  Chat: weight=1, Teams: fixed 32 cols
+---------------------+--------------+  [Tab] cycles: input -> chat -> teams
| [.....]                            |  NCCommandInput: Enter=submit, Tab=cycle
+====================================+
| dev-null (local) | 3 players | ..|  StatusBar (row 2)
```

**Playing:**
```
| File  Edit  View  Help              |  MenuBar (row 0)
+====================================+
|                                    |  GameView (aspect-ratio: W*W*9/16)
|  Game viewport                     |    Enter -> focus command input
|                                    |    all other keys -> game.OnInput
+------------------------------------+
|  [chat messages]                   |  NCTextView (chat, fills remaining)
+------------------------------------+
| [.....]                            |  NCCommandInput: submit/Esc -> refocus GameView
+====================================+
| HP: 100  Score: 42    15:04:05     |  StatusBar: game.StatusBar() left, time right
```

**Viewport sizing:** Ideal `gameH = W * 9 / 16`. Chat gets the remaining rows. `minChatH = max(5, interiorH/3)` -- chat always gets at least 1/3 of interior rows. Interior = window height minus borders, dividers, and command input.

**Focus model:** NCWindow owns all focus management. Tab cycles between focusable controls. In the playing view, GameView has focus by default -- Enter moves focus to the command input, submit/Esc returns it to GameView. For NC-tree games (layout), game controls participate in the Tab cycle alongside chat and command input.

**Chat scroll buffer:** 200 lines per player. `PgUp`/`PgDn` scroll the chat panel. Multi-line command replies (e.g. `/help`) are split into individual lines before storage.

**Command history:** 50 entries per NCCommandInput. Up/Down browse history. Down past the newest entry restores the draft. History is managed by the NCCommandInput widget.

## Themes

JSON files in `dist/themes/` that control the NC-style chrome colors. Switch at runtime with `/theme <name>` (per-player, not global). Bundled themes: `norton` (default), `dark`, `light`.

Themes use a 4-layer depth model matching the original Norton Commander. Each layer (`ThemeLayer`) carries **both** a color palette (`Palette`) **and** a border character set (`BorderSet`):

| Layer | Depth | NC role |
|-------|-------|---------|
| Primary | 0 | Desktop: main windows, panels |
| Secondary | 1, 3, 5... | Menus, dropdowns, status bar |
| Tertiary | 2, 4, 6... | Dialogs, nested popups |
| Warning | (explicit) | Error/warning dialogs |

`Theme.LayerAt(depth)` returns the layer, cycling Secondary/Tertiary for depth > 0. `Theme.WarningLayer()` returns the Warning layer regardless of depth. `Theme.ShadowStyle()` is global (not per-layer).

**Color fields** (per layer): `bg/fg`, `accent`, `highlightBg/Fg`, `activeBg/Fg`, `inputBg/Fg`, `disabledFg`. **Border fields** (per layer): outer frame (`outerTL/TR/BL/BR/H/V`), inner dividers (`innerH/V`), intersections (`crossL/R/T/B/X`), bar separator (`barSep`). Defaults: double-line outer, single-line inner. Any omitted field falls back to hardcoded defaults. Different layers can use different border styles.

**Render signatures:** `Control.Render(buf, x, y, w, h, focused, layer)` writes directly into a `*ImageBuffer`. `Window.RenderToBuf(buf, x, y, w, h, layer)` writes into a caller-provided buffer. `Screen.RenderToBuf(buf, x, y, w, h, theme)` renders the full chrome (MenuBar at secondary layer, Window at primary, StatusBar at secondary). `MenuBar` renders directly into the buffer using `SetChar`/`WriteString` (no lipgloss). Dropdown/dialog renderers still return strings painted via `PaintANSI` + `Blit`.

**Widget tree reconciler** (`internal/widget/reconcile.go`): `ReconcileGameWindow()` builds real `Control` instances from a `WidgetNode` tree, reusing controls by tree path to preserve state (focus, cursor, scroll) across frames. Supports interactive nodes: `button` (action via OnInput), `textinput` (submit via OnInput), `checkbox` (toggle via OnInput), `textview` (scrollable), `gameview` (optionally focusable). NC framework owns focus -- Tab cycles controls, Esc blurs all, unfocused keys fall through to `game.OnInput()`.

**JSON backwards compat**: Global border fields at the theme root are copied into any layer that has empty borders via `resolveDefaults()`. New themes should define borders per-layer.
