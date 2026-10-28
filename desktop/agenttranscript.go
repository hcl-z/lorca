package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// A coding agent's transcript as its sheet and its full view draw it, from the entries its Runner
// keeps: what it was sent, beside a bar; what it said, as a message is; each tool it used as a
// line with its state, which opens to its command's output on a terminal, its edits as a diff, or
// what it gave back; what it thought, a line that opens to it; its plan as a checklist; its
// notices, marked by how much they matter; each time it compacted its context, a line that opens to
// what it kept; and Lorca's notes. A tool that runs, one that
// failed, and the last one are open; a click opens or closes any. The view opens on the newest page
// of entries and asks the Runner again every two seconds, five through the relay, for what
// changed; it asks for the page before the oldest when the top comes into view, and keeps to the
// end unless the user scrolled up to read.

const (
	// The live terminals one view has at most, the latest; an older open output is plain text.
	entryScreens = 6
	// The entries one page of a transcript carries: a couple of screens, so the top loads with
	// little wait.
	transcriptPage = 20
	// A command's output grows to this many rows, then shows its last ones.
	outputRows = 14
	outputRow  = 15.5
)

type agentTranscript struct {
	chatID, messageID string
	remote            bool
	entries           []model.AgentEntry
	rev               uint64
	// text is the transcript as the bot reads it, shown by a Runner that keeps no entries.
	text   string
	err    string
	loaded bool
	closed bool
	timer  *time.Timer
	list   ui.ListState
	// hasOlder says a page before the oldest entry here is left on the Runner; loadingOlder
	// keeps one such page in flight.
	hasOlder     bool
	loadingOlder bool
	open         map[uint64]bool
	screens      map[uint64]*agentScreen
	// folds is what each said entry's text comes to for a folded row, kept between frames.
	folds map[uint64]foldedText
	// fill is what the transcript stands on, which a folded entry's fade runs into.
	fill ui.Color
	// maxWidth keeps the entries to a reading width, centered, where the view is wide; 0 fills it.
	maxWidth float32
	// kind is the agent's, for its mark beside what it said; bot is who sends it its work.
	kind, bot string
}

// plan is the steps of the latest plan the agent made.
func (t *agentTranscript) plan() []model.AgentStep {
	for i := len(t.entries) - 1; i >= 0; i-- {
		if t.entries[i].Kind == "plan" {
			return t.entries[i].Steps
		}
	}
	return nil
}

func newAgentTranscript(chatID string, message *model.Message) *agentTranscript {
	t := &agentTranscript{chatID: chatID, messageID: message.ID, remote: !store.RunsHere(message), open: map[uint64]bool{}, screens: map[uint64]*agentScreen{}, folds: map[uint64]foldedText{}, bot: botName(message)}
	t.list.FollowEnd = true
	if agent := model.AgentOf(message); agent != nil {
		t.kind = agent.Kind
	}
	t.load()
	return t
}

func (t *agentTranscript) load() {
	if t.closed {
		return
	}
	var since *uint64
	if t.loaded {
		rev := t.rev
		since = &rev
	}
	// The newest page first; every refresh after it asks only for what changed.
	limit := 0
	if !t.loaded {
		limit = transcriptPage
	}
	store.AgentTranscript(t.chatID, t.messageID, since, nil, limit, func(page model.AgentTranscriptPage, err error) {
		if t.closed {
			return
		}
		switch {
		case err != nil:
			// What was shown stays; the next try may reach the Runner.
			if !t.loaded {
				t.err = model.ErrorText(err)
			}
		case t.loaded && page.Rev < t.rev:
			// The Runner counts afresh, as after a restart that kept no entries: all of them again.
			t.loaded, t.entries, t.rev = false, nil, 0
			t.load()
			return
		default:
			entries, _ := model.MergeAgentEntries(t.entries, page)
			t.entries, t.rev, t.text, t.loaded, t.err = entries, page.Rev, page.Text, true, ""
			t.hasOlder = len(entries) > 0 && entries[0].Seq > page.First
		}
		t.schedule()
	})
}

