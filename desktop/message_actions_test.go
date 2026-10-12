package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Hovering a message brings its toolbar up beside the bubble without moving it; the pointer
// crosses onto the toolbar and it stays; Reply there quotes the message; away from both, it goes.
func TestMessageToolbarFloatsBesideTheBubble(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-quill")
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	// The sidebar's preview says the same words; without it the transcript's are the only ones.
	m.sidebarCollapsed = true
	settle(tt)
	words := "The email to Ana is ready above. Send it when it reads right."
	text, ok := tt.Find(words)
	if !ok {
		t.Fatalf("no message in %q", tt.Texts())
	}
	if _, shown := tt.Find(L("Message actions")); shown {
		t.Fatal("the toolbar is up before the pointer is")
	}
	tt.Move(text.X+20, text.Y+5)
	settle(tt)
	bar, shown := tt.Find(L("Message actions"))
	if !shown {
		t.Fatal("no toolbar under the pointer")
	}
	if after, _ := tt.Find(words); after != text {
		t.Errorf("the message moved: %+v, then %+v", text, after)
	}
	if bar.X < text.X+text.W {
		t.Errorf("the toolbar %+v is not beside the bubble's text %+v", bar, text)
	}
	renderBoth(t, tt, "message-toolbar")
	// Across onto the toolbar, and it stays.
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	settle(tt)
	if _, shown := tt.Find(L("Message actions")); !shown {
		t.Fatal("the toolbar went as the pointer reached it")
	}
	if err := tt.Click(L("Reply")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if m.chat.composer.reply == nil {
		t.Fatal("Reply did not quote the message")
	}
	tt.Move(5, 400)
	settle(tt)
	if _, shown := tt.Find(L("Message actions")); shown {
		t.Error("the toolbar stayed after the pointer left")
	}
}

// The toolbar goes beside the bubble when there is room, over its top corner when there is not,
// to the bubble's bottom when its top is under the header, never past the composer, and always
// inside the transcript.
func TestPlaceActionsKeepsClear(t *testing.T) {
	size := actionsSizeOf{W: 90, H: 34}
	safe := ui.Rect{X: 0, Y: 52, W: 800, H: 600} // the header above 52, the composer below 652
	row := ui.Rect{X: 0, Y: 200, W: 800, H: 80}
	cases := []struct {
		name   string
		bubble ui.Rect
		user   bool
		want   ui.Point
	}{
		// A bot's narrow bubble: beside it at its trailing side, level with its top.
		{"bot beside", ui.Rect{X: 22, Y: 214, W: 300, H: 60}, false, ui.Point{X: 22 + 300 + 6, Y: 14}},
		// The user's: at its leading side.
		{"user beside", ui.Rect{X: 400, Y: 214, W: 378, H: 60}, true, ui.Point{X: 400 - 6 - 90, Y: 14}},
		// A bot's bubble as wide as the transcript: over its top-right corner, half above it.
		{"bot corner", ui.Rect{X: 22, Y: 214, W: 760, H: 60}, false, ui.Point{X: 22 + 760 - 6 - 90, Y: 14 - 17}},
		// The user's, reaching the leading edge: over its top-left corner.
		{"user corner", ui.Rect{X: 30, Y: 214, W: 748, H: 60}, true, ui.Point{X: 30 + 6, Y: 14 - 17}},
		// Its top under the header: at the bubble's bottom instead.
		{"under header", ui.Rect{X: 22, Y: 20, W: 300, H: 200}, false, ui.Point{X: 328, Y: 20 + 200 - 34 - 200}},
		// Its bottom behind the composer: no lower than the composer's top.
		{"over composer", ui.Rect{X: 22, Y: 640, W: 300, H: 60}, false, ui.Point{X: 328, Y: 652 - 4 - 34 - 200}},
	}
	for _, tc := range cases {
		if got := placeActions(row, tc.bubble, size, tc.user, safe); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// A transcript narrower than the toolbar's room on either side still keeps it on screen.
	narrow := ui.Rect{X: 0, Y: 52, W: 200, H: 600}
	got := placeActions(ui.Rect{X: 0, Y: 200, W: 200, H: 80}, ui.Rect{X: 22, Y: 214, W: 170, H: 60}, size, false, narrow)
	if got.X < 4 || got.X+size.W > 200-4 {
		t.Errorf("narrow: %+v leaves the transcript", got)
	}
}
