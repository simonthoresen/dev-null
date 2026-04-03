package render

import (
	"encoding/json"
	"fmt"
	"os"
)


// PUA (Private Use Area) codepoint range used for charmap sprites.
// Games write these into ImageBuffer cells; the custom client renders them
// as sprites from a sprite sheet. Regular SSH clients show tofu/blank.
const (
	PUAStart rune = '\uE000'
	PUAEnd   rune = '\uF8FF'
)

// IsPUA reports whether r is in the Unicode Private Use Area range
// reserved for charmap sprites.
func IsPUA(r rune) bool {
	return r >= PUAStart && r <= PUAEnd
}

// CharMapEntry maps a single PUA codepoint to a region in the sprite atlas.
type CharMapEntry struct {
	Codepoint rune   `json:"codepoint"` // PUA codepoint (U+E000..U+F8FF)
	X         int    `json:"x"`         // sprite X offset in atlas (pixels)
	Y         int    `json:"y"`         // sprite Y offset in atlas (pixels)
	W         int    `json:"w"`         // sprite width (pixels)
	H         int    `json:"h"`         // sprite height (pixels)
	Name      string `json:"name"`      // human-readable name, e.g. "player_ship"
}

// CharMapDef is the full charmap definition shared between server and client.
type CharMapDef struct {
	Name       string         `json:"name"`       // e.g. "pacman"
	Version    int            `json:"version"`     // for client-side cache invalidation
	CellWidth  int            `json:"cellWidth"`   // sprite cell width in pixels
	CellHeight int            `json:"cellHeight"`  // sprite cell height in pixels
	Atlas      string         `json:"atlas"`       // filename of the PNG sprite sheet
	Entries    []CharMapEntry `json:"entries"`

	lookup map[rune]*CharMapEntry // built lazily by Lookup
}

// buildLookup populates the codepoint→entry map from Entries.
func (d *CharMapDef) buildLookup() {
	d.lookup = make(map[rune]*CharMapEntry, len(d.Entries))
	for i := range d.Entries {
		d.lookup[d.Entries[i].Codepoint] = &d.Entries[i]
	}
}

// Lookup returns the entry for a PUA codepoint, or nil if not mapped.
func (d *CharMapDef) Lookup(r rune) *CharMapEntry {
	if d.lookup == nil {
		d.buildLookup()
	}
	return d.lookup[r]
}

// UnmarshalJSON decodes a CharMapDef from JSON and eagerly builds the
// codepoint lookup map.
func (d *CharMapDef) UnmarshalJSON(data []byte) error {
	type Alias CharMapDef // prevent recursion
	var raw Alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = CharMapDef(raw)
	d.buildLookup()
	return nil
}

// LoadCharMapDef reads and parses a charmap JSON file.
func LoadCharMapDef(jsonPath string) (*CharMapDef, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read charmap %s: %w", jsonPath, err)
	}
	var def CharMapDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse charmap %s: %w", jsonPath, err)
	}
	return &def, nil
}
