package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// A coding agent's transcript as its sheet and its full view draw it, from the entries its Runner
// keeps: its steps in order, as Cursor shows an agent's run. What it was sent is a card that says
// who wrote it; a thought is one line, "Thought for 3s", that opens to what it thought; a tool is
// one line in the past or present tense, and a run of reads and searches folds into "Explored 3
// files, 2 searches"; what it said is Markdown, a long one folded to its head with Show more under
// it; its plan is a checklist; its notices, refusals, and compactions are lines of their own. The
// view opens on the newest page of entries, asks the Runner again every two seconds, five through
// the relay, for what changed, asks for the page before the oldest when the top comes into view,
// and keeps to the end unless the user scrolled up to read.

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
	// card is the agent as the transcript last answered it, for the header and the footer.
	card *model.AgentRun
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
		t.kind, t.card = agent.Kind, agent
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
			if page.Card != nil {
				t.card = page.Card
			}
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

// view draws the steps as a list, which builds only the rows in view and keeps the reader's
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
	lastTool := uint64(0)
	found := false
	for i := range t.entries {
		if tool := t.entries[i].Tool; tool != nil && hasBody(tool) {
			lastTool, found = t.entries[i].Seq, true
		}
	}
	live := map[uint64]bool{}
	if found {
		for i := len(t.entries) - 1; i >= 0 && len(live) < entryScreens; i-- {
			e := &t.entries[i]
			if e.Tool != nil && e.Tool.Output != "" && t.isOpen(e, e.Seq == lastTool) {
				live[e.Seq] = true
			}
		}
	}
	for seq, s := range t.screens {
		if !live[seq] {
			s.close()
			delete(t.screens, seq)
		}
	}
	steps := agentSteps(t.entries)
	t.list.Key = func(i int) any { return steps[i].entry.Seq }
	list := ui.List(c, &t.list, len(steps), func(i int) {
		step := steps[i]
		// Tool lines sit close together; the rest a paragraph apart.
		top := float32(12)
		switch {
		case i == 0:
			top = 0
		case step.entry.Kind == "tool" && steps[i-1].entry.Kind == "tool":
			top = 2
		}
		row := ui.Row(c.Key(fmt.Sprintf("step:%d", step.entry.Seq))).FillWidth().Justify(ui.Center).Margin(top, 0, 0, 0)
		row.Children(func() {
			column := ui.Column(c).Grow(1).Shrink(1).MinWidth(0)
			if t.maxWidth > 0 {
				column.MaxWidth(t.maxWidth)
			}
			column.Children(func() { t.step(c, step, found && step.entry.Seq == lastTool, live) })
		})
	}).Fill().Padding(4, 2, 12, 2).TextColor(p.Label)
	// Nearing the first step: the page before it.
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

// agentStep is a line of the page: one entry, or a run of reads and searches folded into one.
type agentStep struct {
	entry    *model.AgentEntry
	explored []*model.AgentEntry
}

func agentSteps(entries []model.AgentEntry) []agentStep {
	var steps []agentStep
	for i := range entries {
		e := &entries[i]
		if e.Kind == "tool" && e.Tool != nil && (e.Tool.Kind == "read" || e.Tool.Kind == "search") {
			if n := len(steps); n > 0 && steps[n-1].explored != nil {
				steps[n-1].explored = append(steps[n-1].explored, e)
				continue
			}
			steps = append(steps, agentStep{entry: e, explored: []*model.AgentEntry{e}})
			continue
		}
		steps = append(steps, agentStep{entry: e})
	}
	return steps
}

