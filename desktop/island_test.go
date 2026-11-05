package main

import (
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// The island's tests: Cindy's rules on a machine fed by hand, the numbers its layouts compute, and
// the views drawn for a look. LORCA_RENDER=dir writes the frames.

var islandT0 = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return islandT0.Add(d) }

func runningObs(key string) islandObservation {
	return islandObservation{Key: key, ChatID: "chat-" + key, BotID: "bot-" + key, Title: key, Running: true, Prompt: "Fix the tests", PromptID: "p-" + key}
}

// TestIslandTurnLifecycle follows one session through Cindy's transitions: it starts, runs a tool
// whose line lingers two seconds, completes with a five-second dwell and an eight-second reveal, and
// leaves the island once read.
func TestIslandTurnLifecycle(t *testing.T) {
	m := newIslandMachine()
	o := runningObs("a")
	m.observe([]islandObservation{o}, at(0))
	s := m.sessions["a"]
	if s == nil || s.Phase != islandRunning || !s.Running {
		t.Fatalf("a run starts a running session: %+v", s)
	}
	if line := s.lastLine(); line == nil || line.Kind != islandLineUser || line.Text != "Fix the tests" {
		t.Fatalf("the user's words are the first line: %+v", s.Lines)
	}

	o.Tool, o.ToolID = "$ bun test", "tool-1"
	m.observe([]islandObservation{o}, at(time.Second))
	if s.Detail != "$ bun test" || !s.DetailFromTool {
		t.Fatalf("the tool is the detail: %q", s.Detail)
	}
	o.Tool, o.ToolID = "", ""
	m.observe([]islandObservation{o}, at(2*time.Second))
	if s.Detail != "$ bun test" {
		t.Fatalf("the tool's line lingers after it ends: %q", s.Detail)
	}
	m.tick(at(2*time.Second + islandToolDetailLinger))
	if s.Detail != "" {
		t.Fatalf("the tool's line clears after %v: %q", islandToolDetailLinger, s.Detail)
	}

	o.Running, o.Reply, o.ReplyID, o.ReplyFinal = false, "All 42 tests pass.", "r-1", true
	m.observe([]islandObservation{o}, at(5*time.Second))
	if s.Phase != islandCompleted || !s.Unread {
		t.Fatalf("a finished turn completes unread: %v unread=%v", s.Phase, s.Unread)
	}
	if got := s.CompletedUntil.Sub(at(5 * time.Second)); got != islandCompletionDwell {
		t.Fatalf("completion dwell %v", got)
	}
	if got := s.RevealUntil.Sub(at(5 * time.Second)); got < islandCompletionRevealMin {
		t.Fatalf("completion reveal %v, want at least %v", got, islandCompletionRevealMin)
	}
	d := m.display(at(6 * time.Second))
	if !d.Expanded || d.Surface != islandSurfaceCompletion || d.Current.Key != "a" {
		t.Fatalf("the result is revealed as a card: %+v", d)
	}
	if got := s.summary().Text; got != "All 42 tests pass." {
		t.Fatalf("the card's summary is the reply: %q", got)
	}
	d = m.display(at(5*time.Second + s.RevealUntil.Sub(at(5*time.Second)) + time.Millisecond))
	if d.Expanded {
		t.Fatalf("the reveal ends: %+v", d)
	}
	if d.Current == nil || d.Current.Key != "a" {
		t.Fatalf("an unread result stays on the compact island: %+v", d.Current)
	}
	m.read("chat-a", true, at(20*time.Second))
	if m.sessions["a"] != nil {
		t.Fatal("a read result leaves the island")
	}
}

