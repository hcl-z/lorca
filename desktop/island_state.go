package main

import (
	"slices"
	"time"
)

// The island's sessions and what it shows of them, ported from Cindy's Agent Island state machine
// (makecindy/cindy, Apache-2.0, apps/desktop/src/main/agent-island/state.ts): each bot at work in a
// chat and each coding agent is a session, with a phase, an activity line, and its timers; the
// island shows the one waiting on the user, else the result just in, else the one first in line.
//
// Lorca's store says what is so now, not what happened, so the machine reads a snapshot of every
// session (islandObservation) and turns the changes between two snapshots into Cindy's transitions:
// a turn that starts, a tool that runs, a question that comes or goes, a turn that ends or fails.

// Cindy's timers, in milliseconds there.
const (
	islandHoverExpandDelay     = 500 * time.Millisecond
	islandLeaveCollapseDelay   = 150 * time.Millisecond
	islandHoverCooldown        = 300 * time.Millisecond
	islandOutsideClickGuard    = 300 * time.Millisecond
	islandExpandedMinDwell     = 1000 * time.Millisecond
	islandCompletionDwell      = 5000 * time.Millisecond
	islandCompletionRevealMin  = 8000 * time.Millisecond
	islandErrorDwell           = 12000 * time.Millisecond
	islandErrorRevealDwell     = 12000 * time.Millisecond
	islandRevealDwell          = 5000 * time.Millisecond
	islandCompactCurrentDwell  = 1200 * time.Millisecond
	islandToolDetailLinger     = 2000 * time.Millisecond
	islandPreviewMinDwell      = 1600 * time.Millisecond
	islandCompletedRetention   = 24 * time.Hour
	islandUnreadErrorRetention = 4 * time.Hour
	// islandVisibleQuestionGrace is how long a question the user is already looking at in the app
	// stays on the island before the chat's own card takes it over.
	islandVisibleQuestionGrace = islandCompletionDwell
)

// islandPhase is where a session stands, as Cindy's phase.
type islandPhase int

const (
	islandRunning islandPhase = iota
	islandNeedsYou
	islandCompleted
	islandFailed
)

func (p islandPhase) String() string {
	switch p {
	case islandNeedsYou:
		return "needs-interaction"
	case islandCompleted:
		return "completed"
	case islandFailed:
		return "error"
	}
	return "running"
}

// islandLineKind is an activity line's kind, as Cindy's.
type islandLineKind int

const (
	islandLineUser islandLineKind = iota
	islandLineAssistant
	islandLineStatus
	islandLineTool
)

type islandLine struct {
	ID   int
	Kind islandLineKind
	Text string
}

// islandObservation is one session as the store has it now: whether its turn runs, what it last
// said and asked, and what it runs. The store is read into these once per change (island_store.go).
type islandObservation struct {
	Key    string
	ChatID string
	BotID  string
	// AgentKind is a coding agent's kind (`claude`, `codex`, …); empty for a bot.
	AgentKind string
	Title     string
	Running   bool
	// Tool is the line of the tool running now, and ToolID tells one call from the next.
	Tool, ToolID string
	// Reply is the bot's latest words of this turn, ReplyID the message they are in, and
	// ReplyFinal whether they are done streaming.
	Reply, ReplyID string
	ReplyFinal     bool
	// Prompt is the user's latest message to it, PromptID the message.
	Prompt, PromptID string
	// Failure is why the turn failed, when it did.
	Failure, FailureID string
	Ask                *islandAsk
	// Muted is a chat the user muted: tracked, never shown.
	Muted bool
}

// islandSession is a session's state on the island, after Cindy's AgentIslandSessionState.
type islandSession struct {
	Key, ChatID, BotID, AgentKind, Title string
	Phase                                islandPhase
	Running                              bool
	Detail                               string
	DetailFromTool                       bool
	ToolID                               string
	ToolDetailUntil                      time.Time
	Lines                                []islandLine
	lineSeq                              int
	streamLineID                         int
	streamReplyID                        string
	Preview                              *islandLine
	previewUntil                         time.Time
	previewQueue                         []islandLine
	Ask                                  *islandAsk
	askKey                               string
	// StartedAt is when the run began, SortAt when the user last wrote to it, LastAt its latest
	// activity, and CompletedAt when it first completed.
	StartedAt, SortAt, LastAt, CompletedAt               time.Time
	CompletedUntil, FailedUntil, RevealUntil, SuppressAt time.Time
	Unread                                               bool
	// Generation tells one card from the next: a click carries it, and one on a card since replaced
	// does nothing.
	Generation uint64
	// Dismissed is a question card the user closed: it stays in the list, and opens again only for
	// a new question.
	Dismissed bool
	// Deferred is a reveal that waits in the queue, or one the user is looking at in the app.
	Deferred        bool
	queuedDwell     time.Duration
	lastPromptID    string
	lastFailureID   string
	lastSeenReplyID string
	Muted           bool
}

