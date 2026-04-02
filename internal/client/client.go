// Package client implements the null-space graphical client.
//
// Architecture overview:
//
//   SSH connection (golang.org/x/crypto/ssh)
//     ├── PTY channel        — standard terminal I/O
//     └── ENV vars           — NULL_SPACE_CLIENT=enhanced
//
//   ANSI parser
//     ├── Cell grid          — parsed from ANSI stream (inverse of ImageBuffer.ToString)
//     ├── OSC ns;charmap     — intercepts charmap definition
//     ├── OSC ns;atlas       — intercepts sprite sheet PNG
//     └── OSC ns;viewport    — intercepts game viewport bounds
//
//   Ebitengine renderer
//     ├── Text cells         — rendered with bundled monospace font
//     └── Sprite cells       — PUA codepoints rendered from charmap atlas
//
// The charmap only applies to the game viewport rectangle. NC chrome
// (menus, dialogs, chat, status bars) always renders as text.
package client