// TestIslandQuestionWins makes a question expand the island at once, over a result being revealed,
// which goes back to wait its turn first in line.
func TestIslandQuestionWins(t *testing.T) {
	m := newIslandMachine()
	a, b := runningObs("a"), runningObs("b")
	m.observe([]islandObservation{a, b}, at(0))
	a.Running, a.Reply, a.ReplyID, a.ReplyFinal = false, "Done with a.", "ra", true
	m.observe([]islandObservation{a, b}, at(time.Second))
	if d := m.display(at(time.Second)); d.Surface != islandSurfaceCompletion || d.Current.Key != "a" {
		t.Fatalf("a's result reveals: %+v", d)
	}
	b.Ask = &islandAsk{Kind: islandAskPermission, ChatID: "chat-b", MessageID: "m1", Command: "rm -rf build", Decisions: (&model.AgentQuestion{Kind: "command", HasRule: true}).Decisions()}
	m.observe([]islandObservation{a, b}, at(2*time.Second))
	d := m.display(at(2 * time.Second))
	if !d.Expanded || d.Surface != islandSurfaceInteraction || d.Current.Key != "b" {
		t.Fatalf("the question takes the island: %+v", d)
	}
	if len(m.queue) == 0 || m.queue[0] != "a" {
		t.Fatalf("the result waits first in line: %v", m.queue)
	}
	if d.Pill.Pending != 1 || d.Pill.Attention != 2 {
		t.Fatalf("pill counts: %+v", d.Pill)
	}

	// Answered, the question goes and a's result comes back.
	b.Ask = nil
	m.observe([]islandObservation{a, b}, at(3*time.Second))
	d = m.display(at(3 * time.Second))
	if d.Surface != islandSurfaceCompletion || d.Current.Key != "a" {
		t.Fatalf("the queued result reveals after the question: %+v", d)
	}
}

// TestIslandTurnEndKeepsAskUser keeps a bot's ask_user alive after its turn ends, as Cindy keeps
// ask_user_question through done, and fails a turn without a later completion covering it.
func TestIslandTurnEndKeepsAskUser(t *testing.T) {
	m := newIslandMachine()
	o := runningObs("a")
	m.observe([]islandObservation{o}, at(0))
	o.Ask = &islandAsk{Kind: islandAskChoices, ChatID: "chat-a", MessageID: "ask", Question: "Ship it?", Options: []model.AskOption{{Label: "Ship"}, {Label: "Wait"}}}
	o.Running = false
	m.observe([]islandObservation{o}, at(time.Second))
	s := m.sessions["a"]
	if s.Phase != islandNeedsYou || s.Running {
		t.Fatalf("the question outlives the turn: %v running=%v", s.Phase, s.Running)
	}

	f := runningObs("f")
	m.observe([]islandObservation{o, f}, at(2*time.Second))
	f.Running, f.Failure, f.FailureID = false, "Rate limit reached", "fail-1"
	m.observe([]islandObservation{o, f}, at(3*time.Second))
	failed := m.sessions["f"]
	if failed.Phase != islandFailed || !failed.Unread || failed.Detail != "Rate limit reached" {
		t.Fatalf("a failed turn: %+v", failed)
	}
	m.read("chat-f", false, at(4*time.Second))
	if !m.sessions["f"].Unread {
		t.Fatal("seeing the chat does not clear an unread error; reading it on purpose does")
	}
}

// TestIslandHoverTimers follows Cindy's hover: half a second over the menu bar opens the list, the
// pointer leaving closes it 150 ms later but not within a second of it opening, and hover cannot
// open it again for 300 ms.
func TestIslandHoverTimers(t *testing.T) {
	m := newIslandMachine()
	m.observe([]islandObservation{runningObs("a"), runningObs("b")}, at(0))
	m.hover(true, false, at(0))
	if m.display(at(400 * time.Millisecond)).Expanded {
		t.Fatal("hover opens only after its delay")
	}
	if d := m.display(at(islandHoverExpandDelay)); !d.Expanded || d.Surface != islandSurfaceList || len(d.Sessions) != 2 {
		t.Fatalf("hover opens the list: %+v", d)
	}
	m.hover(false, false, at(600*time.Millisecond))
	if !m.display(at(800 * time.Millisecond)).Expanded {
		t.Fatal("the list stays its least dwell")
	}
	if m.display(at(islandHoverExpandDelay + islandExpandedMinDwell + time.Millisecond)).Expanded {
		t.Fatal("the list closes after its dwell")
	}
	// A pass over a closed island: leaving starts the cooldown, and coming back within it arms
	// nothing.
	m.hover(true, false, at(3*time.Second))
	m.hover(false, false, at(3*time.Second+100*time.Millisecond))
	m.hover(true, false, at(3*time.Second+200*time.Millisecond))
	if !m.hoverIntentAt.IsZero() {
		t.Fatal("hover cannot open the list during its cooldown")
	}
	m.hover(false, false, at(4*time.Second))
	m.hover(true, false, at(5*time.Second))
	if m.hoverIntentAt.IsZero() {
		t.Fatal("hover arms again after the cooldown")
	}
}