// loadOlder asks for the page before the oldest entry the view holds, once.
func (t *agentTranscript) loadOlder() {
	if t.closed || t.loadingOlder || !t.hasOlder || len(t.entries) == 0 {
		return
	}
	t.loadingOlder = true
	before := t.entries[0].Seq
	store.AgentTranscript(t.chatID, t.messageID, nil, &before, transcriptPage, func(page model.AgentTranscriptPage, err error) {
		t.loadingOlder = false
		if t.closed || err != nil {
			return
		}
		entries, _ := model.MergeAgentEntries(t.entries, page)
		t.entries = entries
		t.hasOlder = len(entries) > 0 && entries[0].Seq > page.First
	})
}

func (t *agentTranscript) schedule() {
	if t.closed || store.IsMock {
		return
	}
	every := 2 * time.Second
	if t.remote {
		every = 5 * time.Second
	}
	t.timer = time.AfterFunc(every, func() { post(t.load) })
}

func (t *agentTranscript) close() {
	t.closed = true
	if t.timer != nil {
		t.timer.Stop()
	}
	for seq, s := range t.screens {
		s.close()
		delete(t.screens, seq)
	}
}

// isOpen is whether a tool's entry shows what it did: as the user left it, else while it runs,
// when it failed, and for the last one.
func (t *agentTranscript) isOpen(e *model.AgentEntry, last bool) bool {
	if open, ok := t.open[e.Seq]; ok {
		return open
	}
	return e.Tool.Running() || e.Tool.Failed() || last
}

func hasBody(tool *model.AgentTool) bool {
	return tool.Output != "" || len(tool.Diffs) > 0 || tool.Result != ""
}

// view draws the entries as a list, which builds only the rows in view and keeps the reader's
// place when a page of older entries lands above it; the caller sizes it.
func (t *agentTranscript) view(c *ui.Context) ui.Element {
	p := colors(c)
	if len(t.entries) == 0 {
		box := ui.Scroll(c).FillWidth().TextColor(p.Label)
		box.Children(func() {
			box := ui.Column(c).Padding(4, 2, 12, 2)
			if t.maxWidth > 0 {
				box.MaxWidth(t.maxWidth)
			}
			box.Children(func() { t.empty(c, p) })
		})
		return box
	}
	// The open outputs with a terminal of their own, the latest first; the rest give theirs up.
	lastTool := -1
	for i := range t.entries {
		if tool := t.entries[i].Tool; tool != nil && hasBody(tool) {
			lastTool = i
		}
	}
	live := map[uint64]bool{}
	for i := len(t.entries) - 1; i >= 0 && len(live) < entryScreens; i-- {
		e := &t.entries[i]
		if e.Tool != nil && e.Tool.Output != "" && t.isOpen(e, i == lastTool) {
			live[e.Seq] = true
		}
	}
	for seq, s := range t.screens {
		if !live[seq] {
			s.close()
			delete(t.screens, seq)
		}
	}
	t.list.Key = func(i int) any { return t.entries[i].Seq }
	list := ui.List(c, &t.list, len(t.entries), func(i int) {
		e := &t.entries[i]
		// The rail runs on to the next entry with a node.
		rail := i+1 < len(t.entries) && t.entries[i+1].Kind != "sent" && t.entries[i+1].Kind != "log"
		row := ui.Row(c.Key(fmt.Sprintf("row:%d", e.Seq))).FillWidth().Justify(ui.Center)
		row.Children(func() {
			column := ui.Column(c).Grow(1).Shrink(1).MinWidth(0)
			if t.maxWidth > 0 {
				column.MaxWidth(t.maxWidth)
			}
			column.Children(func() { t.entry(c, p, e, i == lastTool, live[e.Seq], rail) })
		})
	}).Fill().Padding(4, 2, 12, 2).TextColor(p.Label)
	// Nearing the first entry: the page before it.
	if first, last := t.list.Visible(); last >= first && first < 4 && t.hasOlder {
		t.loadOlder()
	}
	return list
}

