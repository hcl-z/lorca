package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/l10n"
	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// pendingPosts are what the store posted to the main thread in a test, run between frames.
var pendingPosts []func()

func runPosts() {
	for len(pendingPosts) > 0 {
		batch := pendingPosts
		pendingPosts = nil
		for _, fn := range batch {
			fn()
		}
	}
}

// demoWindow is the main window over the demo's store, as `LORCA_MOCK=1` shows it, without a
// window of its own.
func demoWindow(t *testing.T) *mainWindow {
	t.Helper()
	l10n.Set("en", "en-US")
	// Each test starts from the defaults, saving where no other run reads.
	prefs.loadFrom(filepath.Join(t.TempDir(), "preferences.json"))
	store = model.NewStore(nil, func(fn func()) { pendingPosts = append(pendingPosts, fn) }, true)
	app = &appDelegate{notifier: newNotifier()}
	store.Start()
	m := newMainWindow()
	m.userWantsInspector = true
	// The agents' floating windows repeat what the chat's cards show; their own test turns them on.
	m.pips.off = true
	app.main = m
	m.restoreSelection()
	return m
}

// renderTo writes the tester's last frame to $LORCA_RENDER/name.png, for looking at a view.
func renderTo(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("LORCA_RENDER")
	if dir == "" {
		return
	}
	file, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestRenderMainWindow(t *testing.T) {
	m := demoWindow(t)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	runPosts()
	for range 4 {
		tt.Frame()
	}
	if r, ok := tt.Find(L("Message %@ — @ to address one bot", "Launch room")); ok {
		t.Logf("composer field %+v", r)
	} else {
		t.Logf("no composer field")
	}
	renderTo(t, tt, "main-light")
	tt.SetDark(true)
	tt.Frame()
	renderTo(t, tt, "main-dark")
}

// The inspector slides closed: drawn narrower for a moment, then gone. With Reduce Motion it goes at
// once.
func TestInspectorSlidesClosed(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		m := demoWindow(t)
		m.selectChat("chat-patch")
		tt := ui.NewTester(m.frame(m.view), 1180, 760)
		tt.SetPreferences(ui.Preferences{ReduceMotion: reduce})
		settle(tt)
		closeButton, ok := tt.Find(L("Close Details"))
		if !ok {
			t.Fatal("no inspector")
		}
		m.toggleInspector()
		tt.Frame()
		if _, ok := tt.Find(L("Close Details")); ok == reduce {
			t.Fatalf("reduce motion %v: the inspector is drawn %v the frame after closing", reduce, ok)
		}
		if !reduce {
			if r, _ := tt.Find(L("Close Details")); r.X == closeButton.X {
				t.Error("the closing inspector did not move")
			}
			renderTo(t, tt, "inspector-closing")
		}
		time.Sleep(700 * time.Millisecond)
		settle(tt)
		if tt.HasText(L("Close Details")) {
			t.Errorf("reduce motion %v: the inspector stayed", reduce)
		}
	}
}

