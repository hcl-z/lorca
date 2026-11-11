package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The drawer above the composer, for the render the design is checked against.
func TestRenderComposerMenu(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-quill")
	tt := ui.NewTester(m.frame(m.view), 1000, 780)
	settleTransitions(tt)
	click(t, tt, L("Attach files"))
	if !tt.HasText(L("Instructions")) {
		t.Fatalf("the drawer did not open: %q", tt.Texts())
	}
	renderBoth(t, tt, "composer-menu")
}
