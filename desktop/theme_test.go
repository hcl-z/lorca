package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The palette takes the desktop's accent, with dark text on a light one, and keeps its own blue
// where the desktop has none; the selected row and the ink stay neutral whatever the accent.
func TestPaletteFollowsTheDesktopAccent(t *testing.T) {
	var got *palette
	tt := ui.NewTester(func(c *ui.Context) { got = colors(c) }, 100, 100)
	tt.Frame()
	if got != &lightPalette {
		t.Fatalf("no accent: got %+v", got.Accent)
	}
	purple := ui.RGB(149, 61, 150)
	tt.SetPreferences(ui.Preferences{Accent: purple, TextScale: 1})
	tt.Frame()
	if got.Accent != purple || got.Selection != lightPalette.Selection || got.Ink != lightPalette.Ink || got.AccentText != lightPalette.AccentText {
		t.Errorf("purple: accent %+v, selection %+v, ink %+v, text %+v", got.Accent, got.Selection, got.Ink, got.AccentText)
	}
	tt.SetDark(true)
	tt.Frame()
	if got.Accent != purple || got.Window != darkPalette.Window || got.Ink != darkPalette.Ink {
		t.Errorf("dark purple: accent %+v, window %+v, ink %+v", got.Accent, got.Window, got.Ink)
	}
	yellow := ui.RGB(255, 204, 0)
	tt.SetPreferences(ui.Preferences{Accent: yellow, TextScale: 1})
	tt.Frame()
	if got.AccentText != ui.Hex("#000000") {
		t.Errorf("yellow: text %+v", got.AccentText)
	}
}
