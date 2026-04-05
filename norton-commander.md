# Norton Commander 5.0 — UI Design Reference

> Source: https://ilyabirman.net/meanwhile/all/ui-museum-norton-commander-5-0/
>
> This document captures Norton Commander's visual design language as inspiration for dev-null's terminal UI. NC5 ran in 16-color EGA/VGA text mode at 80×25 characters.

---

## Box-Drawing Characters

NC5 uses the full IBM CP437 box-drawing set. Two distinct styles are used intentionally:

**Double-line** — used for the outer frames of the main panels:
```
╔ ═ ╗
║   ║
╚ ═ ╝
```
| Char | Unicode | Usage |
|------|---------|-------|
| `═`  | U+2550  | Double horizontal |
| `║`  | U+2551  | Double vertical |
| `╔`  | U+2554  | Top-left corner |
| `╗`  | U+2557  | Top-right corner |
| `╚`  | U+255A  | Bottom-left corner |
| `╝`  | U+255D  | Bottom-right corner |
| `╠`  | U+2560  | Left T-junction |
| `╣`  | U+2563  | Right T-junction |
| `╦`  | U+2566  | Top T-junction |
| `╩`  | U+2569  | Bottom T-junction |

**Single-line** — used for dialogs, group boxes, and menu separators:
```
┌ ─ ┐
│   │
└ ─ ┘
```
| Char | Unicode | Usage |
|------|---------|-------|
| `─`  | U+2500  | Horizontal |
| `│`  | U+2502  | Vertical |
| `┌`  | U+250C  | Top-left |
| `┐`  | U+2510  | Top-right |
| `└`  | U+2514  | Bottom-left (also tree connectors) |
| `┘`  | U+2518  | Bottom-right |
| `├`  | U+251C  | Left T |
| `┤`  | U+2524  | Right T |

**Special characters used in UI:**
| Char  | Unicode | Usage |
|-------|---------|-------|
| `░`   | U+2591  | Light shade — separator between filename stem and extension for system files |
| `►`   | U+25BA  | Right-pointing triangle — directory type indicators in status bar (`►SUB-DIR◄`) |
| `◄`   | U+25C4  | Left-pointing triangle — same |
| `·`   | U+00B7  | Middle dot — fills unfocused input field width (`[············]`) |
| `√`   | CP437   | Checkmark bullet in menu — marks the currently active panel mode (renders as `J` in some fonts) |

---

## Color Palette

All 16 EGA colors. The core scheme:

| Element | Background | Foreground |
|---------|-----------|------------|
| Main panels | Dark Blue `#0000AA` | Bright White `#FFFFFF` |
| Active panel title | Bright Cyan `#55FFFF` | Black `#000000` |
| Inactive panel title | Dark Blue | Cyan `#00AAAA` |
| Cursor / selected row | Bright Cyan | Black |
| Directories | Dark Blue | Bright White (ALL CAPS) |
| Files | Dark Blue | White |
| Insert-selected files | Dark Blue | Yellow `#FFFF55` |
| Panel status line | Dark Blue | Bright White |
| Selection count line | Dark Blue | Yellow |
| Menu bar (inactive items) | Black `#000000` | Cyan |
| Menu bar (active item) | Black | Yellow |
| Dropdown body | Black | Yellow (items) |
| Dropdown shortcuts | Black | Cyan (right-aligned) |
| Dropdown highlighted item | Reverse/bright | White or Yellow |
| Standard dialog | Light Grey `#AAAAAA` | Black |
| Outer dialog (with children) | Cyan `#00AAAA` | Yellow or White |
| Error/warning dialog | Bright Red `#FF5555` | Yellow (title), White (body) |
| Active button | Yellow | Black |
| Inactive button | Grey/dark | White |
| Checkbox/radio active mark | (parent bg) | Yellow |
| Function key bar | Black | Cyan (numbers) + White (labels) |
| Drop shadow | Black | — |

**Color scheme options:** NC5 was the first version to offer color scheme selection: `B & W`, `Laptop`, `Color1`, `Color2`.

---

## Drop Shadows

Every modal dialog has a drop shadow — a key part of the NC visual identity.

