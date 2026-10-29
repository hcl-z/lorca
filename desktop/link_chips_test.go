package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A chip's label stays on one line, even when it has hyphens or the width
// changes: a chip that wraps paints its fill across the lines and over its
// neighbours.
func TestAChipStaysOnOneLine(t *testing.T) {
	text := "Two guides lead with what you can do, and the rest start with accounts:\n\n- [Getting started](https://docs.github.com/en/get-started) opens on a first repository\n- [Quickstart](https://docs.rs/serde/latest/serde/) starts from an install command\n- [Linear's docs](https://linear.app/docs) open on a sample project\n\nThe comparison table is in https://www.example.com/guides/compare, and the notes behind it are on https://notes.demo-sources.dev/setup. Both lean on the same [Getting started](https://docs.github.com/en/get-started) page."
	width := float32(700)
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Padding(20).Width(width).Children(func() {
			markdownView(c, text, markdownOptions{})
		})
	}, 760, 900)
	for _, w := range []float32{700, 900, 480} {
		width = w
		tt.Frame()
		tt.Frame()
		var chipText string
		for _, shown := range tt.Texts() {
			if strings.Contains(strings.ReplaceAll(shown, "\u2060", ""), "notes.demo-sources.dev") {
				chipText = shown
			}
		}
		if chipText == "" {
			t.Fatalf("width %v: the chip split across lines: %q", w, tt.Texts())
		}
		chip, ok := tt.Find(chipText)
		if !ok {
			t.Fatalf("width %v: no chip in %q", w, tt.Texts())
		}
		if chip.H > 30 {
			t.Fatalf("width %v: the chip wraps: %+v", w, chip)
		}
	}
	renderTo(t, tt, "link-chips")
}