// TestIslandCompactCurrentHolds keeps the compact island on one of two sessions at work for 1.2
// seconds, so the name does not flicker.
func TestIslandCompactCurrentHolds(t *testing.T) {
	m := newIslandMachine()
	a := runningObs("a")
	m.observe([]islandObservation{a}, at(0))
	if d := m.display(at(0)); d.Current.Key != "a" {
		t.Fatalf("current %s", d.Current.Key)
	}
	b := runningObs("b")
	m.observe([]islandObservation{a, b}, at(100*time.Millisecond))
	if d := m.display(at(200 * time.Millisecond)); d.Current.Key != "a" {
		t.Fatalf("the current holds: %s", d.Current.Key)
	}
	if d := m.display(at(islandCompactCurrentDwell + time.Millisecond)); d.Current.Key != "b" {
		t.Fatalf("the newer one takes over after the dwell: %s", d.Current.Key)
	}
}

// TestIslandWatchedChatHoldsBack defers the result of the chat the user is looking at, and hands a
// question they see over to the chat after five seconds; an approval stays.
func TestIslandWatchedChatHoldsBack(t *testing.T) {
	m := newIslandMachine()
	m.watch("chat-a", at(0))
	a := runningObs("a")
	m.observe([]islandObservation{a}, at(0))
	a.Running, a.Reply, a.ReplyID = false, "Done.", "r"
	m.observe([]islandObservation{a}, at(time.Second))
	if d := m.display(at(time.Second)); d.Expanded {
		t.Fatalf("the chat on screen reveals nothing: %+v", d)
	}
	b := runningObs("a2")
	b.ChatID = "chat-a"
	b.Ask = &islandAsk{Kind: islandAskChoices, ChatID: "chat-a", MessageID: "ask", Options: []model.AskOption{{Label: "x"}}}
	m.observe([]islandObservation{a, b}, at(2*time.Second))
	m.watch("chat-a", at(2*time.Second))
	for _, s := range m.ordered(at(2*time.Second + islandVisibleQuestionGrace + time.Millisecond)) {
		if s.Key == "a2" {
			t.Fatal("the question the user sees leaves the island after its grace")
		}
	}
	c := runningObs("a3")
	c.ChatID = "chat-a"
	c.Ask = &islandAsk{Kind: islandAskPermission, ChatID: "chat-a", MessageID: "perm"}
	m.observe([]islandObservation{a, b, c}, at(10*time.Second))
	if d := m.display(at(10 * time.Second)); d.Surface != islandSurfaceInteraction || d.Current.Key != "a3" {
		t.Fatalf("an approval is never held back: %+v", d)
	}
}

// TestIslandRanksAsCindy orders the sessions waiting, then unread, then at work, the newest prompt
// first within each.
func TestIslandRanksAsCindy(t *testing.T) {
	m := newIslandMachine()
	run1, run2, done, ask := runningObs("run1"), runningObs("run2"), runningObs("done"), runningObs("ask")
	m.observe([]islandObservation{run1, done, ask}, at(0))
	m.observe([]islandObservation{run1, run2, done, ask}, at(time.Second))
	done.Running, done.Reply, done.ReplyID = false, "ok", "r"
	ask.Ask = &islandAsk{Kind: islandAskElsewhere, ChatID: "chat-ask", MessageID: "q"}
	m.observe([]islandObservation{run1, run2, done, ask}, at(2*time.Second))
	m.active, m.queue = "", nil
	m.sessions["done"].RevealUntil = time.Time{}
	var keys []string
	for _, s := range m.ordered(at(3 * time.Second)) {
		keys = append(keys, s.Key)
	}
	want := []string{"ask", "done", "run2", "run1"}
	for n := range want {
		if n >= len(keys) || keys[n] != want[n] {
			t.Fatalf("order %v, want %v", keys, want)
		}
	}
}

