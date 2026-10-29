package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A reply still being written shows what its unfinished Markdown will most
// likely become: an unclosed bold span reads bold, without its asterisks.
func TestAStreamingReplyShowsItsUnfinishedMarkdown(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Padding(20).Width(360).Children(func() {
			markdownStream(c, "A **bold", markdownOptions{})
		})
	}, 400, 200)
	texts := tt.Texts()
	joined := strings.Join(texts, "")
	if !strings.Contains(joined, "bold") {
		t.Fatalf("no bold word in %q", texts)
	}
	if strings.Contains(joined, "**") {
		t.Errorf("the asterisks still show: %q", texts)
	}
}

// A mention in the user's own bubble reads in its bot's color too.
func TestAMentionInAUserBubbleIsColored(t *testing.T) {
	bot := &model.Bot{ID: "bot-chef", Name: "Chef", Accent: "purple"}
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		p := colors(c)
		ui.Column(c).Padding(20).Width(400).Background(p.Ink).TextColor(p.InkText).Children(func() {
			markdownView(c, "Hi @Chef please check.", markdownOptions{OnUserBubble: true, Mentionable: []*model.Bot{bot}})
		})
	}, 460, 200)
	tt.Frame()
	tt.Frame()
	text, ok := tt.Find("Hi @Chef please check.")
	if !ok {
		t.Fatalf("no message in %q", tt.Texts())
	}
	accent := lightPalette.accentColor(bot.Accent)
	img := tt.Image()
	tinted := 0
	for y := int(text.Y); y < int(text.Y+text.H); y++ {
		for x := int(text.X); x < int(text.X+text.W); x++ {
			px := img.RGBAAt(x, y)
			if absDiff(px.R, accent.R) < 24 && absDiff(px.G, accent.G) < 24 && absDiff(px.B, accent.B) < 24 {
				tinted++
			}
		}
	}
	if tinted < 20 {
		t.Errorf("the mention is not in Chef's color (%d pixels)", tinted)
	}
}

// A drag runs across a message's paragraphs, list, and code, and Copy joins them a line apart,
// without the list's markers.
func TestMessageSelectsAcrossBlocks(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\n- one\n- two\n\n```\ncode line\n```"
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Padding(20).Width(360).Children(func() { markdownView(c, text, markdownOptions{}) })
	}, 400, 400)
	first, ok := tt.Find("First paragraph.")
	if !ok {
		t.Fatalf("no first paragraph: %q", tt.Texts())
	}
	last, ok := tt.Find("code line")
	if !ok {
		t.Fatalf("no code: %q", tt.Texts())
	}
	tt.Press(first.X+1, first.Y+first.H/2)
	tt.Move(last.X+last.W/2, last.Y+last.H/2)
	tt.Release(last.X+last.W-1, last.Y+last.H/2)
	tt.Key(ui.Cmd, ui.KeyC)
	want := "First paragraph.\nSecond paragraph.\none\ntwo\ncode line"
	if got := tt.Clipboard(); got != want {
		t.Errorf("copied %q, want %q", got, want)
	}
}

// A web link in a bot's message reads as a source chip inside its paragraph, in the site's words
// or the link's own, with room for the icon; the user's own bubble keeps the link as text.
func TestMessageLinksAreSourceChips(t *testing.T) {
	text := "Read [the guide](https://www.example.com/guide), https://docs.rs/serde and mail a@b.co."
	build := func(o markdownOptions) *ui.Tester {
		tt := ui.NewTester(func(c *ui.Context) {
			applyTheme(c)
			ui.Column(c).Padding(20).Width(420).Children(func() { markdownView(c, text, o) })
		}, 460, 120)
		// The chips are painted from the last frame's layout.
		tt.Frame()
		tt.Frame()
		return tt
	}
	chip := chipWords
	bot := build(markdownOptions{})
	for _, want := range []string{chip("the guide"), chip("docs.rs")} {
		if _, ok := bot.Find(want); !ok {
			t.Errorf("no chip %q in %q", want, bot.Texts())
		}
	}
	if slices.Contains(bot.Texts(), "https://docs.rs/serde") {
		t.Errorf("the address is still shown: %q", bot.Texts())
	}
	// Mail stays a link in the text.
	if !slices.Contains(bot.Texts(), "a@b.co") {
		t.Errorf("the mail link changed: %q", bot.Texts())
	}
	renderTo(t, bot, "source-chips")
	user := build(markdownOptions{OnUserBubble: true})
	if !slices.Contains(user.Texts(), "https://docs.rs/serde") || slices.Contains(user.Texts(), chip("docs.rs")) {
		t.Errorf("the user's bubble changed its links: %q", user.Texts())
	}
}