// exploredText is a fold's line: "Explored 3 files, 2 searches", "Exploring 1 file" while one runs.
func exploredText(items []*model.AgentEntry) string {
	files, searches, running := 0, 0, false
	for _, item := range items {
		if item.Tool.Kind == "read" {
			files++
		} else {
			searches++
		}
		running = running || item.Tool.Running()
	}
	var parts []string
	switch {
	case files == 1:
		parts = append(parts, L("1 file"))
	case files > 1:
		parts = append(parts, L("%d files", files))
	}
	switch {
	case searches == 1:
		parts = append(parts, L("1 search"))
	case searches > 1:
		parts = append(parts, L("%d searches", searches))
	}
	switch {
	case len(parts) == 2 && running:
		return L("Exploring %@ %@", parts[0], parts[1])
	case len(parts) == 2:
		return L("Explored %@ %@", parts[0], parts[1])
	case running:
		return L("Exploring %@", parts[0])
	}
	return L("Explored %@", parts[0])
}

// toolSubject is what a tool acts on: its detail when the agent says, else its title with a
// leading verb dropped, so "Edit src/a.rs" does not read "Edited Edit src/a.rs".
func toolSubject(tool *model.AgentTool) string {
	subject := tool.Detail
	if subject == "" {
		subject = tool.Title
		for _, verb := range []string{"Read ", "Reading ", "Searched ", "Searching ", "Edit ", "Edited ", "Editing ", "Run ", "Ran ", "Running ", "Fetch ", "Fetched ", "Fetching ", "Used ", "Using "} {
			if strings.HasPrefix(subject, verb) {
				subject = strings.TrimSpace(strings.TrimPrefix(subject, verb))
				break
			}
		}
	}
	return subject
}

// agentToolText is a step's line, in the past once it is done and in the present while it runs:
// "Ran bun test", "Editing src/a.rs", "Not allowed: git push".
func agentToolText(tool *model.AgentTool) string {
	running := tool.Running()
	subject := toolSubject(tool)
	if tool.Failed() && !running {
		return L("Failed: %@", subject)
	}
	switch {
	case tool.Kind == "read" && running:
		return L("Reading %@", subject)
	case tool.Kind == "read":
		return L("Read %@", subject)
	case tool.Kind == "search" && running:
		return L("Searching %@", subject)
	case tool.Kind == "search":
		return L("Searched %@", subject)
	case tool.Kind == "edit" && running:
		return L("Editing %@", subject)
	case tool.Kind == "edit":
		return L("Edited %@", subject)
	case tool.Kind == "execute" && running:
		return L("Running %@", subject)
	case tool.Kind == "execute":
		return L("Ran %@", subject)
	case tool.Kind == "fetch" && running:
		return L("Fetching %@", subject)
	case tool.Kind == "fetch":
		return L("Fetched %@", subject)
	case running:
		return L("Using %@", subject)
	}
	return L("Used %@", subject)
}

// toolSpans are a step's line with its subject in the text color, a command in monospace, and an
// edit's counts in green and red; a failed step reads in red.
func toolSpans(p *palette, tool *model.AgentTool) []ui.Span {
	text := agentToolText(tool)
	subject := toolSubject(tool)
	quiet, ink := p.Label2, p.Label
	if tool.Failed() {
		quiet, ink = p.Red, p.Red
	}
	font := ""
	if tool.Kind == "execute" {
		font = monoFont
	}
	var spans []ui.Span
	if i := strings.Index(text, subject); subject != "" && i >= 0 {
		spans = append(spans, ui.Span{Text: text[:i], Color: quiet}, ui.Span{Text: subject, Color: ink, Font: font}, ui.Span{Text: text[i+len(subject):], Color: quiet})
	} else {
		spans = append(spans, ui.Span{Text: text, Color: quiet})
	}
	if tool.Kind == "edit" && !tool.Failed() {
		added, removed := 0, 0
		for _, diff := range tool.Diffs {
			added, removed = added+diff.Added, removed+diff.Removed
		}
		if added > 0 {
			spans = append(spans, ui.Span{Text: "  +" + strconv.Itoa(added), Color: p.Green, Weight: 500})
		}
		if removed > 0 {
			spans = append(spans, ui.Span{Text: " −" + strconv.Itoa(removed), Color: p.Red, Weight: 500})
		}
	}
	return spans
}