// TestIslandSoundFor picks Cindy's sound of a change: a new question before a new error, a result,
// and a start.
func TestIslandSoundFor(t *testing.T) {
	cases := []struct {
		prev, next map[string]islandPhase
		attention  map[string]bool
		want       islandSoundEvent
	}{
		{map[string]islandPhase{}, map[string]islandPhase{"a": islandRunning}, nil, islandSoundStart},
		{map[string]islandPhase{"a": islandRunning}, map[string]islandPhase{"a": islandCompleted}, nil, islandSoundComplete},
		{map[string]islandPhase{"a": islandRunning}, map[string]islandPhase{"a": islandNeedsYou, "b": islandFailed}, map[string]bool{"a": true, "b": true}, islandSoundAttention},
		{map[string]islandPhase{"b": islandRunning}, map[string]islandPhase{"b": islandFailed}, map[string]bool{"b": true}, islandSoundError},
		{map[string]islandPhase{"a": islandRunning}, map[string]islandPhase{"a": islandRunning}, nil, islandSoundNone},
	}
	for n, c := range cases {
		if got := islandSoundFor(c.prev, c.next, c.attention); got != c.want {
			t.Errorf("case %d: %v, want %v", n, got, c.want)
		}
	}
}

// TestIslandMetricsMatchCindy holds the numbers to Cindy's: the badge's width, the compact widths
// beside a notch and a simulated one, and the expanded heights.
func TestIslandMetricsMatchCindy(t *testing.T) {
	badges := []struct {
		pill islandPill
		want float32
	}{
		{islandPill{Sessions: 3}, 22},
		{islandPill{Sessions: 12}, 22},
		{islandPill{Attention: 3, Running: 2}, 30},
		{islandPill{Attention: 12, Running: 3}, 37},
	}
	for _, b := range badges {
		if got := islandBadgeWidth(b.pill, true); got != b.want {
			t.Errorf("badge %+v: %v, want %v", b.pill, got, b.want)
		}
	}
	if got := islandSimulatedNotchWidth(1728); got != 240 {
		t.Errorf("simulated notch of 1728: %v", got)
	}
	if got := islandSimulatedNotchWidth(1440); got != 201.6 {
		t.Errorf("simulated notch of 1440: %v", got)
	}
	flat := islandScreenOf(1728, islandNotch{})
	if got := islandCompactWidth(flat, false, 0); got != 240 {
		t.Errorf("idle compact width without a notch: %v", got)
	}
	if got := islandCompactWidth(flat, true, 0); got != 328 {
		t.Errorf("busy compact width without a notch: %v", got)
	}
	notched := islandScreenOf(1512, islandNotch{Width: 185, TopBar: 32})
	if got := islandCompactWidth(notched, false, 0); got != 249 {
		t.Errorf("idle compact width with a notch: %v", got)
	}
	if got := islandCompactWidth(notched, true, 37); got != 277 {
		t.Errorf("busy compact width with a wide badge: %v", got)
	}
	if got := islandExpandedHeightFor(3, true, 0, false); got != 270 {
		t.Errorf("list of three: %v", got)
	}
	if got := islandExpandedHeightFor(1, false, 0, true); got != islandExpandedApprovalHeight {
		t.Errorf("approval card: %v", got)
	}
	if got := islandTruncate("a very long title that goes on", 12); got != "a very lo..." {
		t.Errorf("truncate: %q", got)
	}
	if got := islandToolText("Install dependencies", "bun install\n--frozen", ""); got != "Install dependencies · $ bun install" {
		t.Errorf("tool text: %q", got)
	}
}

// TestIslandShapeReachesIntoTheMenuBar draws the notch's silhouette: wider than the body at the top
// by its extension, and down to the body's bottom.
func TestIslandShapeReachesIntoTheMenuBar(t *testing.T) {
	r := ui.Rect{X: 100, Y: 0, W: 300, H: 120}
	b := islandNotchPath(r, 14, 24, 32).Bounds()
	if b.X > 86.5 || b.X+b.W < 413.5 || b.H < 119 {
		t.Fatalf("shape bounds %+v", b)
	}
}