// islandMachine is the island's state over every session, after Cindy's AgentIslandState.
type islandMachine struct {
	sessions   map[string]*islandSession
	generation uint64
	// active is the result or failure revealed now; queue the ones waiting their turn.
	active string
	queue  []string
	// The pointer: over the menu bar strip or the panel, hover expanding it after a delay, leaving
	// collapsing it after another.
	inMenuBar, inPanel bool
	hoverIntentAt      time.Time
	hoverExpanded      bool
	collapseAt         time.Time
	cooldownUntil      time.Time
	expandedAt         time.Time
	autoRevealAt       time.Time
	// expandedOrder is the list's order while it is open, so rows do not move under the pointer.
	expandedOrder []string
	// compactCurrent is the session the compact island names, kept a moment so two at work do not
	// flicker.
	compactCurrent      string
	compactCurrentUntil time.Time
	// watched is the chat the user is looking at in the app, with the app in front.
	watched string
	// last is what the island showed last, for the sounds and a click.
	last islandDisplay
	// now is the time of the change being handled, for the rules that read the clock.
	now time.Time
}

func newIslandMachine() *islandMachine {
	return &islandMachine{sessions: map[string]*islandSession{}}
}

// islandSurface is what the expanded island holds.
type islandSurface int

const (
	islandSurfaceCollapsed islandSurface = iota
	islandSurfaceList
	islandSurfaceInteraction
	islandSurfaceCompletion
)

// islandDisplay is what the island shows now, after Cindy's display decision.
type islandDisplay struct {
	Expanded bool
	Surface  islandSurface
	// Current is the session the compact island names, or the expanded card's.
	Current *islandSession
	// Sessions are the list's rows, or the one card's.
	Sessions []*islandSession
	Pill     islandPill
	// Shadow is the island's shadow on: expanded, under the pointer, or a question waiting.
	Shadow bool
}

// islandPill is the counts the compact badge shows.
type islandPill struct {
	Sessions, Running, Pending, UnreadCompleted, Attention int
}

func (m *islandMachine) nextGeneration(s *islandSession) {
	m.generation++
	s.Generation = m.generation
}

// observe folds the store's sessions into the machine, as Cindy's event handlers do.
func (m *islandMachine) observe(list []islandObservation, now time.Time) {
	m.now = now
	seen := map[string]bool{}
	for _, o := range list {
		seen[o.Key] = true
		s := m.sessions[o.Key]
		if s == nil {
			if !o.Running && o.Ask == nil {
				// Nothing at work and nothing asked: a session starts with a run or a question.
				continue
			}
			s = &islandSession{Key: o.Key, ChatID: o.ChatID, BotID: o.BotID, AgentKind: o.AgentKind, StartedAt: now, SortAt: now, lastPromptID: o.PromptID, lastSeenReplyID: o.ReplyID, lastFailureID: o.FailureID}
			m.sessions[o.Key] = s
			// A run found under way begins with the user's words that started it.
			if o.Running {
				s.appendLine(islandLineUser, islandActivityText(o.Prompt))
			}
		}
		s.Title, s.Muted = o.Title, o.Muted
		m.observeOne(s, o, now)
	}
	for key, s := range m.sessions {
		if !seen[key] {
			// The store no longer lists it: a bot whose turn ended with nothing asked, or a card
			// gone. It is read as at rest, so a turn that ran ends in its result, a question that
			// went is answered, and a session already at rest stays as it is.
			m.observeOne(s, islandObservation{Key: key, ChatID: s.ChatID, BotID: s.BotID, AgentKind: s.AgentKind, Title: s.Title, Muted: s.Muted}, now)
		}
	}
	m.prune(now)
}

func (m *islandMachine) observeOne(s *islandSession, o islandObservation, now time.Time) {
	// The user wrote to it: a new run, and their words as the first line.
	if o.PromptID != "" && o.PromptID != s.lastPromptID {
		s.lastPromptID = o.PromptID
		m.markRunning(s, now)
		s.Phase = islandRunning
		s.Detail, s.DetailFromTool, s.ToolID = "", false, ""
		s.streamLineID, s.streamReplyID = 0, ""
		if line := s.appendLine(islandLineUser, islandActivityText(o.Prompt)); line != nil {
			s.enqueuePreview(*line, now)
		}
		s.SortAt = now
	}
	if o.Running && !s.Running {
		m.markRunning(s, now)
		if s.Ask == nil {
			s.Phase = islandRunning
		}
	}
	// Words of the reply, streamed into one line in place.
	if o.Reply != "" && (o.ReplyID != s.lastSeenReplyID || o.Running) {
		s.lastSeenReplyID = o.ReplyID
		s.applyReply(o.ReplyID, o.Reply, o.ReplyFinal, now)
		if s.DetailFromTool {
			s.clearTool()
		}
	}
	// A tool that runs, and the one that just ended lingering a moment.
	switch {
	case o.Tool != "" && o.ToolID != s.ToolID:
		s.streamLineID, s.streamReplyID = 0, ""
		s.ToolID = o.ToolID
		s.LastAt = now
		if s.Ask == nil {
			s.Phase = islandRunning
			s.Detail, s.DetailFromTool = o.Tool, true
			s.ToolDetailUntil = time.Time{}
			s.appendLine(islandLineTool, o.Tool)
		}
	case o.Tool == "" && s.ToolID != "" && s.DetailFromTool && s.ToolDetailUntil.IsZero():
		s.ToolDetailUntil = now.Add(islandToolDetailLinger)
	}
	// A question that comes, or goes once answered.
	key := ""
	if o.Ask != nil {
		key = o.Ask.ChatID + ":" + o.Ask.MessageID
	}
	if key != s.askKey {
		hadAsk := s.Ask != nil
		s.askKey, s.Ask = key, o.Ask
		if o.Ask != nil {
			s.Phase = islandNeedsYou
			s.Detail, s.DetailFromTool = o.Ask.Question, false
			s.CompletedUntil, s.FailedUntil = time.Time{}, time.Time{}
			s.Dismissed = false
			s.SuppressAt = time.Time{}
			m.preemptReveal(now)
			s.RevealUntil = now.Add(islandRevealDwell)
			s.Deferred = false
			m.nextGeneration(s)
			s.LastAt = now
		} else if hadAsk {
			m.answered(s, o.Running, now)
		}
	}
	// A turn that failed: an error card, unread until the user reads it.
	if o.FailureID != "" && o.FailureID != s.lastFailureID {
		s.lastFailureID = o.FailureID
		m.fail(s, o.Failure, now)
	}
	// A turn that ended.
	if !o.Running && s.Running {
		s.Running = false
		s.clearTool()
		if s.Ask != nil {
			s.Phase = islandNeedsYou
			m.nextGeneration(s)
		} else if s.Phase != islandFailed {
			m.complete(s, now)
		}
	}
}