// stepLine is a step's one line: a spinner while it runs, its words, and when it holds more, a
// chevron that shows it. It answers a click that opens or closes it.
func stepLine(c *ui.Context, spans []ui.Span, label string, running, expandable, open bool, size float32) bool {
	p := colors(c)
	var line ui.Element
	clicked := false
	if expandable {
		line = ui.ButtonBase(c).Cursor(ui.CursorPointer).FocusRing(false)
		if line.Hovered() {
			line.Background(p.RowHover)
		}
		clicked = line.Clicked()
	} else {
		line = ui.Row(c)
	}
	line.Gap(6).MinHeight(22).Padding(2, 6).Margin(0, -6).Radius(6).MinWidth(0).MaxWidthPercent(100).AlignSelf(ui.Start).Label(label).Tooltip(label)
	line.Children(func() {
		if running {
			spinner(c, 11)
		}
		ui.RichText(c, spans...).FontSize(size).SingleLine().Shrink(1).MinWidth(0)
		if expandable {
			name := "chevron.right"
			if open {
				name = "chevron.down"
			}
			ui.Row(c).Shrink(0).TextColor(p.Label3).Children(func() { symbol(c, name, 11, 2.2) })
		}
	})
	return clicked
}

// transcript is the page's body: each step in order, or what stands in for them.
func (t *agentTranscript) transcript(c *ui.Context, lastTool uint64, found bool, live map[uint64]bool) {
	p := colors(c)
	switch {
	case len(t.entries) == 0 && t.text != "":
		transcriptText(c, p, t.text)
		return
	case len(t.entries) == 0 && t.err != "":
		ui.Text(c, t.err).FontSize(12).TextColor(p.Label2)
		return
	case len(t.entries) == 0 && t.loaded:
		ui.Text(c, L("Nothing yet")).FontSize(12).TextColor(p.Label2)
		return
	case len(t.entries) == 0:
		ui.Text(c, L("Loading…")).FontSize(12).TextColor(p.Label2)
		return
	}
	steps := agentSteps(t.entries)
	ui.Column(c).Children(func() {
		for i, step := range steps {
			// Tool lines sit close together; the rest a paragraph apart.
			top := float32(12)
			switch {
			case i == 0:
				top = 0
			case step.entry.Kind == "tool" && steps[i-1].entry.Kind == "tool":
				top = 2
			}
			ui.Column(c.Key(fmt.Sprintf("step:%d", step.entry.Seq))).Margin(top, 0, 0, 0).MinWidth(0).Children(func() {
				t.step(c, step, found && step.entry.Seq == lastTool, live)
			})
		}
	})
}

