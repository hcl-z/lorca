package main

import (
	"fmt"
	"math"
	"slices"
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
	// draft is what the user writes the agent at the foot of its page; sending keeps one in
	// flight, and sendErr says why the last one did not go.
	draft   string
	sending bool
	sendErr string
	// images are what goes with the draft, for an agent that takes them.
	images []model.OutgoingAttachment
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
		t.promptCard(c, author, e.Text, e.Attachments, e.From == "user", open, func() { toggle(open) })
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

// promptCard is what the agent was sent. What the user wrote on its page is a bubble at the right,
// as their messages are in the chat, its images above its words; what the bot sent is a card the
// width of the steps, under the bot's name. A long one folds past two lines, with Show more.
func (t *agentTranscript) promptCard(c *ui.Context, author, text string, images []model.AgentAttachment, mine, open bool, toggle func()) {
	p := colors(c)
	long := strings.Count(text, "\n") >= 2 || len([]rune(text)) > 200
	thumbs := func() {
		if len(images) == 0 {
			return
		}
		row := ui.Row(c.Key("sent-images")).Wrap().Gap(6)
		if mine {
			row.Justify(ui.End)
		}
		row.Children(func() {
			for _, image := range images {
				attachment := model.Attachment{ID: image.ID, Name: image.Name, Mime: image.Mime, Size: image.Size, Width: image.Width, Height: image.Height}
				ui.Box(c.Key(image.ID)).Size(96, 96).Radius(12).Clip().Background(p.ComposerControl).Border(0.5, p.CardBorder).Tooltip(image.Name).Children(func() {
					if bitmap := loadBitmap(store.LocalFile(attachment, t.chatID, t.messageID)); bitmap != nil {
						ui.Image(c, bitmap).Size(96, 96).Fit(ui.Cover)
					} else {
						ui.Row(c).Size(96, 96).Center().TextColor(p.Label3).Children(func() { symbol(c, "photo", 18, 2) })
					}
				})
			}
		})
	}
	more := func() {
		if !long {
			return
		}
		label := L("Show more")
		if open {
			label = L("Show less")
		}
		b := ui.ButtonBase(c.Key("more")).Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).FocusRing(false).Label(label).FontSize(11.5).TextColor(p.Label3)
		if b.Hovered() {
			b.TextColor(p.Label)
		}
		b.Children(func() {
			ui.Text(c, label)
			name := "chevron.down"
			if open {
				name = "chevron.up"
			}
			symbol(c, name, 9, 2)
		})
		if b.Clicked() {
			toggle()
		}
	}
	if mine {
		ui.Column(c).FillWidth().AlignItems(ui.End).Gap(6).Children(func() {
			thumbs()
			if text != "" {
				ui.Column(c).MaxWidthPercent(80).Gap(4).Padding(8, 13).Radius(radiusXL).Background(p.Blue).TextColor(p.AccentText).SelectionColor(p.AccentText.Alpha(0.28)).Children(func() {
					body := ui.Text(c, text).FontSize(13).LineHeight(1.45).Selectable()
					if long && !open {
						body.MaxLines(3)
					}
				})
			}
			more()
		})
		return
	}
	ui.Column(c).FillWidth().Gap(5).Padding(9, 12, 10, 12).Radius(radiusMD).Background(p.BotBubble).Border(1, p.BotBubbleBorder).Children(func() {
		ui.Text(c, author).FontSize(textCaption).FontWeight(600).TextColor(p.Label3)
		if text != "" {
			body := ui.Text(c, text).FontSize(12.5).LineHeight(1.4).Selectable()
			if long && !open {
				body.MaxLines(2)
			}
		}
		thumbs()
		more()
	})
}

// writable is whether a message can reach the agent: it is open and past its start, or it ended
// with a session to start again on. One Auto-review held, or nobody answered, never ran.
func writable(agent *model.AgentRun) bool {
	switch agent.State {
	case model.AgentAsking, model.AgentWorking, model.AgentIdle, model.AgentExited, model.AgentFailed, model.AgentStopped:
		return true
	}
	return false
}