// empty draws what a transcript with no entries says: its text, an error, or that it is loading.
func (t *agentTranscript) empty(c *ui.Context, p *palette) {
	switch {
	case t.text != "":
		// A Runner that keeps no entries for it answers the bot's text.
		transcriptText(c, p, t.text)
	case t.err != "":
		ui.Text(c, t.err).FontSize(12).TextColor(p.Label2)
	case t.loaded:
		ui.Text(c, L("Nothing yet")).FontSize(12).TextColor(p.Label2)
	default:
		ui.Text(c, L("Loading…")).FontSize(12).TextColor(p.Label2)
	}
}

// node is an entry's place on the rail: the agent's mark for what it said, a tool's state, or a
// quiet mark of its kind. None for what was sent, which starts a turn of its own.
func (t *agentTranscript) node(c *ui.Context, p *palette, e *model.AgentEntry) bool {
	if e.Kind == "sent" || e.Kind == "log" {
		return false
	}
	disc := ui.Box(c.Key("node")).Size(22, 22).Shrink(0).Radius(11).Background(p.Content).Justify(ui.Center).AlignItems(ui.Center)
	disc.Children(func() {
		ring := func(tint ui.Color) ui.Element {
			return ui.Box(c).Size(22, 22).Radius(11).Background(tint.Alpha(0.1)).Border(0.5, tint.Alpha(0.28)).Justify(ui.Center).AlignItems(ui.Center).TextColor(tint)
		}
		switch e.Kind {
		case "said":
			agentTile(c, p, t.kind, 22).Radius(11)
		case "tool":
			tool := e.Tool
			switch {
			case tool == nil:
			case tool.Running():
				ring(p.Accent).Children(func() { spinner(c, 11) })
			case tool.Failed():
				ring(p.Red).Children(func() { symbol(c, "xmark", 11, 2.4) })
			default:
				ring(p.Label2).Children(func() { symbol(c, toolSymbol(tool.Kind), 11, 2) })
			}
		case "thought":
			ring(p.Purple).Children(func() { symbol(c, "sparkles", 11, 2) })
		case "plan":
			ring(p.Green).Children(func() { symbol(c, "checklist", 11, 2) })
		case "compaction":
			ring(p.Teal).Children(func() {
				if e.Status == "" || e.Status == "in_progress" {
					spinner(c, 11)
				} else {
					symbol(c, "arrow.down.right.and.arrow.up.left", 11, 2)
				}
			})
		case "notice":
			tint, mark := p.Label2, "info.circle"
			switch e.Level {
			case "warning":
				tint, mark = p.Orange, "exclamationmark.triangle"
			case "error":
				tint, mark = p.Red, "xmark.circle"
			}
			ring(tint).Children(func() { symbol(c, mark, 11, 2) })
		default:
			ui.Box(c).Size(6, 6).Radius(3).Background(p.Label3)
		}
	})
	return true
}

// entry draws one entry of the transcript: on the rail beside its node, or as a turn's start.
// `rail` carries the rail on below it, to the next entry's node.
func (t *agentTranscript) entry(c *ui.Context, p *palette, e *model.AgentEntry, last, live, rail bool) {
	key := c.Key(fmt.Sprintf("entry:%d", e.Seq))
	switch e.Kind {
	case "sent":
		// What the bot sent it starts a turn: who sent it, then the words.
		ui.Column(key).FillWidth().Gap(5).Margin(6, 0, 16, 0).Padding(10, 13, 11, 13).Radius(radiusLG).Background(p.Accent.Alpha(0.06)).Border(0.5, p.Accent.Alpha(0.18)).Children(func() {
			ui.Row(c).Gap(5).AlignItems(ui.Center).TextColor(p.Accent).Children(func() {
				symbol(c, "paperplane", 11, 2)
				ui.Text(c, L("%@ asked", t.bot)).FontSize(11).FontWeight(600).SingleLine()
			})
			ui.Text(c, e.Text).FontSize(13).LineHeight(1.45).TextColor(p.Label).MaxLines(12).Selectable().Tooltip(e.Text)
		})
		return
	case "log":
		ui.Column(key).FillWidth().Margin(0, 0, 14, 0).Padding(8, 10).Radius(8).Background(p.Code).Children(func() { transcriptText(c, p, e.Text) })
		return
	}
	ui.Row(key).FillWidth().Gap(12).AlignItems(ui.Stretch).Children(func() {
		gutter := ui.Box(c.Key("gutter")).Width(22).Shrink(0)
		gutter.Children(func() {
			if rail {
				ui.Box(c).Absolute().Left(10.75).Width(0.5).Top(22).Bottom(0).Background(p.Label4)
			}
			t.node(c, p, e)
		})
		ui.Column(c.Key("content")).Grow(1).Shrink(1).MinWidth(0).Padding(1, 0, 16, 0).Children(func() {
			t.content(c, p, e, last, live)
		})
	})
}