// islandTestScenes is a machine in each state the island shows, for the screenshots.
func islandTestScenes(t *testing.T) map[string]islandDisplay {
	t.Helper()
	demoWindow(t)
	bots := store.Bots
	botID := func(n int) string {
		if n < len(bots) {
			return bots[n].ID
		}
		return ""
	}
	running := &islandSession{Key: "s1", ChatID: store.Chats[0].ID, BotID: botID(0), Title: "Atlas", Phase: islandRunning, Running: true, Detail: "Install dependencies · $ bun install", DetailFromTool: true, StartedAt: time.Now().Add(-42 * time.Second)}
	agent := &islandSession{Key: "s2", ChatID: store.Chats[0].ID, AgentKind: "claude", Title: "Claude Code", Phase: islandRunning, Running: true, Lines: []islandLine{{ID: 1, Kind: islandLineAssistant, Text: "Updating the island's shape to hug the notch"}}}
	asking := &islandSession{Key: "s3", ChatID: store.Chats[0].ID, BotID: botID(1), Title: "Nova", Phase: islandNeedsYou, Generation: 3,
		Ask: &islandAsk{Kind: islandAskPermission, ChatID: store.Chats[0].ID, MessageID: "m", Command: "bun test --watch", Decisions: (&model.AgentQuestion{Kind: "command", HasRule: true}).Decisions()}, Detail: "bun test --watch"}
	question := &islandSession{Key: "s4", ChatID: store.Chats[0].ID, BotID: botID(0), Title: "Atlas", Phase: islandNeedsYou, Generation: 4,
		Ask: &islandAsk{Kind: islandAskChoices, ChatID: store.Chats[0].ID, MessageID: "q", Question: "Ship 1.0.37 now?", Options: []model.AskOption{{Label: "Ship now", Description: "Tag and upload the draft"}, {Label: "Wait for the fix"}}}, Detail: "Ship 1.0.37 now?"}
	done := &islandSession{Key: "s5", ChatID: store.Chats[0].ID, BotID: botID(0), Title: "Atlas", Phase: islandCompleted, Unread: true, Generation: 5, Lines: []islandLine{{ID: 1, Kind: islandLineAssistant, Text: "4 files changed, tests pass"}}}
	failed := &islandSession{Key: "s6", ChatID: store.Chats[0].ID, BotID: botID(1), Title: "Nova", Phase: islandFailed, Unread: true, Detail: "Rate limit reached, try again in a minute"}
	pill := func(list ...*islandSession) islandPill { return newIslandMachine().pill(list, time.Now()) }
	return map[string]islandDisplay{
		"2-running":        {Current: running, Sessions: []*islandSession{running}, Pill: pill(running)},
		"2b-running-agent": {Current: agent, Sessions: []*islandSession{agent, running}, Pill: pill(agent, running)},
		"3-approval":       {Expanded: true, Surface: islandSurfaceInteraction, Current: asking, Sessions: []*islandSession{asking}, Pill: pill(asking), Shadow: true},
		"4-question":       {Expanded: true, Surface: islandSurfaceInteraction, Current: question, Sessions: []*islandSession{question}, Pill: pill(question), Shadow: true},
		"5-completed":      {Expanded: true, Surface: islandSurfaceCompletion, Current: done, Sessions: []*islandSession{done}, Pill: pill(done), Shadow: true},
		"6-failed":         {Expanded: true, Surface: islandSurfaceCompletion, Current: failed, Sessions: []*islandSession{failed}, Pill: pill(failed), Shadow: true},
		"7-list":           {Expanded: true, Surface: islandSurfaceList, Current: asking, Sessions: []*islandSession{asking, done, agent, running}, Pill: pill(asking, done, agent, running), Shadow: true},
		"8-compact-done":   {Current: done, Sessions: []*islandSession{done, running}, Pill: pill(done, running)},
	}
}

// TestIslandGallery draws every state on a display without a notch and one with, cropped to the
// island, for a look at them side by side.
func TestIslandGallery(t *testing.T) {
	displays := islandTestScenes(t)
	screens := map[string]islandScreen{
		"flat":  islandScreenOf(1728, islandNotch{TopBar: 24}),
		"notch": islandScreenOf(1512, islandNotch{Width: 185, TopBar: 32}),
	}
	for name, d := range displays {
		for screenName, s := range screens {
			i := &island{m: newIslandMachine(), swipe: map[string]*islandSwipe{}}
			shown, screen := d, s
			tt := ui.NewTester(func(c *ui.Context) { i.draw(c, shown, screen, false) }, islandWindowWidth, 420)
			settle(tt)
			tt.Advance(600 * time.Millisecond)
			settle(tt)
			if shown.Current != nil && !showsLine(tt, shown.Current.Title) && !(screenName == "notch" && !shown.Expanded) {
				t.Errorf("%s on %s: no title in %q", name, screenName, tt.Texts())
			}
			renderTo(t, tt, "island-"+screenName+"-"+name)
		}
	}
}