// TestRenderInspectorTabs opens a DM's inspector from the header's pill and walks its tabs.
func TestRenderInspectorTabs(t *testing.T) {
	m := demoWindow(t)
	m.userWantsInspector = false
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	if err := tt.Click(L("Show Details")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !m.userWantsInspector || !tt.HasText(L("Close Details")) {
		t.Fatalf("the pill left the inspector closed: %q", tt.Texts())
	}
	for _, tab := range inspectorTabs(store.Chat("chat-patch")) {
		if err := tt.Click(tab.Title); err != nil {
			t.Fatalf("no %s tab: %v", tab.Title, err)
		}
		settle(tt)
		renderTo(t, tt, "inspector-"+tab.Symbol)
	}
	// The selected tab's symbol draws over its thumb: dark strokes in the middle of a white segment.
	if r, ok := tt.Find(L("Routines")); !ok || !hasInk(tt.Image(), r) {
		t.Errorf("the selected tab's symbol is hidden under its thumb (%+v)", r)
	}
	if m.inspector.tab != tabRoutines || !tt.HasText(L("Routines")) {
		t.Errorf("tab %d shows %q", m.inspector.tab, tt.Texts())
	}
	if err := tt.Click(L("Close Details")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if m.userWantsInspector {
		t.Error("Close Details left the inspector open")
	}
}

func TestRenderPalette(t *testing.T) {
	m := demoWindow(t)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	runPosts()
	tt.Frame()
	m.palette.show()
	tt.Frame()
	renderTo(t, tt, "palette")
	tt.Type("developer")
	tt.Frame()
	if !tt.HasText("Developer") {
		t.Errorf("palette does not find Developer: %q", tt.Texts())
	}
	renderTo(t, tt, "palette-query")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if m.palette.open || m.selection.ChatID != "chat-patch" {
		t.Errorf("Return left the palette open (%v) on %+v", m.palette.open, m.selection)
	}
}

func TestRenderRunningCommand(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	run := &model.CommandRun{SessionID: "s1", Command: "npm install\nnpm test", State: model.CommandWaiting, Prompt: "Proceed? [y/N]", Output: "added 120 packages\nProceed? [y/N]", Device: "Studio", HandedOver: true}
	store.Append(&model.Message{
		ID:        "msg-run",
		Author:    model.BotAuthor("bot-patch"),
		Body:      model.Body{Kind: model.BodyTool, Tool: &model.ToolInvocation{Name: "bash", Summary: "$ npm install", Description: "Install dependencies", IsRunning: true, Run: run}},
		State:     model.MessageState{Kind: model.StateStreaming},
		CreatedAt: time.Now(),
	}, "chat-patch")
	time.Sleep(model.TaskDelay + 100*time.Millisecond)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	runPosts()
	for range 3 {
		tt.Frame()
	}
	renderTo(t, tt, "command-card")
	if err := tt.Click(L("Running tasks (%d)", 1)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	renderTo(t, tt, "running-tasks")
	if !tt.HasText(L("Run in Background")) {
		t.Errorf("no Run in Background in %q", tt.Texts())
	}
}

// settle runs what the store posted and draws a few frames.
func settle(tt *ui.Tester) {
	for range 6 {
		runPosts()
		tt.Frame()
	}
}

// settleTransitions waits out the sheet's entrance and recoloring, which run on the clock.
func settleTransitions(tt *ui.Tester) {
	time.Sleep(250 * time.Millisecond)
	settle(tt)
}

// renderBoth writes the frame in the light and the dark appearance.
func renderBoth(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	settleTransitions(tt)
	renderTo(t, tt, name+"-light")
	tt.SetDark(true)
	settleTransitions(tt)
	renderTo(t, tt, name+"-dark")
	tt.SetDark(false)
	settleTransitions(tt)
}

// hasInk is whether the middle of `r` in the frame holds a dark pixel, as a symbol's stroke.
func hasInk(img *image.RGBA, r ui.Rect) bool {
	scale := float32(img.Bounds().Dx()) / 1180
	cx, cy := (r.X+r.W/2)*scale, (r.Y+r.H/2)*scale
	for y := int(cy - 8*scale); y <= int(cy+8*scale); y++ {
		for x := int(cx - 8*scale); x <= int(cx+8*scale); x++ {
			if c := img.RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) < 3*140 {
				return true
			}
		}
	}
	return false
}

// countLines is how many times the frame shows `text` as a whole line of its own, labels included.
func countLines(tt *ui.Tester, text string) int {
	n := 0
	for _, line := range tt.Texts() {
		if line == text {
			n++
		}
	}
	return n
}

// showsLine is whether the frame shows `text` as a whole line of its own, as a section's caption
// is, where HasText would also find it inside a longer one ("Project" in "Project Manager").
func showsLine(tt *ui.Tester, text string) bool {
	return slices.Contains(tt.Texts(), text)
}