func (t *agentTranscript) step(c *ui.Context, step agentStep, last bool, live map[uint64]bool) {
	p := colors(c)
	e := step.entry
	toggle := func(open bool) { t.open[e.Seq] = !open }
	switch {
	case step.explored != nil:
		open := t.open[e.Seq]
		text := exploredText(step.explored)
		running := false
		for _, each := range step.explored {
			running = running || each.Tool.Running()
		}
		if stepLine(c, []ui.Span{{Text: text, Color: p.Label2}}, text, running, true, open, 12.5) {
			toggle(open)
		}
		if open {
			ui.Column(c).Padding(2, 0, 2, 14).Children(func() {
				for _, each := range step.explored {
					ui.Box(c.Key(fmt.Sprint(each.Seq))).Children(func() {
						stepLine(c, toolSpans(p, each.Tool), agentToolText(each.Tool), each.Tool.Running(), false, false, 12)
					})
				}
			})
		}
	case e.Kind == "tool" && e.Tool != nil:
		tool := e.Tool
		expandable := hasBody(tool)
		open := t.isOpen(e, last)
		if stepLine(c, toolSpans(p, tool), agentToolText(tool), tool.Running(), expandable, open, 12.5) {
			toggle(open)
		}
		if expandable && open {
			t.toolBody(c, p, e, live[e.Seq])
		}
	case e.Kind == "sent":
		open := t.open[e.Seq]
		author := t.bot
		if e.From == "user" {
			author = L("You")
		}
		t.promptCard(c, author, e.Text, e.From == "user", open, func() { toggle(open) })
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
	case e.Kind == "thought":
		open := t.open[e.Seq]
		text := L("Thought")
		if e.Seconds != nil {
			text = L("Thought for %ds", max(1, int(math.Round(*e.Seconds))))
		}
		expandable := strings.TrimSpace(e.Text) != ""
		if stepLine(c, []ui.Span{{Text: text, Color: p.Label2}}, text, false, expandable, open, 12.5) {
			toggle(open)
		}
		if expandable && open {
			ui.Text(c, e.Text).Padding(2, 0, 0, 14).FontSize(12).LineHeight(1.45).TextColor(p.Label2).Selectable()
		}
	case e.Kind == "plan":
		done := 0
		for _, step := range e.Steps {
			if step.Done {
				done++
			}
		}
		ui.Column(c).FillWidth().Gap(6).Children(func() {
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
	case e.Kind == "compaction":
		open := t.open[e.Seq]
		title := L("Compacting the context…")
		switch e.Status {
		case "completed":
			title = L("Compacted the context")
		case "failed":
			title = L("Could not compact the context")
		case "cancelled":
			title = L("Stopped compacting the context")
		}
		body := strings.TrimSpace(e.Text) != ""
		if stepLine(c, []ui.Span{{Text: title, Color: p.Label2}}, title, e.Status == "" || e.Status == "in_progress", body, open, 12.5) {
			toggle(open)
		}
		if body && open {
			ui.Text(c, e.Text).Padding(2, 0, 0, 14).FontSize(12).LineHeight(1.45).TextColor(p.Label2).Selectable()
		}
	case e.Kind == "notice":
		tint := p.Label2
		switch e.Level {
		case "warning":
			tint = p.Orange
		case "error":
			tint = p.Red
		}
		ui.Text(c, e.Text).FontSize(11.5).LineHeight(1.4).TextColor(tint).Selectable()
	case e.Kind == "note":
		tint := p.Label3
		if strings.HasPrefix(strings.ToLower(e.Text), "not allowed") {
			tint = p.Red
		}
		ui.Text(c, e.Text).FontSize(12).LineHeight(1.4).TextColor(tint).Selectable()
	default:
		ui.Text(c, e.Text).FontSize(12).LineHeight(1.4).TextColor(p.Label3).Selectable()
	}
}

// toolBody is what a tool did when its line is open: its command's output, its edits, or what it
// gave back.
func (t *agentTranscript) toolBody(c *ui.Context, p *palette, e *model.AgentEntry, live bool) {
	tool := e.Tool
	ui.Column(c).FillWidth().Gap(6).Padding(2, 0, 2, 14).Children(func() {
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
}

// promptCard is what the agent was sent, under who wrote it: "You" in the accent's tint for what
// the user wrote, else the bot that passed it on. A long one, as the first brief is, folds past
// two lines, with a chevron on its author line to open or close it.
func (t *agentTranscript) promptCard(c *ui.Context, author, text string, mine, open bool, toggle func()) {
	p := colors(c)
	long := strings.Count(text, "\n") >= 2 || len([]rune(text)) > 200
	card := ui.Column(c).Gap(4).Padding(9, 12, 10, 12).Radius(radiusMD).Background(p.BotBubble).Border(1, p.BotBubbleBorder)
	if mine {
		card.Background(p.Accent.Alpha(0.09)).Border(1, p.Accent.Alpha(0.22))
	}
	card.Children(func() {
		var head ui.Element
		if long {
			label := L("Show more")
			if open {
				label = L("Show less")
			}
			head = ui.ButtonBase(c.Key("head")).FillWidth().Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).FocusRing(false).Label(label)
			if head.Hovered() {
				head.TextColor(p.Label)
			}
			if head.Clicked() {
				toggle()
			}
		} else {
			head = ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center)
		}
		head.Children(func() {
			ui.Text(c, author).Shrink(0).FontSize(textCaption).FontWeight(600).TextColor(p.Label3)
			ui.Box(c).Grow(1)
			if long {
				name := "chevron.down"
				if open {
					name = "chevron.up"
				}
				symbol(c, name, 10, 2).TextColor(p.Label3)
			}
		})
		body := ui.Text(c, text).FontSize(12.5).LineHeight(1.4).Selectable()
		if long && !open {
			body.MaxLines(2)
		}
	})
}

// agent is the agent as the transcript last answered it, else the card the caller showed.
func (t *agentTranscript) agent(fallback *model.AgentRun) *model.AgentRun {
	if t.card != nil {
		return t.card
	}
	return fallback
}

// header is the page's top: the agent's mark, what it was asked, who it is and how it stands,
// its pull request, Stop, and its session: its model, effort, mode, context, and cost.
func (t *agentTranscript) header(c *ui.Context, w *appWindow, agent *model.AgentRun, st *agentCardState, run agentWork) {
	p := colors(c)
	ui.Column(c).Gap(10).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Row(c).Size(22, 22).Center().TextColor(p.Label).Margin(1, 0, 0, 0).Children(func() { agentTile(c, p, agent.Kind, 20) })
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				title := firstNonEmpty(agent.Title, agent.Task)
				ui.Text(c, title).FontSize(15).FontWeight(600).LineHeight(1.3).MaxLines(2).Tooltip(title)
				ui.Text(c, t.subtitle(agent)).FontSize(12).TextColor(p.Label2).SingleLine()
			})
			ui.Row(c).Gap(8).Shrink(0).Children(func() {
				if agent.PullRequest != "" {
					pr := agent.PullRequestName
					if pr == "" {
						pr = L("Pull request")
					}
					b := pushButton(c.Key("pull-request"), pr, pushOptions{Small: true}).Tooltip(agent.PullRequest)
					if b.Clicked() {
						_ = openExternal(agent.PullRequest)
					}
				}
				if agent.IsRunning() && pushButton(c.Key("stop"), L("Stop"), pushOptions{Small: true}).Clicked() {
					store.StopAgent(t.chatID, t.messageID, func(err error) {
						if err != nil {
							post(func() {
								w.showAlert(alertOptions{Message: L("Could not stop %@", agent.Name()), Informative: model.ErrorText(err)}, nil)
							})
						}
					})
				}
			})
		})
		ui.Box(c).Height(1).Background(p.Separator)
		agentFacts(c, p, agent, t.chatID, t.messageID, st, run, 120)
	})
}