// markRunning is a session at work again: a new card if it was not, and no result or reveal left.
func (m *islandMachine) markRunning(s *islandSession, now time.Time) {
	if !s.Running {
		m.nextGeneration(s)
		s.StartedAt = now
		// The Done a completion left last says nothing of the run starting again.
		if last := s.lastLine(); last != nil && last.Kind == islandLineStatus && last.Text == L("Done") {
			s.Lines = s.Lines[:len(s.Lines)-1]
		}
	}
	s.Running = true
	s.CompletedUntil, s.FailedUntil, s.CompletedAt = time.Time{}, time.Time{}, time.Time{}
	if m.active == s.Key {
		m.active = ""
	}
	s.RevealUntil, s.Deferred = time.Time{}, false
	m.queue = slices.DeleteFunc(m.queue, func(k string) bool { return k == s.Key })
	s.LastAt = now
}

// answered is a question gone: the run goes on, or the result it ended with comes back, else the
// session is done.
func (m *islandMachine) answered(s *islandSession, running bool, now time.Time) {
	if !(s.Phase == islandFailed && s.Unread) {
		s.RevealUntil, s.Unread = time.Time{}, false
	}
	s.Detail = ""
	switch {
	case running || s.Running:
		s.Phase = islandRunning
	case s.CompletedUntil.After(now):
		s.Phase = islandCompleted
	case s.FailedUntil.After(now):
		s.Phase = islandFailed
	default:
		delete(m.sessions, s.Key)
	}
}

// complete is a turn done: the result stays five seconds and is revealed for at least eight, longer
// while previews of what it said still play.
func (m *islandMachine) complete(s *islandSession, now time.Time) {
	if s.Phase == islandFailed {
		return
	}
	s.Running = false
	s.Phase = islandCompleted
	if s.CompletedAt.IsZero() {
		s.CompletedAt = now
	}
	s.Detail, s.DetailFromTool = "", false
	if last := s.lastLine(); last == nil || last.Kind == islandLineUser || last.Kind == islandLineTool {
		s.appendLine(islandLineStatus, L("Done"))
	}
	s.CompletedUntil = now.Add(islandCompletionDwell)
	s.Unread = true
	m.nextGeneration(s)
	m.reveal(s, max(islandCompletionRevealMin, s.remainingPreview(now)+islandRevealDwell), now)
}

// fail is a turn that failed: its error, kept twelve seconds and unread until read.
func (m *islandMachine) fail(s *islandSession, why string, now time.Time) {
	s.Phase = islandFailed
	s.Running = false
	s.Ask, s.askKey = nil, ""
	s.Detail, s.DetailFromTool = islandTruncate(why, islandDetailMax), false
	if s.Detail == "" {
		s.Detail = L("Error")
	}
	s.appendLine(islandLineStatus, s.Detail)
	s.FailedUntil = now.Add(islandErrorDwell)
	s.Unread = true
	m.nextGeneration(s)
	m.reveal(s, islandErrorRevealDwell, now)
	s.LastAt = now
}

// reveal shows a result or failure for dwell: now, unless another is showing, when it queues; or
// deferred, when the user is looking at its chat in the app.
func (m *islandMachine) reveal(s *islandSession, dwell time.Duration, now time.Time) {
	if s.Muted {
		return
	}
	if m.suppressed(s) {
		s.Deferred = true
		if s.Phase != islandFailed {
			s.Unread = false
		}
		return
	}
	if a := m.sessions[m.active]; a != nil && a.Key != s.Key && a.RevealUntil.After(now) {
		s.Deferred, s.queuedDwell = true, dwell
		if !slices.Contains(m.queue, s.Key) {
			m.queue = append(m.queue, s.Key)
		}
		return
	}
	m.active = s.Key
	s.RevealUntil, s.Deferred = now.Add(dwell), false
	m.autoRevealAt = now
}

