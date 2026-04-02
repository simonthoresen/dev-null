package common

import "testing"

func TestIsPUA(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{0xDFFF, false},
		{'\uE000', true},
		{'\uE100', true},
		{'\uF8FF', true},
		{'\uF900', false},
		{'A', false},
		{' ', false},
	}
	for _, tt := range tests {
		if got := IsPUA(tt.r); got != tt.want {
			t.Errorf("IsPUA(%U) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func TestCharMapDefLookup(t *testing.T) {
	def := &CharMapDef{
		Entries: []CharMapEntry{
			{Codepoint: '\uE000', X: 0, Y: 0, W: 16, H: 16, Name: "player"},
			{Codepoint: '\uE001', X: 16, Y: 0, W: 16, H: 16, Name: "enemy"},
		},
	}

	if e := def.Lookup('\uE000'); e == nil || e.Name != "player" {
		t.Errorf("Lookup(E000) = %v, want player entry", e)
	}
	if e := def.Lookup('\uE001'); e == nil || e.Name != "enemy" {
		t.Errorf("Lookup(E001) = %v, want enemy entry", e)
	}
	if e := def.Lookup('\uE002'); e != nil {
		t.Errorf("Lookup(E002) = %v, want nil", e)
	}
}