// A right-click on a message's text offers Reply above Copy and Select All, as the Mac's does:
// Reply starts a reply to it, and the others act on its text.
func TestMessageTextMenu(t *testing.T) {
	m := demoWindow(t)
	tt := ui.NewTester(m.frame(m.view), 1000, 700)
	runPosts()
	// The transcript settles at its end over its first frames.
	for range 5 {
		tt.Frame()
	}
	time.Sleep(300 * time.Millisecond)
	tt.Frame()
	bubble, ok := tt.Find("Great. Keep the announcement as a draft until I've reviewed it.")
	if !ok {
		t.Fatalf("no message: %q", tt.Texts())
	}
	tt.RightClickAt(bubble.X+40, bubble.Y+20)
	if want := []string{L("Reply"), L("Save as Standing Instruction…"), "-", "Copy", "-", "Select All"}; !slices.Equal(tt.Menu(), want) {
		t.Fatalf("menu %q, want %q", tt.Menu(), want)
	}
	if err := tt.ChooseMenuItem(L("Reply")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if m.chat.composer.reply == nil {
		t.Error("Reply started no reply")
	}
	// Select All and Copy from the same menu take the message's text. The reply's bar moved it.
	for _, item := range []string{"Select All", "Copy"} {
		for range 3 {
			tt.Frame()
		}
		bubble, _ = tt.Find("Great. Keep the announcement as a draft until I've reviewed it.")
		tt.RightClickAt(bubble.X+40, bubble.Y+20)
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if got := tt.Clipboard(); got != "Great. Keep the announcement as a draft until I've reviewed it." {
		t.Errorf("copied %q", got)
	}
}

// A message's paragraphs sit close, as they read in a chat; a document keeps its looser spacing.
func TestMessageParagraphsSitClose(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		if gap := markdownTheme(c, markdownOptions{}).BlockSpacing; gap != paragraphGap || gap > 0.7 {
			t.Errorf("a message's paragraphs are %v em apart, want %v", gap, paragraphGap)
		}
		if gap := markdownTheme(c, markdownOptions{Document: true}).BlockSpacing; gap != documentParagraphGap {
			t.Errorf("a document's paragraphs are %v em apart, want %v", gap, documentParagraphGap)
		}
	}, 100, 100)
	tt.Frame()
}

// A wide table scrolls sideways and sits as close to the text around it as any other block: a cell
// is one line, so the row's height is the table's, and no blank band opens above or below it.
func TestAWideTableSitsCloseToItsText(t *testing.T) {
	text := "Before.\n\n| Site | Size | What |\n| --- | --- | --- |\n| ~/Library/Application Support | 39G | LarkShell 11G、Cursor 2.6G、Google 2.5G、Citro Labs 2.5G |\n| ~/Library/Developer | 20G | CoreSimulator 13G |\n\nAfter."
	for _, width := range []float32{300, 420, 560} {
		tt := ui.NewTester(func(c *ui.Context) {
			applyTheme(c)
			ui.Column(c).Padding(20).Width(width).Children(func() { markdownView(c, text, markdownOptions{}) })
		}, int(width)+80, 900)
		tt.Frame()
		before, ok := tt.Find("Before.")
		if !ok {
			t.Fatalf("width %v: no text before the table", width)
		}
		head, ok := tt.Find("Site")
		if !ok {
			t.Fatalf("width %v: no table header", width)
		}
		last, ok := tt.Find("~/Library/Developer")
		if !ok {
			t.Fatalf("width %v: no last row", width)
		}
		after, ok := tt.Find("After.")
		if !ok {
			t.Fatalf("width %v: no text after the table", width)
		}
		if gap := head.Y - (before.Y + before.H); gap > 30 {
			t.Errorf("width %v: the table starts %v below the text before it", width, gap)
		}
		if gap := after.Y - (last.Y + last.H); gap > 40 {
			t.Errorf("width %v: the text after the table starts %v below it", width, gap)
		}
	}
}