// subtitle is "Claude Code · Working · fix-login", and the Runner's name when it is another Device.
func (t *agentTranscript) subtitle(agent *model.AgentRun) string {
	parts := []string{agent.Name(), agent.Status()}
	if place := agent.Place(); place != "" {
		parts = append(parts, place)
	}
	if t.remote && agent.Device != "" {
		parts = append(parts, agent.Device)
	}
	return strings.Join(parts, " · ")
}

// agentFooter is what the agent's work changed, "+12 −3 in 4 files", and its branch.
func agentFooter(c *ui.Context, agent *model.AgentRun) {
	p := colors(c)
	if changes := agent.Changes; changes != nil && changes.Files > 0 {
		files := L("in %d files", changes.Files)
		if changes.Files == 1 {
			files = L("in 1 file")
		}
		ui.RichText(c,
			ui.Span{Text: "+" + strconv.Itoa(changes.Added), Color: p.Green, Weight: 500},
			ui.Span{Text: " −" + strconv.Itoa(changes.Removed), Color: p.Red, Weight: 500},
			ui.Span{Text: " " + files, Color: p.Label2},
		).FontSize(12).SingleLine().Shrink(0)
	}
	if agent.Branch != "" {
		ui.Row(c).Gap(4).Shrink(1).MinWidth(0).TextColor(p.Label2).Tooltip(agent.Folder).Children(func() {
			symbol(c, "arrow.triangle.branch", 12, 2)
			ui.Text(c, agent.Branch).Font(monoFont).FontSize(11.5).SingleLine().Shrink(1).MinWidth(0)
		})
	}
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
