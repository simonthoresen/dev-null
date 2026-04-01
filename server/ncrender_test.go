package server

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFitLineExact(t *testing.T) {
	got := fitLine("hello", 5)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestFitLinePad(t *testing.T) {
	got := fitLine("hi", 5)
	if got != "hi   " {
		t.Errorf("expected 'hi   ', got %q", got)
	}
}

func TestFitLineTruncate(t *testing.T) {
	got := fitLine("hello world", 5)
	if ansi.StringWidth(got) != 5 {
		t.Errorf("expected width 5, got %d", ansi.StringWidth(got))
	}
}

func TestFitLineEmpty(t *testing.T) {
	got := fitLine("", 3)
	if got != "   " {
		t.Errorf("expected 3 spaces, got %q", got)
	}
}