// preemptReveal puts the reveal showing now back in the queue for a question, first in line if
// its result is still unread.
func (m *islandMachine) preemptReveal(now time.Time) {
	a := m.sessions[m.active]
	m.active = ""
	if a == nil || !a.RevealUntil.After(now) {
		return
	}
	a.RevealUntil = time.Time{}
	if a.Unread {
		a.Deferred, a.queuedDwell = true, islandRevealDwell
		m.queue = append([]string{a.Key}, slices.DeleteFunc(m.queue, func(k string) bool { return k == a.Key })...)
	}
}

// promote reveals the next queued result once the one before it is done and nothing else holds the
// island.
func (m *islandMachine) promote(now time.Time, blocking bool) {
	if a := m.sessions[m.active]; a != nil && a.RevealUntil.After(now) {
		return
	}
	m.active = ""
	if m.manualExpanded() || blocking {
		return
	}
	for len(m.queue) > 0 {
		key := m.queue[0]
		m.queue = m.queue[1:]
		s := m.sessions[key]
		if s == nil || s.Muted || m.suppressed(s) || !m.visible(s, now) {
			continue
		}
		dwell := s.queuedDwell
		if dwell == 0 {
			dwell = islandRevealDwell
		}
		m.active = key
		s.RevealUntil, s.Deferred = now.Add(dwell), false
		m.autoRevealAt = now
		return
	}
}

// suppressed is a session whose chat the user is looking at in the app: its card would say what
// they see. A permission is never held back.
func (m *islandMachine) suppressed(s *islandSession) bool {
	return m.watched != "" && m.watched == s.ChatID && !(s.Ask != nil && s.Ask.Kind == islandAskPermission)
}

// watch follows the chat the user looks at: what it reveals is deferred, and a question they see in
// the chat leaves the island after a grace.
func (m *islandMachine) watch(chatID string, now time.Time) {
	m.watched = chatID
	for _, s := range m.sessions {
		if chatID == "" || s.ChatID != chatID {
			s.SuppressAt = time.Time{}
			continue
		}
		if s.RevealUntil.After(now) {
			s.RevealUntil, s.Deferred = time.Time{}, true
			if s.Phase == islandCompleted {
				s.Unread = false
			}
		}
		if s.Ask != nil && s.Ask.Kind != islandAskPermission && s.SuppressAt.IsZero() {
			s.SuppressAt = now.Add(islandVisibleQuestionGrace)
		}
	}
}

// read is the user reading a chat: its results are no longer unread, its errors are, until read
// on purpose.
func (m *islandMachine) read(chatID string, explicit bool, now time.Time) {
	for key, s := range m.sessions {
		if s.ChatID != chatID {
			continue
		}
		if s.Phase == islandFailed && !explicit {
			continue
		}
		s.Unread = false
		if explicit {
			s.RevealUntil, s.CompletedUntil, s.FailedUntil = time.Time{}, time.Time{}, time.Time{}
			if m.active == key {
				m.active = ""
			}
		}
	}
	m.prune(now)
}

// visible is a session the island still holds: at work, asking, or a result inside its time.
func (m *islandMachine) visible(s *islandSession, now time.Time) bool {
	if s.Phase == islandCompleted && !s.CompletedAt.IsZero() && !now.Before(s.CompletedAt.Add(islandCompletedRetention)) {
		return false
	}
	switch {
	case s.Ask != nil, s.Running:
		return true
	case s.Unread && s.Phase == islandFailed && now.Before(s.LastAt.Add(islandUnreadErrorRetention)):
		return true
	case s.Unread && s.Phase == islandCompleted:
		return true
	case s.CompletedUntil.After(now), s.FailedUntil.After(now):
		return true
	}
	return m.pointerInside() && (s.Phase == islandCompleted || s.Phase == islandFailed)
}

func (m *islandMachine) prune(now time.Time) {
	for key, s := range m.sessions {
		if !m.visible(s, now) {
			delete(m.sessions, key)
			if m.active == key {
				m.active = ""
			}
		}
	}
}

// tick runs the timers due by now: previews that move on, a tool's line that lingers, hover and
// collapse.
func (m *islandMachine) tick(now time.Time) {
	m.now = now
	for _, s := range m.sessions {
		for s.Preview != nil && !s.previewUntil.After(now) {
			if len(s.previewQueue) == 0 {
				s.Preview = nil
				break
			}
			next := s.previewQueue[0]
			s.previewQueue = s.previewQueue[1:]
			s.Preview, s.previewUntil = &next, now.Add(islandPreviewMinDwell)
		}
		if s.DetailFromTool && !s.ToolDetailUntil.IsZero() && !s.ToolDetailUntil.After(now) {
			s.clearTool()
		}
	}
	if !m.hoverIntentAt.IsZero() && !m.hoverIntentAt.After(now) {
		m.hoverIntentAt = time.Time{}
		if m.inMenuBar || m.inPanel {
			m.hoverExpanded = true
			m.expandedAt = now
		}
	}
	if !m.collapseAt.IsZero() && !m.collapseAt.After(now) {
		if until := m.expandedAt.Add(islandExpandedMinDwell); until.After(now) {
			m.collapseAt = until
		} else {
			m.collapseAt = time.Time{}
			m.hoverExpanded = false
			m.expandedOrder = nil
		}
	}
	m.prune(now)
}