// TestIslandApprovalAnswers clicks Allow once on an approval card and finds the decision in the
// store's card.
func TestIslandApprovalAnswers(t *testing.T) {
	demoWindow(t)
	chat := store.Chat("chat-patch")
	var agentMessage *model.Message
	for _, message := range chat.Messages {
		if model.AgentOf(message) != nil {
			agentMessage = message
			break
		}
	}
	if agentMessage == nil {
		t.Fatal("the demo has no agent card")
	}
	store.Update(agentMessage.ID, chat.ID, func(message *model.Message) {
		tool := *message.Body.Tool
		agent := *tool.Agent
		agent.State = model.AgentAsking
		agent.Question = &model.AgentQuestion{Kind: "command", Command: "bun test", HasRule: true}
		tool.Agent = &agent
		message.Body.Tool = &tool
	})
	var ask *islandAsk
	for _, o := range islandObservations(store) {
		if o.Ask != nil && o.Ask.Kind == islandAskPermission && o.Ask.MessageID == agentMessage.ID {
			ask = o.Ask
		}
	}
	if ask == nil || ask.Command != "bun test" || len(ask.Decisions) != 3 {
		t.Fatalf("the agent's approval: %+v", ask)
	}
	s := &islandSession{Key: "k", ChatID: chat.ID, Title: "Claude Code", AgentKind: "claude", Phase: islandNeedsYou, Ask: ask, Generation: 1}
	d := islandDisplay{Expanded: true, Surface: islandSurfaceInteraction, Current: s, Sessions: []*islandSession{s}}
	i := &island{m: newIslandMachine(), swipe: map[string]*islandSwipe{}}
	tt := ui.NewTester(func(c *ui.Context) { i.draw(c, d, islandScreenOf(1728, islandNotch{TopBar: 24}), false) }, islandWindowWidth, 420)
	settle(tt)
	if err := tt.Click(L("Allow once")); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	settle(tt)
	if agent := model.AgentOf(store.Chat(chat.ID).Message(agentMessage.ID)); agent.Question != nil || agent.State != model.AgentWorking {
		t.Fatalf("the click did not answer: %+v", agent)
	}
}

// TestIslandObservesBotQuestions reads a bot's ask_user into its session, answered in place, and
// several picks into one the chat answers.
func TestIslandObservesBotQuestions(t *testing.T) {
	demoWindow(t)
	chat := store.Chats[0]
	last := chat.Messages[len(chat.Messages)-1]
	store.Update(last.ID, chat.ID, func(message *model.Message) {
		message.Author = model.BotAuthor(store.Bots[0].ID)
		message.Body = model.Body{Kind: model.BodyPermission, Request: &model.PermissionRequest{
			Tool: "ask", Decision: model.DecisionPending,
			Ask: &model.AskQuestion{Question: "Ship it?", Options: []model.AskOption{{Label: "Ship now"}, {Label: "Wait"}}},
		}}
	})
	find := func() *islandAsk {
		for _, o := range islandObservations(store) {
			if o.Ask != nil && o.Ask.MessageID == last.ID {
				return o.Ask
			}
		}
		return nil
	}
	if ask := find(); ask == nil || ask.Kind != islandAskChoices || len(ask.Options) != 2 {
		t.Fatalf("the bot's question: %+v", ask)
	}
	store.Update(last.ID, chat.ID, func(message *model.Message) { message.Body.Request.Ask.Multiple = true })
	if ask := find(); ask == nil || ask.Kind != islandAskElsewhere {
		t.Fatalf("several picks open the chat: %+v", ask)
	}
}