// content is what an entry says, beside its node.
func (t *agentTranscript) content(c *ui.Context, p *palette, e *model.AgentEntry, last, live bool) {
	switch {
	case e.Kind == "said":
		width := t.maxWidth
		if width <= 0 {
			width = 620
		}
		width -= 24
		fold, ok := t.folds[e.Seq]
		if !ok || fold.text != e.Text {
			fold = foldText(e.Text, width, 13, foldedHeight)
			t.folds[e.Seq] = fold
		}
		if fold.folded && !t.open[e.Seq] {
			// The head of what it said, fading into the transcript's fill, and Show more under it.
			ui.Column(c.Key("fold")).FillWidth().Children(func() {
				ui.Box(c).MaxHeight(foldedHeight).Clip().Children(func() {
					markdownView(c, fold.prefix, markdownOptions{Size: 13})
				})
				ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(26).Gradient(t.fill.Alpha(0), t.fill, 180)
			})
			ui.Row(c).FillWidth().Justify(ui.Start).Children(func() {
				more := ui.ButtonBase(c.Key(fmt.Sprintf("more:%d", e.Seq))).Padding(6, 0, 0, 0).FontSize(12).FontWeight(500).TextColor(p.Link).Cursor(ui.CursorPointer).Label(L("Show more")).FocusRing(false)
				more.Children(func() { ui.Text(c, L("Show more")) })
				if more.Clicked() {
					t.open[e.Seq] = true
				}
			})
		} else {
			ui.Box(c).FillWidth().Padding(1, 0, 0, 0).Children(func() { markdownView(c, e.Text, markdownOptions{Size: 13}) })
			if fold.folded {
				ui.Row(c).FillWidth().Justify(ui.Start).Children(func() {
					less := ui.ButtonBase(c.Key(fmt.Sprintf("less:%d", e.Seq))).Padding(6, 0, 0, 0).FontSize(12).FontWeight(500).TextColor(p.Label2).Cursor(ui.CursorPointer).Label(L("Show less")).FocusRing(false)
					less.Children(func() { ui.Text(c, L("Show less")) })
					if less.Clicked() {
						t.open[e.Seq] = false
					}
				})
			}
		}
	case e.Kind == "plan":
		done := 0
		for _, step := range e.Steps {
			if step.Done {
				done++
			}
		}
		ui.Column(c).FillWidth().Gap(6).Padding(2, 0, 0, 0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L("Plan")).FontSize(12.5).FontWeight(600)
				ui.Text(c, L("%@ of %@", fmt.Sprint(done), fmt.Sprint(len(e.Steps)))).FontSize(11.5).FontFeatures("tnum").TextColor(p.Label3)
			})
			ui.Column(c).FillWidth().Gap(5).Padding(9, 11).Radius(radiusMD).Background(p.Code).Children(func() {
				for i, step := range e.Steps {
					ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Start).Children(func() {
						mark, tint := "circle", p.Label3
						if step.Done {
							mark, tint = "checkmark.circle.fill", p.Green
						}
						symbol(c, mark, 13, 1.8).TextColor(tint).Margin(1, 0, 0, 0)
						words := ui.Text(c, step.Text).Grow(1).Shrink(1).MinWidth(0).FontSize(12.5).LineHeight(1.4).TextColor(p.Label)
						if step.Done {
							words.TextColor(p.Label2)
						}
					})
				}
			})
		})
	case e.Kind == "tool" && e.Tool != nil:
		t.tool(c, p, e, last, live)
	case e.Kind == "thought":
		first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(e.Text), "\n", 2)[0])
		t.folded(c, p, e, L("Thought"), first)
	case e.Kind == "compaction":
		title := L("Compacting the context…")
		switch e.Status {
		case "completed":
			title = L("Compacted the context")
		case "failed":
			title = L("Could not compact the context")
		case "cancelled":
			title = L("Stopped compacting the context")
		}
		t.folded(c, p, e, title, "")
	case e.Kind == "notice":
		words := p.Label2
		switch e.Level {
		case "warning":
			words = p.Label
		case "error":
			words = p.Red
		}
		ui.Text(c, e.Text).Margin(3, 0, 0, 0).FontSize(12.5).LineHeight(1.4).TextColor(words).Selectable()
	default:
		ui.Text(c, e.Text).Margin(3, 0, 0, 0).FontSize(12).LineHeight(1.4).TextColor(p.Label3).Selectable()
	}
}