- Shadow = solid black cells, 1 character right and 1–2 characters below the dialog
- Not a darkened version of the background — just pure black
- This was the only technique available with 16 colors: "There was no way to make the colours darker in the shadows, so to simulate the darkened cyan-on-blue or white-on-cyan, the applications of the era used grey-on-black."
- The shadow forms an L-shape along the right and bottom edges of the dialog
- NC5 borrowed this technique from **Turbo Vision** (Borland's TUI framework)
- The Help window is the only overlay that has no shadow (an inconsistency the author notes)

---

## Z-Ordering: Three-Layer Visual Hierarchy

NC5 uses color to signal nesting depth, creating a deliberate three-layer system:

```
Layer 1 (bottom): Blue main panels
Layer 2 (middle): Cyan dialogs — outer dialogs that can contain children
Layer 3 (top):    Grey dialogs — leaf dialogs with no children
```

Each layer has its own drop shadow on the layer below it. This lets users understand nesting at a glance without explicit visual chrome.

---

## Main Panel Layout (80×25 VGA)

```
╔══════════ C:\ ══════════╦══════════ C:\NC ══════════╗
║ Name         Size  Date ║ Name         Size  Date   ║
║ DOS                     ║ .                         ║
║ NC                      ║ ..                        ║
║ autoexec  bat  78  7-21 ║ 123view  exe  15k  7-21   ║
║ command   com  94k 7-21 ║ 4372ansi set  1k   7-21   ║
║ ...                     ║ ...                       ║
╠═════════════════════════╬═══════════════════════════╣
║ autoexec.bat  78  7-21-17  3:15p    .  ►UP--DIR◄   ║
╚═════════════════════════╩═══════════════════════════╝
C:\NC>
1Help  2Menu  3View  4Edit  5Copy  6RenMov  7Mkdir  8Delete  9PullDn  10Quit
```

Key layout details:
- Screen: **80 columns × 25 rows**
- Two panels side-by-side, ~50/50 split, double-line borders with shared center divider
- **Panel title** (path) embedded centered in the top border, replacing the border characters at that position
- **Active panel**: title in bright cyan-on-black; cursor row highlighted in bright cyan-on-black (reverse video)
- **Inactive panel**: title dimmer; no cursor highlight
- **Panel status line** (bottom of each panel): shows `name  size  date  time` of highlighted file, or `►SUB-DIR◄` / `►UP--DIR◄` for directories
- **Selection status**: when files selected with Insert → `1,234,567 bytes in 3 selected files` (in yellow)
- **Command prompt**: one line below panels, shows current dir and prompt character
- **Function key bar**: always-visible bottom row

### Panel display modes
- **Brief**: three columns of filenames only (no size/date)
- **Full**: one column, name + size + date + time
- **Info**: shows directory stats
- **Tree**: directory tree view
- **Quick view**: one panel shows text preview of file highlighted in other panel

---

## Pull-Down Menu (F9)

```
 Left   Files   Disk   Commands   Right
       ┌──────────────────────────────┐
       │ Help                      F1 │
       │ User menu                 F2 │
       ├──────────────────────────────┤
       │ J Brief               Ctrl-F1│
       │   Full                Ctrl-F2│
       │   Info                Ctrl-F3│
       │   Tree                Ctrl-F4│
       │   Quick view          Ctrl-F5│
       ├──────────────────────────────┤
       │ Sort by name          Ctrl-F6│
       └──────────────────────────────┘
```

- Menu bar on row 1: five items, non-active in cyan-on-black, active in yellow-on-black
- Dropdown: black background, no top border (abuts the menu bar), single-line left/right/bottom border
- Items: yellow text + cyan shortcut right-aligned
- Highlighted item: full-row reverse/bright highlight
- Separator lines: `─────────` in dark/grey, grouping related items
- Active mode marker: `J` bullet (checkmark in CP437 font) in leftmost column
- Accelerator letters in item names are rendered in a contrasting color

### Function key bar
```
1Help  2Menu  3View  4Edit  5Copy  6RenMov  7Mkdir  8Delete  9PullDn  10Quit
```
- Number in cyan, label in white, no separators, packed together
- Labels change by context (e.g. inside Find File: `1Help  2Drive  3Tree  4Advanced  ...  10Quit`)

---

## Dialog Designs

### Standard grey dialog (leaf operation)
```
        ┌────── Copy ──────┐
        │ Copy file to:    │  ← black text on grey
        │ [············]   │  ← input field
        │                  │
        │ [x] Ask for each │  ← checkbox, x in yellow
        │                  │
        │  [ Copy ] [Cancel]│ ← buttons; active = yellow bg
        └──────────────────┘
         (black shadow offset right+down)
```
- Single-line border, title centered in top border
- Grey background (`#AAAAAA`)
- Input field: `[············]` — square brackets, middle-dot fill, black bg when focused
- Checkbox: `[ ]` / `[x]` — `x` in yellow
- Radio button: `( )` / `(·)` — center dot in yellow; `•` (solid) for selected
- Group box: single-line rectangle inside dialog, label in yellow at top-left: `┌─ Search Locations ─┐`
- Buttons: `  Copy  ` `  Cancel  ` — active button is yellow-on-black
- Accelerator letters in button labels rendered in contrasting color

### Cyan outer dialog (contains sub-dialogs)
- Same structure, but cyan background
- Group labels and item text in yellow on cyan
- Grey sub-dialogs layer on top with their own shadow

### Error / warning dialog
- Bright red background
- Title `Warning` in yellow (no `?` — NC avoids question marks in titles)
- Body text in white
- `Ok` button in yellow

### Notable UX decisions
- **Make Directory (F7)** has no button — just an input field; press Enter to confirm
- **Copy (F5)** and **RenMov (F6)** pre-populate the destination with the opposite panel's current path
- **Rename and Move are unified** (F6 = `RenMov`) — because at the OS level they are the same operation (changing a full path)
- Dialog titles have **no question marks**: "Do you wish to delete autoexec.bat" not "Delete?"

---

## Typography Conventions

**Filenames in panels:**
- Directories: `ALL CAPS` (e.g. `DOS`, `NC`)
- Regular files: lowercase stem + lowercase extension in separate column (dot hidden): `autoexec  bat`
- System files: halftone `░` character between stem and extension: `Io░sys`
- Parent directory: `..` at top of list

**Labels:**
- Dialog/group titles: centered in border or at top-left, in yellow on the dialog background color
- Keyboard shortcuts in menus: right-aligned in cyan, format: `F1`, `Ctrl-F1`, `Alt-F1`, `Gray +`
- Accelerator letters in menu items and button labels: rendered in brighter/contrasting color

**Context-sensitive labels:** The function key bar adapts to the current mode. The bottom status line of each panel adapts to show either file info or selection count.

---

## Other Noteworthy Details

- **`Ctrl+O`** hides both panels, revealing the underlying DOS session output — panels are an overlay, not a replacement
- **EGA Lines mode**: doubles vertical density (25→50 rows); rarely used in practice
- **Quick search**: type a letter prefix while in a panel → small overlay appears at panel bottom showing typed characters, list jumps to first match
- **NCD (Norton Change Directory)**: full-screen cyan overlay with ASCII tree connectors (`└─`, `│`), speed-search input at bottom
- **Visual lineage**: the shadow technique and grey/cyan dialog color system were borrowed from **Turbo Vision** (Borland's TUI framework for Pascal/C++)

---

## Design Principles to Steal

1. **Double-line borders for top-level panels, single-line for dialogs** — the border weight communicates hierarchy
2. **Color signals dialog nesting depth**: blue → cyan → grey (not arbitrary)
3. **Pure black drop shadows** — simple, effective, achievable with standard terminal colors
4. **Title embedded in the border line** — saves a row and looks elegant
5. **Active panel is visually dominant** — brighter colors, cursor highlight
6. **Keyboard shortcut display** — right-aligned in a different color, every menu item
7. **Function key bar** — always visible, always contextual, number color + label color split
8. **Input fields telegraphed by dot fill** — user knows the width of the field before typing
9. **Checkboxes and radio buttons are visually distinct** — `[ ]`/`[x]` vs `( )`/`(·)`
10. **No decoration for decoration's sake** — every element has a job
