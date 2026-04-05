package engine

import (
	"image/color"
	"testing"

	"github.com/dop251/goja"

	"dev-null/internal/render"
)

func TestParseJSColorValid(t *testing.T) {
	vm := goja.New()
	v := vm.ToValue("#ff8800")
	c := ParseJSColor(v)
	rgba, ok := c.(color.RGBA)
	if !ok {
		t.Fatalf("expected color.RGBA, got %T", c)
	}
	if rgba.R != 0xff || rgba.G != 0x88 || rgba.B != 0x00 {
		t.Fatalf("unexpected color: %v", rgba)
	}
}

func TestParseJSColorNull(t *testing.T) {
	vm := goja.New()
	if c := ParseJSColor(goja.Null()); c != nil {
		t.Fatalf("expected nil for null, got %v", c)
	}
	if c := ParseJSColor(goja.Undefined()); c != nil {
		t.Fatalf("expected nil for undefined, got %v", c)
	}
	if c := ParseJSColor(nil); c != nil {
		t.Fatalf("expected nil for nil, got %v", c)
	}
	// Invalid format.
	if c := ParseJSColor(vm.ToValue("red")); c != nil {
		t.Fatalf("expected nil for invalid format, got %v", c)
	}
}

func TestParseJSColorLowercase(t *testing.T) {
	vm := goja.New()
	c := ParseJSColor(vm.ToValue("#aaBBcc"))
	rgba := c.(color.RGBA)
	if rgba.R != 0xaa || rgba.G != 0xbb || rgba.B != 0xcc {
		t.Fatalf("unexpected color: %v", rgba)
	}
}

func TestHexNibble(t *testing.T) {
	tests := []struct {
		input byte
		want  uint8
	}{
		{'0', 0}, {'9', 9},
		{'a', 10}, {'f', 15},
		{'A', 10}, {'F', 15},
		{'g', 0}, {'z', 0},
	}
	for _, tt := range tests {
		got := hexNibble(tt.input)
		if got != tt.want {
			t.Errorf("hexNibble(%c) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestHexByte(t *testing.T) {
	if got := hexByte('f', 'f'); got != 0xff {
		t.Fatalf("expected 0xff, got 0x%02x", got)
	}
	if got := hexByte('0', '0'); got != 0x00 {
		t.Fatalf("expected 0x00, got 0x%02x", got)
	}
	if got := hexByte('1', 'a'); got != 0x1a {
		t.Fatalf("expected 0x1a, got 0x%02x", got)
	}
}

func TestParseJSAttr(t *testing.T) {
	vm := goja.New()

	if a := ParseJSAttr(nil); a != render.AttrNone {
		t.Fatalf("expected AttrNone for nil, got %d", a)
	}
	if a := ParseJSAttr(goja.Null()); a != render.AttrNone {
		t.Fatalf("expected AttrNone for null, got %d", a)
	}
	if a := ParseJSAttr(goja.Undefined()); a != render.AttrNone {
		t.Fatalf("expected AttrNone for undefined, got %d", a)
	}

	v := vm.ToValue(int(render.AttrBold | render.AttrItalic))
	a := ParseJSAttr(v)
	if a != render.AttrBold|render.AttrItalic {
		t.Fatalf("expected Bold|Italic, got %d", a)
	}
}