// transcriptText draws the transcript as the bot reads it: what was sent in the accent, each tool's
// line in bold, and what came back dimmed.
func transcriptText(c *ui.Context, p *palette, text string) {
	for i, line := range strings.Split(text, "\n") {
		t := ui.Text(c.Key(i), line).Font(monoFont).FontSize(11.5).FixedLineHeight(17).Selectable()
		switch {
		case strings.HasPrefix(line, "> "):
			t.TextColor(p.Accent)
		case strings.HasPrefix(line, "● "):
			t.FontWeight(600)
		case strings.HasPrefix(line, "  ⎿"), strings.HasPrefix(line, "    "):
			t.TextColor(p.Label2)
		}
	}
}

// folded draws a line that opens to an entry's text: what the agent thought, or what a compaction
// kept. Closed, it shows the first of it beside its title.
func (t *agentTranscript) folded(c *ui.Context, p *palette, e *model.AgentEntry, title, preview string) {
	body := strings.TrimSpace(e.Text) != ""
	open := body && t.open[e.Seq]
	ui.Column(c.Key("folded")).FillWidth().Gap(4).Children(func() {
		head := ui.ButtonBase(c.Key("head")).FillWidth().Gap(7).AlignItems(ui.Center).Padding(2, 6).Margin(0, 0, 0, -6).Radius(6).TextColor(p.Label2)
		if body {
			head.Cursor(ui.CursorPointer).Label(title)
			if head.Hovered() {
				head.Background(p.Hover)
			}
		}
		if head.Clicked() && body {
			t.open[e.Seq] = !open
		}
		head.Children(func() {
			ui.Text(c, title).Shrink(0).FontSize(12.5).FontWeight(500).SingleLine()
			if !open && preview != "" {
				ui.Text(c, preview).Grow(1).Shrink(1).MinWidth(0).FontSize(12).TextColor(p.Label3).SingleLine().Tooltip(preview)
			} else {
				ui.Box(c).Grow(1)
			}
			if body {
				chevron := "chevron.right"
				if open {
					chevron = "chevron.down"
				}
				symbol(c, chevron, 10, 2).TextColor(p.Label3)
			}
		})
		if open {
			ui.Box(c).FillWidth().Padding(4, 12).Radius(radiusMD).Background(p.Code).TextColor(p.Label2).Children(func() { markdownView(c, e.Text, markdownOptions{Size: 12.5}) })
		}
	})
}