// nextDeadline is the earliest timer still to run, for the one wake-up the island schedules.
func (m *islandMachine) nextDeadline(now time.Time) time.Time {
	var next time.Time
	add := func(t time.Time) {
		if t.After(now) && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	add(m.hoverIntentAt)
	add(m.collapseAt)
	add(m.compactCurrentUntil)
	for _, s := range m.sessions {
		add(s.previewUntil)
		add(s.ToolDetailUntil)
		add(s.CompletedUntil)
		add(s.FailedUntil)
		add(s.RevealUntil)
		add(s.SuppressAt)
		if !s.CompletedAt.IsZero() {
			add(s.CompletedAt.Add(islandCompletedRetention))
		}
		if s.Phase == islandFailed && s.Unread {
			add(s.LastAt.Add(islandUnreadErrorRetention))
		}
	}
	return next
}

// The pointer, as the native side reports it in Cindy: over the menu bar strip, or the panel.

func (m *islandMachine) pointerInside() bool { return m.inMenuBar || m.inPanel }

// manualExpanded is the list the user opened: by hover or a click, or the pointer still in the open
// panel or on its way out, unless a question or a reveal holds the island.
func (m *islandMachine) manualExpanded() bool {
	return m.hoverExpanded || (m.inPanel || !m.collapseAt.IsZero()) && !m.hoverSuppressed(m.now)
}

// hoverSuppressed is a question or a reveal holding the island: hover does not expand it into the
// list over them.
func (m *islandMachine) hoverSuppressed(now time.Time) bool {
	for _, s := range m.sessions {
		if m.blocking(s, now) {
			return true
		}
	}
	a := m.sessions[m.active]
	return a != nil && a.RevealUntil.After(now) && !m.suppressed(a)
}

// hover follows the pointer's zones, as Cindy's setAgentIslandPointerZones: inside, a pending
// collapse is off, and over the menu bar strip of a closed island hover opens it after its delay,
// unless a question or a reveal holds the island or the cooldown runs; leaving, an open list closes
// after its delay, and the cooldown starts.
func (m *islandMachine) hover(menuBar, panel bool, now time.Time) {
	m.now = now
	m.inMenuBar, m.inPanel = menuBar, panel
	if m.pointerInside() {
		m.collapseAt = time.Time{}
		switch {
		case m.hoverExpanded || panel:
			m.hoverIntentAt = time.Time{}
		case menuBar && !m.hoverSuppressed(now) && !m.cooldownUntil.After(now):
			m.hoverIntentAt = now.Add(islandHoverExpandDelay)
		default:
			m.hoverIntentAt = time.Time{}
		}
		return
	}
	m.hoverIntentAt = time.Time{}
	if m.hoverExpanded {
		m.collapseAt = now.Add(islandLeaveCollapseDelay)
	} else {
		m.collapseAt = time.Time{}
	}
	m.cooldownUntil = now.Add(islandHoverCooldown)
}

// expand is a click on the compact island: the list, at once.
func (m *islandMachine) expand(now time.Time) {
	m.hoverIntentAt, m.collapseAt = time.Time{}, time.Time{}
	m.hoverExpanded = true
	m.expandedAt = now
}

// collapse folds the island: a click on its compact place, a row opened, or a click outside.
func (m *islandMachine) collapse(now time.Time) {
	m.hoverIntentAt, m.collapseAt = time.Time{}, time.Time{}
	m.hoverExpanded = false
	m.expandedOrder = nil
	m.cooldownUntil = now.Add(islandHoverCooldown)
}

// outsideClick closes the card showing: a result goes back to wait, a question stays in the list
// without opening again. A click right after the island opened by itself is taken for a miss.
func (m *islandMachine) outsideClick(now time.Time) {
	if now.Sub(m.autoRevealAt) < islandOutsideClickGuard {
		return
	}
	if s := m.last.Current; s != nil && m.last.Expanded {
		switch m.last.Surface {
		case islandSurfaceCompletion:
			if a := m.sessions[s.Key]; a != nil {
				a.RevealUntil = time.Time{}
				if a.Unread {
					a.Deferred = true
				}
			}
			m.active = ""
		case islandSurfaceInteraction:
			if a := m.sessions[s.Key]; a != nil {
				a.Dismissed = true
			}
		}
	}
	m.collapse(now)
}

// opened is the user opening a session's chat from the island: its card closes, and a result is
// read.
func (m *islandMachine) opened(key string, now time.Time) {
	m.now = now
	if s := m.sessions[key]; s != nil {
		if s.Phase == islandCompleted || s.Phase == islandFailed {
			s.RevealUntil = time.Time{}
			if m.active == key {
				m.active = ""
			}
			if m.manualExpanded() {
				for _, k := range m.queue {
					if q := m.sessions[k]; q != nil {
						q.Deferred = false
					}
				}
				m.queue = nil
			}
		}
		if s.Ask != nil {
			s.Dismissed = true
		}
	}
	m.collapse(now)
}

// markRead is a completed card swiped away: read, and gone from the island, if it is still the card
// the swipe began on.
func (m *islandMachine) markRead(key string, generation uint64, now time.Time) bool {
	s := m.sessions[key]
	if s == nil || s.Generation != generation || s.Phase != islandCompleted {
		return false
	}
	s.Unread = false
	s.RevealUntil, s.CompletedUntil = time.Time{}, time.Time{}
	if m.active == key {
		m.active = ""
	}
	delete(m.sessions, key)
	return true
}

// blocking is a session asking the user, not closed by them, and not held back for the chat on
// screen.
func (m *islandMachine) blocking(s *islandSession, now time.Time) bool {
	if s.Ask == nil || s.Dismissed || s.Muted {
		return false
	}
	return !m.questionHandedToChat(s, now)
}

// questionHandedToChat is a question the user is looking at in the app, past its grace: the chat's
// card has it, and the island leaves it.
func (m *islandMachine) questionHandedToChat(s *islandSession, now time.Time) bool {
	return s.Ask != nil && s.Ask.Kind != islandAskPermission && m.watched == s.ChatID && !s.SuppressAt.IsZero() && !s.SuppressAt.After(now)
}

// rank is a session's place in line, as Cindy's liveTaskPriorityRank: waiting on the user, then a
// result unread, then at work; the result revealed now goes just after what waits.
func (m *islandMachine) rank(s *islandSession, now time.Time) float32 {
	waiting := s.Ask != nil || s.Phase == islandNeedsYou || s.Phase == islandFailed && (s.Unread || s.FailedUntil.After(now))
	unread := s.Phase == islandCompleted && (s.Unread || s.CompletedUntil.After(now))
	rank := float32(3)
	switch {
	case waiting:
		rank = 0
	case unread && !s.Running:
		rank = 1
	case s.Running:
		rank = 2
	}
	if m.active == s.Key && s.RevealUntil.After(now) && rank > 0 {
		rank = 0.5
	}
	return rank
}

// ordered are the sessions the island shows, in line: by rank, then the user's latest words to it,
// then when it started. While the list is open its order holds, a row moving only when its rank
// does.
func (m *islandMachine) ordered(now time.Time) []*islandSession {
	var list []*islandSession
	for _, s := range m.sessions {
		if s.Muted || m.questionHandedToChat(s, now) {
			continue
		}
		list = append(list, s)
	}
	slices.SortStableFunc(list, func(a, b *islandSession) int {
		if ra, rb := m.rank(a, now), m.rank(b, now); ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
		if c := b.SortAt.Compare(a.SortAt); c != 0 {
			return c
		}
		if c := b.StartedAt.Compare(a.StartedAt); c != 0 {
			return c
		}
		return compareStrings(a.Key, b.Key)
	})
	if m.manualExpanded() {
		list = m.frozen(list, now)
	}
	return list
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// frozen keeps the open list's order: rows already in it stay where they were unless their rank
// changed, and new rows go last.
func (m *islandMachine) frozen(list []*islandSession, now time.Time) []*islandSession {
	if m.expandedOrder == nil {
		for _, s := range list {
			m.expandedOrder = append(m.expandedOrder, s.Key)
		}
		return list
	}
	byKey := map[string]*islandSession{}
	for _, s := range list {
		byKey[s.Key] = s
	}
	var out []*islandSession
	for _, key := range m.expandedOrder {
		if s := byKey[key]; s != nil {
			out = append(out, s)
			delete(byKey, key)
		}
	}
	for _, s := range list {
		if byKey[s.Key] != nil {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b *islandSession) int {
		ra, rb := m.rank(a, now), m.rank(b, now)
		switch {
		case ra < rb:
			return -1
		case ra > rb:
			return 1
		}
		return 0
	})
	m.expandedOrder = m.expandedOrder[:0]
	for _, s := range out {
		m.expandedOrder = append(m.expandedOrder, s.Key)
	}
	return out
}

// display is what the island shows now, as Cindy's buildDisplayDecision: the list when the user
// opened it, nothing when nothing runs, a question, a result revealed, else the compact island.
func (m *islandMachine) display(now time.Time) islandDisplay {
	m.tick(now)
	ordered := m.ordered(now)
	var blocking *islandSession
	for _, s := range ordered {
		if m.blocking(s, now) && !m.suppressed(s) {
			blocking = s
			break
		}
	}
	m.promote(now, blocking != nil)
	ordered = m.ordered(now)
	d := islandDisplay{Pill: m.pill(ordered, now)}
	defer func() { m.last = d }()
	switch {
	case m.manualExpanded():
		d.Expanded, d.Surface, d.Sessions = true, islandSurfaceList, ordered
		if len(ordered) > 0 {
			d.Current = ordered[0]
		}
	case len(ordered) == 0:
	case blocking != nil:
		d.Expanded, d.Surface, d.Current, d.Sessions = true, islandSurfaceInteraction, blocking, ordered
		if blocking.RevealUntil.After(now) && m.last.Surface != islandSurfaceInteraction {
			m.autoRevealAt = now
		}
	default:
		if a := m.sessions[m.active]; a != nil && a.RevealUntil.After(now) && !m.suppressed(a) && !a.Muted {
			d.Expanded, d.Surface, d.Current, d.Sessions = true, islandSurfaceCompletion, a, []*islandSession{a}
			break
		}
		d.Current = m.stableCurrent(ordered, now)
		d.Sessions = ordered
	}
	if d.Expanded && !m.last.Expanded {
		m.expandedAt = now
	}
	d.Shadow = m.manualExpanded() || m.pointerInside() || d.Current != nil && d.Current.Ask != nil
	return d
}

// stableCurrent is the session the compact island names: the first in line, unless the one named
// a moment ago and the new first are both at work, when the old one holds a little longer.
func (m *islandMachine) stableCurrent(ordered []*islandSession, now time.Time) *islandSession {
	top := ordered[0]
	if prev := m.sessions[m.compactCurrent]; prev != nil && prev.Key != top.Key && prev.Phase == islandRunning && top.Phase == islandRunning && m.compactCurrentUntil.After(now) {
		return prev
	}
	if top.Key != m.compactCurrent {
		m.compactCurrent = top.Key
		m.compactCurrentUntil = now.Add(islandCompactCurrentDwell)
	}
	return top
}

// pill counts the badge's numbers: at work, waiting on the user, results unread.
func (m *islandMachine) pill(ordered []*islandSession, now time.Time) islandPill {
	p := islandPill{Sessions: len(ordered)}
	for _, s := range ordered {
		attention := s.Ask != nil || s.Phase == islandNeedsYou || s.Unread && (s.Phase == islandCompleted || s.Phase == islandFailed)
		if s.Phase == islandRunning && !attention {
			p.Running++
		}
		if s.Ask != nil {
			p.Pending++
		}
		if s.Unread && (s.Phase == islandCompleted || s.Phase == islandFailed) {
			p.UnreadCompleted++
		}
		if attention {
			p.Attention++
		}
	}
	return p
}

// Activity lines.

func (s *islandSession) appendLine(kind islandLineKind, text string) *islandLine {
	if text == "" {
		return nil
	}
	if n := len(s.Lines); n > 0 && s.Lines[n-1].Kind == kind && kind != islandLineUser && kind != islandLineAssistant {
		s.Lines[n-1].Text = text
		return &s.Lines[n-1]
	}
	s.lineSeq++
	s.Lines = append(s.Lines, islandLine{ID: s.lineSeq, Kind: kind, Text: text})
	if len(s.Lines) > islandActivityLines {
		s.Lines = s.Lines[len(s.Lines)-islandActivityLines:]
	}
	return &s.Lines[len(s.Lines)-1]
}

func (s *islandSession) lastLine() *islandLine {
	if len(s.Lines) == 0 {
		return nil
	}
	return &s.Lines[len(s.Lines)-1]
}

// applyReply puts the reply's words in the line it streams into, or a new one for a new message.
func (s *islandSession) applyReply(id, text string, final bool, now time.Time) {
	text = islandActivityText(text)
	if text == "" {
		return
	}
	if s.streamReplyID == id && s.streamLineID != 0 {
		for i := range s.Lines {
			if s.Lines[i].ID == s.streamLineID {
				s.Lines[i].Text = text
				if s.Preview != nil && s.Preview.ID == s.streamLineID {
					s.Preview.Text = text
				}
				for j := range s.previewQueue {
					if s.previewQueue[j].ID == s.streamLineID {
						s.previewQueue[j].Text = text
					}
				}
				if final {
					s.streamLineID, s.streamReplyID = 0, ""
				}
				s.LastAt = now
				return
			}
		}
	}
	line := s.appendLine(islandLineAssistant, text)
	if line == nil {
		return
	}
	s.enqueuePreview(*line, now)
	if final {
		s.streamLineID, s.streamReplyID = 0, ""
	} else {
		s.streamLineID, s.streamReplyID = line.ID, id
	}
	s.LastAt = now
}

func (s *islandSession) enqueuePreview(line islandLine, now time.Time) {
	if line.Kind != islandLineUser && line.Kind != islandLineAssistant {
		return
	}
	if s.Preview == nil || !s.previewUntil.After(now) {
		s.Preview, s.previewUntil = &line, now.Add(islandPreviewMinDwell)
		return
	}
	s.previewQueue = append(s.previewQueue, line)
}

func (s *islandSession) remainingPreview(now time.Time) time.Duration {
	if s.Preview == nil {
		return 0
	}
	return max(0, s.previewUntil.Sub(now)) + time.Duration(len(s.previewQueue))*islandPreviewMinDwell
}

func (s *islandSession) clearTool() {
	if !s.DetailFromTool {
		return
	}
	s.ToolID, s.ToolDetailUntil = "", time.Time{}
	s.Detail, s.DetailFromTool = "", false
}

// The words a session shows, after Cindy's summary rules.

func (s *islandSession) latest(kinds ...islandLineKind) *islandLine {
	for i := len(s.Lines) - 1; i >= 0; i-- {
		if slices.Contains(kinds, s.Lines[i].Kind) && islandCompactText(s.Lines[i].Text) != "" {
			return &s.Lines[i]
		}
	}
	return nil
}

// compactDetail is the compact island's second line: a preview of what was said, the detail unless
// it is generic, the latest line, else the user's.
func (s *islandSession) compactDetail() string {
	text := ""
	switch {
	case s.Preview != nil && s.Phase != islandNeedsYou && s.Phase != islandFailed:
		text = s.Preview.Text
	case s.Detail != "" && !islandGenericDetail(s.Detail):
		text = s.Detail
	case s.latest(islandLineAssistant, islandLineStatus, islandLineTool) != nil:
		text = s.latest(islandLineAssistant, islandLineStatus, islandLineTool).Text
	case s.latest(islandLineUser) != nil:
		text = s.latest(islandLineUser).Text
	default:
		text = s.Detail
	}
	return islandTruncate(text, islandDetailMax)
}

// subtitle is the compact island's detail line.
func (s *islandSession) subtitle() string { return islandStripPrompt(s.compactDetail()) }

// displayTitle is the session's name, shortened for the compact island.
func (s *islandSession) displayTitle() string {
	title := s.Title
	if title == "" {
		if line := s.latest(islandLineUser); line != nil {
			title = islandTruncate(line.Text, 72)
		}
	}
	if title == "" {
		title = "#" + s.Key[:min(8, len(s.Key))]
	}
	return islandTruncate(title, islandCompactTitleMax)
}

// islandSummary is a row's summary line, and whether it is the user's own words.
type islandSummary struct {
	Text   string
	IsUser bool
}

func (s *islandSession) summary() islandSummary {
	preview := func() *islandSummary {
		if s.Preview == nil {
			return nil
		}
		text := islandStripPrompt(islandCompactText(s.Preview.Text))
		if text == "" {
			return nil
		}
		return &islandSummary{Text: text, IsUser: s.Preview.Kind == islandLineUser}
	}
	switch s.Phase {
	case islandNeedsYou:
		if s.Detail != "" {
			return islandSummary{Text: islandStripPrompt(s.Detail)}
		}
		if s.Ask != nil && s.Ask.Kind == islandAskPermission {
			return islandSummary{Text: L("Awaiting permission")}
		}
		if s.Ask != nil && s.Ask.Kind == islandAskChoices {
			return islandSummary{Text: L("Awaiting your reply")}
		}
		return islandSummary{Text: L("Needs input")}
	case islandCompleted:
		if p := preview(); p != nil {
			return *p
		}
		if line := s.latest(islandLineAssistant); line != nil {
			return islandSummary{Text: islandStripPrompt(islandCompactText(line.Text))}
		}
		if line := s.latest(islandLineStatus, islandLineTool); line != nil {
			if text := islandStripPrompt(islandCompactText(line.Text)); text != L("Done") && text != "Done" {
				return islandSummary{Text: text}
			}
		}
		return islandSummary{Text: L("Completed")}
	case islandFailed:
		if s.Detail != "" {
			return islandSummary{Text: islandStripPrompt(s.Detail)}
		}
		if line := s.latest(islandLineStatus, islandLineAssistant, islandLineTool); line != nil {
			return islandSummary{Text: islandStripPrompt(islandCompactText(line.Text))}
		}
		return islandSummary{Text: L("Error")}
	}
	if p := preview(); p != nil {
		return *p
	}
	detail := islandStripPrompt(s.Detail)
	if detail != "" && !islandGenericDetail(detail) {
		return islandSummary{Text: detail}
	}
	if line := s.latest(islandLineAssistant, islandLineStatus, islandLineTool); line != nil {
		return islandSummary{Text: islandStripPrompt(islandCompactText(line.Text))}
	}
	if line := s.latest(islandLineUser); line != nil {
		return islandSummary{Text: islandStripPrompt(islandCompactText(line.Text)), IsUser: true}
	}
	if detail != "" {
		return islandSummary{Text: detail}
	}
	return islandSummary{Text: L("Running")}
}

// statusLabel is the word at a row's right: Input, Done, Error, or Running.
func (s *islandSession) statusLabel() string {
	switch s.Phase {
	case islandNeedsYou:
		return L("Input")
	case islandCompleted:
		return L("Done")
	case islandFailed:
		return L("Failed")
	}
	return L("Running")
}

// attention is a session the badge counts as waiting on the user or unread.
func (s *islandSession) attention() bool {
	return s.Ask != nil || s.Phase == islandNeedsYou || s.Unread && (s.Phase == islandCompleted || s.Phase == islandFailed)
}

// islandSoundEvent is a sound the island plays, after Cindy's sound events.
type islandSoundEvent int

const (
	islandSoundNone islandSoundEvent = iota
	islandSoundStart
	islandSoundAttention
	islandSoundComplete
	islandSoundError
)

// islandSoundFor is the sound a change of display calls for, as Cindy's
// getAgentIslandSoundEventForTransition: a new question first, then a new error, a new result, a new
// run.
func islandSoundFor(prev, next map[string]islandPhase, attention map[string]bool) islandSoundEvent {
	for key, phase := range next {
		if phase == islandNeedsYou && attention[key] && prev[key] != islandNeedsYou {
			return islandSoundAttention
		}
	}
	for key, phase := range next {
		if phase == islandFailed && attention[key] {
			if p, ok := prev[key]; !ok || p != islandFailed {
				return islandSoundError
			}
		}
	}
	for key, phase := range next {
		if phase == islandCompleted {
			if p, ok := prev[key]; !ok || p != islandCompleted {
				return islandSoundComplete
			}
		}
	}
	for key, phase := range next {
		if phase == islandRunning {
			if p, ok := prev[key]; !ok || p != islandRunning {
				return islandSoundStart
			}
		}
	}
	return islandSoundNone
}

// islandSoundCooldown is how long an event's sound waits before it plays again, as Cindy's.
func islandSoundCooldown(e islandSoundEvent) time.Duration {
	if e == islandSoundStart {
		return 800 * time.Millisecond
	}
	return 1500 * time.Millisecond
}