// composer is the foot of the agent's page: the user writes the agent there, and only there. It
// takes the message up once it is done with what it does now, or starts again on its session;
// neither the chat nor the bot sees it. An agent that takes images takes them too (any but one that
// said it takes none, `NoImages`):
// pasted, dropped on the field, or chosen with the photo button, shown above the field until they
// go. A message that did not go stays in the field, with why.
func (t *agentTranscript) composer(c *ui.Context, w *appWindow, agent *model.AgentRun) {
	p := colors(c)
	open := writable(agent)
	images := !agent.NoImages
	send := func() {
		text := strings.TrimSpace(t.draft)
		attached := slices.Clone(t.images)
		if (text == "" && len(attached) == 0) || t.sending || !open {
			return
		}
		t.sending, t.sendErr = true, ""
		store.SendToAgent(t.chatID, t.messageID, text, attached, func(err error) {
			post(func() {
				t.sending = false
				if err != nil {
					t.sendErr = model.ErrorText(err)
					return
				}
				// What went leaves the field; what was typed or added meanwhile stays.
				if strings.TrimSpace(t.draft) == text {
					t.draft = ""
				}
				t.images = slices.DeleteFunc(t.images, func(item model.OutgoingAttachment) bool {
					return slices.ContainsFunc(attached, func(sent model.OutgoingAttachment) bool { return sent.Attachment.ID == item.Attachment.ID })
				})
				t.list.FollowEnd = true
				t.load()
			})
		})
	}
	// Only images, and only for an agent that takes them; the rest get one line of why.
	add := func(files []fileInfo) {
		var refused []string
		for _, info := range files {
			switch {
			case !info.IsFile:
				refused = append(refused, L("%@ is not a file.", info.Name))
			case !strings.HasPrefix(info.Mime, "image/"):
				refused = append(refused, L("%@ is not an image.", info.Name))
			case info.Size > maxAttachmentBytes:
				refused = append(refused, L("%@ is larger than %d MB.", info.Name, maxAttachmentBytes/1024/1024))
			case len(t.images) >= maxAttachments:
				refused = append(refused, L("At most %d files per message.", maxAttachments))
			default:
				t.images = append(t.images, composerOutgoing(info))
			}
		}
		t.sendErr = strings.Join(slices.Compact(refused), " ")
	}
	paste := func(text string) bool {
		if !images || !open {
			return false
		}
		if paths, err := clipboardFiles(); err == nil && len(paths) > 0 {
			var files []fileInfo
			for _, path := range paths {
				files = append(files, inspect(path))
			}
			add(files)
			return true
		}
		if text != "" {
			return false
		}
		data := clipboardImage()
		if len(data) == 0 {
			return false
		}
		if info, err := savePasted("image.png", data); err == nil {
			add([]fileInfo{info})
		}
		return true
	}
	placeholder := L("Message %@", agent.Name())
	ui.Column(c.Key("agent-composer")).FillWidth().Gap(5).Margin(10, 0, 0, 0).Children(func() {
		if len(t.images) > 0 {
			removed := -1
			ui.Row(c.Key("agent-images")).Wrap().GapX(14).GapY(8).Padding(6, 4, 2, 4).Children(func() {
				for i, item := range t.images {
					ui.Box(c.Key(item.Attachment.ID)).Tooltip(item.Attachment.Name).Children(func() {
						ui.Box(c).Size(56, 56).Radius(10).Clip().Background(p.ComposerControl).Children(func() {
							if bitmap := loadBitmap(item.Path); bitmap != nil {
								ui.Image(c, bitmap).Size(56, 56).Fit(ui.Cover)
							}
						})
						remove := ui.ButtonBase(c).Absolute().Top(-6).Right(-6).Size(18, 18).Radius(9).Center().Background(ui.RGBA(0, 0, 0, 0.7)).TextColor(p.White).
							Label(L("Remove")).Tooltip(L("Remove"))
						remove.Children(func() { symbol(c, "xmark", 9, 3.2) })
						if remove.Clicked() {
							removed = i
						}
					})
				}
			})
			if removed >= 0 {
				t.images = slices.Delete(slices.Clone(t.images), removed, removed+1)
			}
		}
		// One pill, as the chat's composer is: the photo button, the field, and a round Send.
		pill := ui.Row(c.Key("agent-pill")).FillWidth().Gap(8).AlignItems(ui.End).Padding(6).MinHeight(composerControlSize+12).
			Radius(radiusXL+6).Background(p.ComposerField).Border(1, p.ComposerBorder).Cursor(ui.CursorText)
		if !c.Theme().Dark {
			pill.Shadow(0, 2, 10, 0, ui.RGBA(0, 0, 0, 0.05))
		}
		if pill.FocusWithin() {
			pill.BorderColor(p.Label3)
		}
		if images && open {
			if dropped := pill.DroppedFiles(); dropped != nil {
				var files []fileInfo
				for _, path := range dropped {
					files = append(files, inspect(path))
				}
				add(files)
			}
			if pill.FileDragOver() {
				pill.Border(2, p.Accent)
			}
		}
		pill.Children(func() {
			if images {
				attach := ui.ButtonBase(c.Key("agent-attach")).Size(composerControlSize, composerControlSize).Radius(composerControlSize / 2).Center().
					Background(p.ComposerControl).TextColor(p.Label2).Label(L("Attach images")).Tooltip(L("Attach images")).Disabled(!open || t.sending)
				if attach.Hovered() {
					attach.Background(p.ComposerControl.Mix(p.Label, 0.06)).TextColor(p.Label)
				}
				attach.Children(func() { symbol(c, "photo", 15, 2) })
				if attach.Clicked() && w != nil {
					chooseFiles(w.win, "", L("Attach images to your message"), L("Attach"), true, true, add)
				}
			}
			field := ui.TextAreaBase(c, &t.draft).Lines(1, 6).Grow(1).Shrink(1).MinWidth(0).Padding(6, 4).FontSize(13.5).LineHeight(1.4).
				TextColor(p.Label).Placeholder(placeholder).Label(placeholder).Disabled(!open || t.sending).FocusRing(false)
			// A paste of a picture or of copied image files attaches them; text pastes as text.
			field.OnPaste(paste)
			// Return sends; Shift-Return is a new line, and Return that ends a composition (as a
			// Chinese input method's) only ends it.
			composing := field.Composing()
			field.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputKeyDown && !composing && ev.Key == ui.KeyEnter && ev.Mods == 0 {
					send()
					return true
				}
				return false
			})
			ready := (strings.TrimSpace(t.draft) != "" || len(t.images) > 0) && open && !t.sending
			button := ui.ButtonBase(c.Key("agent-send")).Size(composerControlSize, composerControlSize).Radius(composerControlSize / 2).Center().
				Background(p.ComposerPrimary).TextColor(p.ComposerPrimaryContent).Label(L("Send")).Tooltip(L("Send (Return) · Shift-Return for a new line"))
			if !ready {
				button.Opacity(0.35)
			} else if button.Hovered() {
				button.Background(p.inkHover())
			}
			button.Children(func() {
				if t.sending {
					spinner(c, 14)
				} else {
					symbol(c, "arrow.up", 16, 2.6)
				}
			})
			if button.Clicked() && ready {
				send()
			}
		})
		hint := ""
		switch {
		case t.sendErr != "":
			hint = t.sendErr
		case !open:
			hint = L("%@ cannot take a message.", agent.Name())
		case agent.State == model.AgentWorking || agent.State == model.AgentAsking:
			hint = L("%@ takes it up once it is done with what it does now.", agent.Name())
		case agent.State == model.AgentExited || agent.State == model.AgentFailed || agent.State == model.AgentStopped:
			hint = L("Sending starts %@ again on its session.", agent.Name())
		}
		if hint != "" {
			text := ui.Text(c, hint).FontSize(11).LineHeight(1.35).TextColor(p.Label3).Padding(0, 10)
			if t.sendErr != "" {
				text.TextColor(p.Red)
			}
		}
	})
}

// agent is the agent as its row in the chat has it, else as the transcript last answered it,
// else the card the caller showed.
func (t *agentTranscript) agent(fallback *model.AgentRun) *model.AgentRun {
	// The row in the chat follows the Runner's events, so its card is the freshest: the same card
	// the chat shows, never one the page took as it opened.
	if chat := store.Chat(t.chatID); chat != nil {
		if live := model.AgentOf(chat.Message(t.messageID)); live != nil {
			return live
		}
	}
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
				// Where it runs: the folder (its worktree in a repository) on its Runner.
				if agent.Folder != "" {
					ui.Row(c).Gap(5).Margin(2, 0, 0, 0).AlignItems(ui.Center).MinWidth(0).TextColor(p.Label3).Tooltip(agent.Folder).Children(func() {
						symbol(c, "folder", 11, 2)
						ui.Text(c, agent.Folder).Font(monoFont).FontSize(11.5).SingleLine().Shrink(1).MinWidth(0).Selectable()
					})
				}
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

// subtitle is "Claude Code · Working · shop", and the Runner's name when it is another Device.
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