// TestIslandGalleryDrawsEveryScene opens the gallery on each scene and display, so none of them
// panics or leaves its name out of the list.
func TestIslandGalleryDrawsEveryScene(t *testing.T) {
	demoWindow(t)
	g := &islandGalleryWindow{island: &island{m: newIslandMachine(), swipe: map[string]*islandSwipe{}}}
	tt := ui.NewTester(g.view, 1240, 760)
	scenes := islandScenes()
	for n, s := range scenes {
		for screen := range islandGalleryScreens {
			g.scene, g.screen = n, screen
			settle(tt)
		}
		if !showsLine(tt, s.Name) {
			t.Errorf("scene %q is not in the list", s.Name)
		}
		if n == 0 || n == 14 || n == len(scenes)-1 {
			// The island springs to its scene's size before the frame is kept.
			tt.Advance(700 * time.Millisecond)
			settle(tt)
			renderTo(t, tt, "island-gallery-"+s.Group)
		}
	}
}

// TestIslandComesOutOfTheNotch draws the island empty, then with a session, then empty again: it is
// nothing at first, grows out of the notch to its compact size, and goes back in, its windows hiding
// once it is gone.
func TestIslandComesOutOfTheNotch(t *testing.T) {
	demoWindow(t)
	notched := islandScreenOf(1512, islandNotch{Width: 185, TopBar: 32})
	flat := islandScreenOf(1728, islandNotch{TopBar: 24})
	running := galleryCompact(galleryRunning("r1", 0, "Install dependencies · $ bun install"))
	for name, s := range map[string]islandScreen{"notch": notched, "flat": flat} {
		i := &island{m: newIslandMachine(), swipe: map[string]*islandSwipe{}}
		d := islandDisplay{}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.Hex("#1f2430")).Children(func() { i.draw(c, d, s, false) })
		}, 560, 60)
		settle(tt)
		if showsLine(tt, "Install dependencies") {
			t.Fatalf("%s: an idle island draws nothing", name)
		}
		renderTo(t, tt, "transition-"+name+"-0-idle")
		d = running
		settle(tt)
		tt.Advance(140 * time.Millisecond)
		settle(tt)
		renderTo(t, tt, "transition-"+name+"-1-growing")
		tt.Advance(900 * time.Millisecond)
		settle(tt)
		if !showsLine(tt, "Atlas") && !showsLine(tt, store.Bots[0].Name) {
			t.Fatalf("%s: the session's island never came out: %q", name, tt.Texts())
		}
		renderTo(t, tt, "transition-"+name+"-2-shown")
		d = islandDisplay{}
		settle(tt)
		tt.Advance(120 * time.Millisecond)
		settle(tt)
		renderTo(t, tt, "transition-"+name+"-3-leaving")
		if i.leaving.Current == nil {
			t.Fatalf("%s: the leaving island stopped drawing its session before it went in", name)
		}
		tt.Advance(1200 * time.Millisecond)
		settle(tt)
		if i.leaving.Current != nil {
			t.Fatalf("%s: the island never went back in", name)
		}
		renderTo(t, tt, "transition-"+name+"-4-gone")
	}
}

// TestIslandFailedMark draws a failure's card: the bot's avatar in its red ring, the card washed
// red, and the Failed pill.
func TestIslandFailedMark(t *testing.T) {
	demoWindow(t)
	failed := galleryFailed("e1", 1, "Rate limit reached, try again in a minute")
	agent := galleryFailed("e2", 0, "Claude Code exited with code 1")
	agent.AgentKind, agent.Title = "claude", "Claude Code"
	for name, d := range map[string]islandDisplay{
		"card":    galleryCard(islandSurfaceCompletion, failed),
		"agent":   galleryCard(islandSurfaceCompletion, agent),
		"list":    galleryList(failed, failed, galleryDone("d1", 0, "4 files changed, tests pass"), galleryRunning("r1", 0, "$ bun install")),
		"compact": galleryCompact(failed),
	} {
		i := &island{m: newIslandMachine(), swipe: map[string]*islandSwipe{}}
		shown := d
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(ui.Hex("#1f2430")).Children(func() { i.draw(c, shown, islandScreenOf(1728, islandNotch{TopBar: 24}), false) })
		}, 560, 300)
		settle(tt)
		tt.Advance(1200 * time.Millisecond)
		settle(tt)
		if d.Expanded && !showsLine(tt, L("Failed")) {
			t.Errorf("%s: no Failed pill in %q", name, tt.Texts())
		}
		renderTo(t, tt, "failed-"+name)
	}
}