// tool draws a tool's line, which opens to what it did.
func (t *agentTranscript) tool(c *ui.Context, p *palette, e *model.AgentEntry, last, live bool) {
	tool := e.Tool
	body := hasBody(tool)
	open := body && t.isOpen(e, last)
	ui.Column(c.Key("tool")).FillWidth().Gap(6).Children(func() {
		head := ui.ButtonBase(c.Key("head")).FillWidth().Gap(7).AlignItems(ui.Center).Padding(2, 6).Margin(0, 0, 0, -6).Radius(6).TextColor(p.Label)
		if body {
			head.Cursor(ui.CursorPointer).Label(tool.Title)
			if head.Hovered() {
				head.Background(p.Hover)
			}
		}
		if head.Clicked() && body {
			t.open[e.Seq] = !open
		}
		head.Children(func() {
			ui.Text(c, tool.Title).Shrink(1).MinWidth(0).FontSize(12.5).FontWeight(500).SingleLine().Tooltip(tool.Title)
			if tool.Detail != "" {
				ui.Text(c, tool.Detail).Shrink(2).MinWidth(0).Font(monoFont).FontSize(11.5).TextColor(p.Label2).SingleLine().Tooltip(tool.Detail)
			}
			ui.Box(c).Grow(1)
			added, removed := 0, 0
			for _, diff := range tool.Diffs {
				added, removed = added+diff.Added, removed+diff.Removed
			}
			if added+removed > 0 {
				ui.Text(c, fmt.Sprintf("+%d", added)).Font(monoFont).FontSize(11).TextColor(p.Green)
				ui.Text(c, fmt.Sprintf("−%d", removed)).Font(monoFont).FontSize(11).TextColor(p.Red)
			}
			if tool.ExitCode != nil && *tool.ExitCode != 0 {
				ui.Text(c, L("exit %@", fmt.Sprint(*tool.ExitCode))).Font(monoFont).FontSize(11).TextColor(p.Red)
			}
			if body {
				mark := "chevron.right"
				if open {
					mark = "chevron.down"
				}
				symbol(c, mark, 10, 2).TextColor(p.Label3)
			}
		})
		if !open {
			return
		}
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			if tool.Output != "" {
				t.output(c, p, e.Seq, tool.Output, live)
			}
			for _, diff := range tool.Diffs {
				diffView(c, p, diff)
			}
			if tool.Output == "" && len(tool.Diffs) == 0 && tool.Result != "" {
				commandOutput(c, tool.Result, 10).Margin(0, 0, 0, 0)
			}
		})
	})
}

// output draws a command's output on a terminal of its own, as tall as its lines up to
// `outputRows`, or as text where it has none.
func (t *agentTranscript) output(c *ui.Context, p *palette, seq uint64, output string, live bool) {
	plain := outputPlain(output)
	rows := min(max(strings.Count(plain, "\n")+1, 1), outputRows)
	screen := t.screens[seq]
	if live && screen == nil {
		if screen = newAgentScreen(11.5); screen != nil {
			t.screens[seq] = screen
		}
	}
	if !live || screen == nil {
		commandOutput(c, plain, outputRows).Margin(0, 0, 0, 0)
		return
	}
	screen.show(output)
	ui.Box(c.Key("terminal")).FillWidth().Height(float32(rows)*outputRow+14).Padding(7, 10).Radius(radiusMD).Background(p.Code).Clip().Children(func() {
		terminal.View(c, screen.term).Fill()
	})
}

// diffView draws an edit: its file, then its lines, added ones green and removed ones red.
func diffView(c *ui.Context, p *palette, diff model.AgentFileDiff) {
	ui.Column(c).FillWidth().Radius(6).Background(p.Code).Clip().Children(func() {
		ui.Row(c).FillWidth().Gap(6).Padding(5, 9).AlignItems(ui.Center).Children(func() {
			ui.Text(c, diff.Path).Grow(1).Shrink(1).MinWidth(0).Font(monoFont).FontSize(11).TextColor(p.Label2).SingleLine().Tooltip(diff.Path)
		})
		ui.Column(c).FillWidth().Padding(0, 0, 5, 0).Children(func() {
			for _, line := range diff.Lines {
				row := ui.Box(c).FillWidth().Padding(0, 9)
				tint := p.Label2
				switch {
				case strings.HasPrefix(line, "@@"):
					tint = p.Label3
				case strings.HasPrefix(line, "+"):
					tint = p.Green
					row.Background(p.Green.Alpha(0.1))
				case strings.HasPrefix(line, "-"):
					tint = p.Red
					row.Background(p.Red.Alpha(0.1))
				}
				row.Children(func() {
					ui.Text(c, line).Font(monoFont).FontSize(11).FixedLineHeight(15).TextColor(tint).SingleLine().Selectable()
				})
			}
		})
	})
}

// toolSymbol is the mark of a tool that ended well, by its kind.
func toolSymbol(kind string) string {
	switch kind {
	case "execute":
		return "terminal"
	case "edit", "delete", "move":
		return "pencil"
	case "read":
		return "doc.text"
	case "search":
		return "magnifyingglass"
	case "fetch":
		return "globe"
	case "think":
		return "sparkles"
	}
	return "checkmark"
}
