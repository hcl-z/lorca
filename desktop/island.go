package main

import (
	"fmt"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The island is the window at the top of every display that says what the bots and coding agents
// are doing while the main window is out of sight: at work, done, or waiting for the user. It is
// its own window over every app, so hiding the main window leaves it up, and it draws native UI on
// a transparent, frameless window, so only the block shows.
//
// The proportions follow OpenBot's Dynamic Island (packages/ui/src/features/dynamic-island): a
// 268-wide, 32-tall compact capsule that grows a little under the pointer, and a 420-wide panel
// with a 14/20/20 inset for what waits on the user, over a 614×380 transparent canvas so the panel
// never clips. A display with a MacBook notch keeps the notch strip above the panel; a display
// without one, like an external monitor, is the "external" shape with rounded corners alone.
//
// The states, most urgent first: a question waiting for the user, a result from the last few
// seconds, and a bot or coding agent at work, else idle. Idle stays on screen as a small capsule.

const (
	// The canvas: every window is this large and transparent, centered on a display's top edge,
	// so a panel opens inside it instead of resizing the window.
	islandWindowWidth  = 614
	islandWindowHeight = 380
	// The compact capsule, and how much it grows under the pointer.
	islandCompactWidth = 268
	// islandIdleWidth is the idle capsule's width: the mark and a count, as narrow as the idle
	// island OpenBot shrinks to.
	islandIdleWidth         = 136
	islandCompactHeight     = 32
	islandHoverGrowWidth    = 32
	islandHoverGrowHeight   = 8
	islandCompactRadius     = 16
	islandExpandedWidth     = 420
	islandExpandedRadius    = 24
	islandNotchHeight       = 32
	islandPanelTop          = 14
	islandPanelSide         = 20
	islandPanelBottom       = 20
	islandPanelGap          = 10
	islandIdentityHeight    = 20
	islandDescriptionHeight = 32
	islandCommandHeight     = 44
	islandOptionHeight      = 48
	islandOptionGap         = 6
	islandOptionIndex       = 22
	islandActionsHeight     = 32
	islandHitSlop           = 6
	// islandPointerPoll is how often the pointer is read to grow or shrink the capsule and to let
	// clicks through elsewhere. MyGo 0.4.5 cannot forward mouse moves to an ignored window, so the
	// island finds the pointer itself.
	islandPointerPoll = 120 * time.Millisecond
	// islandResultFor is how long a finished turn stays on the island.
	islandResultFor = 8 * time.Second
)

// islandState is what the island shows.
type islandState int

const (
	islandIdle islandState = iota
	islandWorking
	islandResult
	islandAsking
)

func (s islandState) String() string {
	switch s {
	case islandWorking:
		return "working"
	case islandResult:
		return "result"
	case islandAsking:
		return "asking"
	}
	return "idle"
}

// islandAskKind is how a question is answered on the island.
type islandAskKind int

const (
	// islandAskChoices is a bot's `ask_user`: one option per row, a click answers.
	islandAskChoices islandAskKind = iota
	// islandAskPermission is a coding agent's Allow once / Always allow / Deny.
	islandAskPermission
	// islandAskElsewhere is a form the island cannot answer in place: it opens the chat instead.
	islandAskElsewhere
)

// islandAsk is a question waiting for the user, as the island shows it.
type islandAsk struct {
	Kind      islandAskKind
	ChatID    string
	MessageID string
	BotName   string
	Question  string
	// Command is what a permission would run, in monospace under the header.
	Command string
	// Options are a bot's options by their place: the label a row shows, and its one-line
	// description when the bot gave one.
	Options []model.AskOption
	// Decisions are a permission's buttons, as the agent's card orders them.
	Decisions []model.Answer
}

// islandFinished is a turn that just ended: what to say about it, and where it ran.
type islandFinished struct {
	ChatID string
	BotID  string
	Title  string
	Line   string
	At     time.Time
}

// islandOptions is the island's own preferences, read once per frame and by the polling.
type islandOptions struct {
	// CompactWidth is the folded capsule's width: "small", "regular", or "large" in Settings.
	CompactWidth int
	HoverGrow    bool
	IdleVisible  bool
	AllDisplays  bool
	ResultFor    time.Duration
}

// islandOptionsNow reads the island's preferences, with the defaults a test and a first launch get.
func islandOptionsNow() islandOptions {
	p := prefs.get()
	width := islandCompactWidth
	switch p.IslandSize {
	case "small":
		width = 220
	case "large":
		width = 316
	}
	seconds := p.IslandResultSeconds
	if seconds <= 0 {
		seconds = 8
	}
	return islandOptions{
		CompactWidth: width,
		HoverGrow:    p.IslandHoverGrow,
		IdleVisible:  p.IslandIdleVisible,
		AllDisplays:  p.IslandAllDisplays,
		ResultFor:    time.Duration(seconds) * time.Second,
	}
}

// defaultIslandOptions is what the island shows when the preferences are not loaded, as in the
// tests, which draw a state without a store behind it.
func defaultIslandOptions() islandOptions {
	return islandOptions{CompactWidth: islandCompactWidth, HoverGrow: true, IdleVisible: true, AllDisplays: true, ResultFor: islandResultFor}
}

// islandAskOf reads the question the island would show: the first coding agent asking or waiting
// for an approval, else the first bot's `ask_user` still pending. It is pure over the store, so the
// tests read the projection without a window.
func islandAskOf(s *model.Store) *islandAsk {
	for _, r := range s.RunningAgents() {
		agent := r.Agent
		if agent == nil || r.Message == nil || r.Chat == nil {
			continue
		}
		if q := agent.Question; q != nil {
			ask := &islandAsk{
				ChatID:    r.Chat.ID,
				MessageID: r.Message.ID,
				BotName:   agent.Name(),
				Question:  islandQuestionText(q),
			}
			if q.IsPermission() {
				ask.Kind, ask.Decisions = islandAskPermission, q.Decisions()
				ask.Command = model.FirstLine(q.Command)
				if ask.Command == "" {
					ask.Command = model.FirstLine(q.Text)
				}
			} else {
				ask.Kind = islandAskElsewhere
			}
			return ask
		}
		if agent.Asking != "" {
			// The agent asks with a form of its own (agent_ask_card.go), which the chat draws.
			return &islandAsk{ChatID: r.Chat.ID, MessageID: r.Message.ID, BotName: agent.Name(), Kind: islandAskElsewhere}
		}
	}
	for _, chat := range s.Chats {
		for _, message := range chat.Messages {
			request := message.Body.Request
			if message.Body.Kind != model.BodyPermission || request == nil || !request.IsAsk() || !request.IsPending() {
				continue
			}
			ask := &islandAsk{
				Kind:      islandAskChoices,
				ChatID:    chat.ID,
				MessageID: message.ID,
				BotName:   islandAuthorName(message),
				Question:  request.Ask.Question,
				Options:   request.Ask.Options,
			}
			return ask
		}
	}
	return nil
}

// islandQuestionText is a coding agent's question in one line: the command it would run, or what it
// asks.
func islandQuestionText(q *model.AgentQuestion) string {
	if q.Command != "" {
		return "$ " + model.FirstLine(q.Command)
	}
	return q.Text
}

// islandAuthorName names the bot that wrote a message, for the island's header.
func islandAuthorName(message *model.Message) string {
	if bot := store.Bot(message.Author.BotID); bot != nil {
		return bot.Name
	}
	return L("The bot")
}

// islandStateOf is what the island shows: a question first, else a fresh result, else work, else
// idle. It is pure, for the tests.
func islandStateOf(ask *islandAsk, result *islandFinished, agents []model.RunningAgent, workingBots []string, now time.Time, resultFor time.Duration) islandState {
	if ask != nil {
		return islandAsking
	}
	if result != nil && now.Sub(result.At) < resultFor {
		return islandResult
	}
	if len(agents) > 0 || len(workingBots) > 0 {
		return islandWorking
	}
	return islandIdle
}

// islandBlockSize is the painted block, as OpenBot's island sizes it: a compact capsule that grows
// under the pointer, and a panel whose height follows what it holds.
func islandBlockSize(state islandState, ask *islandAsk, hovered bool, o islandOptions) (width, height int) {
	switch state {
	case islandAsking:
		return islandExpandedWidth, islandPanelHeight(ask)
	case islandResult:
		return islandExpandedWidth, islandPanelTop + islandIdentityHeight + 6 + 16 + islandPanelBottom
	}
	width, height = o.CompactWidth, islandCompactHeight
	if state == islandIdle {
		width = islandIdleWidth
	}
	if hovered && o.HoverGrow {
		width += islandHoverGrowWidth
		height += islandHoverGrowHeight
	}
	return width, height
}

// islandPanelHeight is the expanded panel's height, from the same numbers its view draws with.
func islandPanelHeight(ask *islandAsk) int {
	h := islandPanelTop + islandIdentityHeight + islandPanelGap
	rows := 0
	switch {
	case ask == nil:
	case ask.Kind == islandAskPermission:
		if ask.Command != "" {
			h += islandCommandHeight + islandPanelGap
		}
	case ask.Kind == islandAskChoices:
		if ask.Question != "" {
			h += islandDescriptionHeight + 4
		}
		rows = len(ask.Options)
	default:
		h += islandDescriptionHeight + 4
		rows = 1
	}
	if rows > 0 {
		h += rows*islandOptionHeight + (rows-1)*islandOptionGap
	}
	return h + islandPanelGap + islandActionsHeight + islandPanelBottom
}

// islandWindow is one display's window: the native window, its display, and whether the pointer is
// over the block, which grows the compact capsule and lets the click through elsewhere.
type islandWindow struct {
	win     *mygo.Window
	display mygo.Display
	hovered bool
}

// island holds the island of every display and the result it is showing.
type island struct {
	wins   map[int64]*islandWindow
	result *islandFinished
	expiry *time.Timer
}

// newIsland creates the island's windows and follows the displays: a HUD on one screen of two says
// nothing to a user looking at the other.
func newIsland() *island {
	i := &island{wins: map[int64]*islandWindow{}}
	i.rebuild()
	mygo.Screen.OnDisplaysChanged(func() { post(i.rebuild) })
	go func() {
		for range time.Tick(islandPointerPoll) {
			post(i.pollPointer)
		}
	}()
	// The island is a visible window of the app, so AppKit reports one to a Dock click even when
	// the main window is hidden. Bring the main window back as a click on the Dock icon does
	// without the island.
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if hasVisibleWindows && app.main != nil && app.main.win != nil && !app.main.win.IsVisible() {
			post(app.reopen)
		}
	})
	return i
}

// islandDisplays are the displays that get a window: every one, or the main one alone.
func islandDisplays(o islandOptions) []mygo.Display {
	all := mygo.Screen.Displays()
	if o.AllDisplays || len(all) < 2 {
		return all
	}
	for _, d := range all {
		if d.Primary {
			return []mygo.Display{d}
		}
	}
	return all[:1]
}

// rebuild puts a canvas window on each display it should be on, and closes the rest.
func (i *island) rebuild() {
	if i == nil || store == nil || !islandSupported {
		return
	}
	i.buildWindows(islandDisplays(islandOptionsNow()))
	i.sync()
}

// buildWindows creates the missing windows and closes those of displays left out.
func (i *island) buildWindows(displays []mygo.Display) {
	seen := map[int64]bool{}
	for _, d := range displays {
		seen[d.ID] = true
		if _, ok := i.wins[d.ID]; ok {
			continue
		}
		r := islandWindowBounds(d)
		id := d.ID
		win := &islandWindow{display: d}
		win.win = mygo.NewWindow(mygo.WindowOptions{
			Title:             appName() + " Island",
			X:                 r.X,
			Y:                 r.Y,
			Width:             r.Width,
			Height:            r.Height,
			Frameless:         true,
			Transparent:       true,
			AlwaysOnTop:       true,
			Hidden:            true,
			DisableResize:     true,
			DisableMove:       true,
			DisableMinimize:   true,
			DisableMaximize:   true,
			DisableFullScreen: true,
			DisableShadow:     true,
			SkipTaskbar:       true,
			Content:           ui.View(func(c *ui.Context) { i.view(c, id) }),
		})
		polishIslandWindow(win.win)
		win.win.SetBounds(r)
		// Until the pointer is over the block, clicks go to what is under it.
		win.win.SetIgnoreMouseEvents(true)
		i.wins[id] = win
	}
	for id, win := range i.wins {
		if !seen[id] {
			win.win.Close()
			delete(i.wins, id)
		}
	}
}

// missingWindows is whether the windows on screen are not the ones the preferences ask for.
func (i *island) missingWindows(displays []mygo.Display) bool {
	if len(i.wins) != len(displays) {
		return true
	}
	for _, d := range displays {
		if _, ok := i.wins[d.ID]; !ok {
			return true
		}
	}
	return false
}

// islandWindowBounds is the transparent canvas on a display's top edge, centered: OpenBot's
// 614×380 window, which holds the compact capsule and the panel without resizing.
func islandWindowBounds(d mygo.Display) mygo.Rectangle {
	return mygo.Rectangle{
		X:      d.Bounds.X + (d.Bounds.Width-islandWindowWidth)/2,
		Y:      d.Bounds.Y,
		Width:  islandWindowWidth,
		Height: islandWindowHeight,
	}
}

// sync shows the island in the state the store is in and hides it where the user turned it off,
// or where the idle capsule is not wanted.
func (i *island) sync() {
	if i == nil || store == nil || len(i.wins) == 0 || !islandSupported {
		return
	}
	o := islandOptionsNow()
	displays := islandDisplays(o)
	if i.missingWindows(displays) {
		i.buildWindows(displays)
	}
	state, _ := islandNow(o)
	visible := prefs.get().ShowsIsland && (state != islandIdle || o.IdleVisible)
	for _, win := range i.wins {
		win.win.Invalidate()
	}
	i.show(visible)
}

// pollPointer reads the pointer against each display's block: over it, the compact capsule grows
// and the window takes clicks so an option can be answered; away from it, clicks pass through.
func (i *island) pollPointer() {
	if i == nil || store == nil || !prefs.get().ShowsIsland {
		return
	}
	o := islandOptionsNow()
	state, ask := islandNow(o)
	if state == islandIdle && !o.IdleVisible {
		return
	}
	p := mygo.Screen.CursorScreenPoint()
	for _, win := range i.wins {
		if win.win.IsDestroyed() || !win.win.IsVisible() {
			continue
		}
		width, height := islandBlockSize(state, ask, false, o)
		if win.hovered {
			width, height = islandBlockSize(state, ask, true, o)
		}
		// The block is centered on the display, inside a canvas that is centered too.
		x := win.display.Bounds.X + (win.display.Bounds.Width-width)/2
		y := win.display.Bounds.Y
		inside := p.X >= x && p.X < x+width && p.Y >= y && p.Y < y+height+islandHitSlop
		if inside == win.hovered {
			continue
		}
		win.hovered = inside
		win.win.SetIgnoreMouseEvents(!inside)
		win.win.Invalidate()
	}
}

// show raises or hides every display's island. ShowInactive brings the window to the front without
// activating the app or taking the keyboard from the field the user is typing in; Show would do
// both.
func (i *island) show(visible bool) {
	for _, win := range i.wins {
		if win.win.IsDestroyed() {
			continue
		}
		if !visible {
			if win.win.IsVisible() {
				win.win.Hide()
			}
			continue
		}
		if !win.win.IsVisible() {
			win.win.ShowInactive()
			revealIslandWindow(win.win)
		}
	}
}

// invalidate redraws every display's island. It is the island's own redraw: its windows are not in
// windows(), so invalidateWindows leaves them out.
func (i *island) invalidate() {
	if i == nil {
		return
	}
	for _, win := range i.wins {
		if !win.win.IsDestroyed() {
			win.win.Invalidate()
		}
	}
}

// islandNow is the state of the store the island shows, and the question if one waits.
func islandNow(o islandOptions) (islandState, *islandAsk) {
	if store == nil {
		return islandIdle, nil
	}
	ask := islandAskOf(store)
	var result *islandFinished
	if app.island != nil {
		result = app.island.resultNow(o.ResultFor)
	}
	state := islandStateOf(ask, result, store.RunningAgents(), islandWorkingBots(store), time.Now(), o.ResultFor)
	return state, ask
}

// resultNow is the result still fresh, or nil.
func (i *island) resultNow(resultFor time.Duration) *islandFinished {
	if i == nil || i.result == nil {
		return nil
	}
	if time.Since(i.result.At) >= resultFor {
		return nil
	}
	return i.result
}

// turnFinished remembers a turn that just ended, so the island can say how it went. A turn with
// nothing to say keeps the last result.
func (i *island) turnFinished(event model.Event) {
	if i == nil || store == nil {
		return
	}
	chat := store.Chat(event.ChatID)
	if chat == nil {
		return
	}
	line := islandResultLine(chat, event.BotID)
	if line == "" {
		return
	}
	i.result = &islandFinished{ChatID: chat.ID, BotID: event.BotID, Title: islandBotName(event.BotID), Line: line, At: time.Now()}
	if i.expiry != nil {
		i.expiry.Stop()
	}
	i.expiry = time.AfterFunc(islandOptionsNow().ResultFor, func() { post(i.sync) })
}

// islandResultLine is the last thing a bot said in a chat, on one line.
func islandResultLine(chat *model.Chat, botID string) string {
	for n := len(chat.Messages) - 1; n >= 0; n-- {
		message := chat.Messages[n]
		if message.Author.Kind != model.AuthorBot || message.Body.Kind != model.BodyText {
			continue
		}
		if botID != "" && message.Author.BotID != botID {
			continue
		}
		if line := model.FirstLine(message.Body.Text); line != "" {
			return line
		}
	}
	return ""
}

// view draws one display's island: the block alone, since the window and the root are transparent.
func (i *island) view(c *ui.Context, displayID int64) {
	applyTheme(c)
	c.Root().Background(ui.Transparent)
	if store == nil {
		return
	}
	o := islandOptionsNow()
	state, ask := islandNow(o)
	hovered := false
	if win := i.wins[displayID]; win != nil && o.HoverGrow {
		hovered = win.hovered && state != islandAsking
	}
	i.draw(c, state, ask, hovered, o)
}

// draw paints one state of the island at the size it would have over a display. The tests draw a
// state without a store behind it.
func (i *island) draw(c *ui.Context, state islandState, ask *islandAsk, hovered bool, o islandOptions) {
	c.Root().Background(ui.Transparent)
	width, height := islandBlockSize(state, ask, hovered, o)
	radius := islandCompactRadius
	shadow := ui.RGBA(0, 0, 0, 0.26)
	if state == islandAsking || state == islandResult {
		radius = islandExpandedRadius
		shadow = ui.RGBA(0, 0, 0, 0.38)
	}
	// A state change morphs the block, as OpenBot's island springs between its sizes.
	morph := ui.Animation{Duration: 420 * time.Millisecond, Ease: ui.EaseOut}
	ui.Column(c).Fill().AlignItems(ui.Center).Children(func() {
		block := ui.Row(c.Key("island"))
		mw := block.AnimateTo("w", float32(width), morph)
		mh := block.AnimateTo("h", float32(height), morph)
		mr := block.AnimateTo("r", float32(radius), morph)
		block.Width(mw).Height(mh).Radius(mr).
			Background(islandBackground()).Clip().
			Shadow(0, 6, 20, 0, shadow)
		block.Children(func() {
			switch state {
			case islandAsking:
				i.askPanel(c, ask)
			case islandResult:
				i.resultPanel(c)
			case islandWorking:
				i.compactBody(c, islandWorking)
			default:
				i.compactBody(c, islandIdle)
			}
		})
		if state == islandResult && block.Clicked() {
			if result := i.resultNow(o.ResultFor); result != nil {
				app.openChat(result.ChatID)
			}
		}
	})
}

// islandBackground is the block's fill: the near-black of a notch in both appearances, so the words
// on it keep one contrast.
func islandBackground() ui.Color { return ui.RGBA(12, 12, 14, 0.97) }

var (
	islandInk    = ui.RGBA(255, 255, 255, 0.96)
	islandDim    = ui.RGBA(255, 255, 255, 0.60)
	islandFaint  = ui.RGBA(255, 255, 255, 0.14)
	islandGreen  = ui.Hex("#30d158")
	islandBlue   = ui.Hex("#0a84ff")
	islandWarn   = ui.Hex("#ff9f0a")
	islandRowBG  = ui.RGBA(255, 255, 255, 0.08)
	islandRowHov = ui.RGBA(255, 255, 255, 0.16)
)

// compactBody is the folded capsule: an avatar or the app's mark, a name, and a trailing badge or
// greeting, as OpenBot's compact island.
func (i *island) compactBody(c *ui.Context, state islandState) {
	ui.Row(c).Fill().Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		switch state {
		case islandWorking:
			agents := store.RunningAgents()
			if len(agents) > 0 && agents[0].Agent != nil {
				islandAvatar(c, agents[0].Agent.Name())
				ui.Text(c, agents[0].Agent.Name()).FontSize(12.5).FontWeight(600).TextColor(islandInk).
					Shrink(1).MinWidth(0).SingleLine()
			} else if bots := islandWorkingBots(store); len(bots) > 0 {
				name := islandBotName(bots[0])
				islandAvatar(c, name)
				ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(islandInk).
					Shrink(1).MinWidth(0).SingleLine()
			}
			ui.Spacer(c)
			ui.Row(c).Padding(3, 7).Radius(8).Gap(4).AlignItems(ui.Center).
				Background(ui.RGBA(48, 209, 88, 0.16)).Shrink(0).Children(func() {
				ui.Spinner(c).Width(10).Height(10).TextColor(islandGreen)
				ui.Text(c, L("Working")).FontSize(10.5).FontWeight(600).TextColor(islandGreen).SingleLine()
			})
		default:
			// The app's mark, as OpenBot shows its logo beside the idle greeting.
			ui.Row(c).Width(20).Height(20).Radius(6).Background(islandInk).Center().Shrink(0).Children(func() {
				symbol(c, "sparkles", 12, 2).TextColor(islandBackground())
			})
			ui.Spacer(c)
			if n := unreadTotal(); n > 0 {
				ui.Row(c).MinWidth(18).Height(18).Padding(0, 5).Radius(9).Background(islandFaint).
					Center().Shrink(0).Children(func() {
					ui.Text(c, fmt.Sprint(n)).FontSize(10.5).FontWeight(600).TextColor(islandInk).
						FontFeatures("tnum").SingleLine()
				})
			} else {
				ui.Text(c, "✨").FontSize(12)
			}
		}
	})
}

// islandAvatar is the 20-point avatar the compact island shows, with the name's initial until the
// app has the bot's image here.
func islandAvatar(c *ui.Context, name string) {
	ui.Avatar(c, name, nil).Width(20).Height(20).Shrink(0)
}

// resultPanel is a finished turn: the agent or bot, that it is done, and the last line it said.
func (i *island) resultPanel(c *ui.Context) {
	result := i.resultNow(islandOptionsNow().ResultFor)
	if result == nil {
		return
	}
	ui.Column(c).Fill().Padding(islandPanelTop, islandPanelSide, islandPanelBottom, islandPanelSide).
		Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(islandPanelGap).AlignItems(ui.Center).Children(func() {
			islandAvatar(c, result.Title)
			ui.Text(c, result.Title).FontSize(12.5).FontWeight(600).TextColor(islandInk).Shrink(1).MinWidth(0).SingleLine()
			i.chip(c, L("Done"), islandGreen, ui.RGBA(48, 209, 88, 0.16))
			ui.Spacer(c)
			ui.Text(c, L("just now")).FontSize(11.5).TextColor(islandDim).Shrink(0)
		})
		ui.Text(c, result.Line).FontSize(12).TextColor(islandDim).SingleLine().Shrink(1).MinWidth(0)
	})
}

// askPanel is the expanded panel: OpenBot's identity header, then the question or the command, then
// one row per option or decision, then the actions.
func (i *island) askPanel(c *ui.Context, ask *islandAsk) {
	if ask == nil {
		return
	}
	ui.Column(c).Fill().
		Padding(islandPanelTop, islandPanelSide, islandPanelBottom, islandPanelSide).
		Gap(islandPanelGap).Children(func() {
		i.identity(c, ask)
		switch ask.Kind {
		case islandAskPermission:
			if ask.Command != "" {
				i.commandBlock(c, ask.Command)
			}
		case islandAskChoices:
			for n, option := range ask.Options {
				place := n
				i.optionRow(c, fmt.Sprint("ask-", n), fmt.Sprint(n+1), option.Label, option.Description, func() {
					store.AnswerAsk(ask.ChatID, ask.MessageID, []int{place}, "", func(error) { i.sync() })
				})
			}
		default:
			i.optionRow(c, "answer-elsewhere", "", L("Answer in Lorca"), "", func() { app.openChat(ask.ChatID) })
		}
		ui.Row(c).FillWidth().Height(islandActionsHeight).AlignItems(ui.Center).Children(func() {
			if more := islandPendingCount(store) - 1; more > 0 {
				ui.Text(c, L("%d more", more)).FontSize(11).TextColor(islandDim).Shrink(0)
			}
			ui.Spacer(c)
			if ask.Kind == islandAskPermission {
				// The decisions, as the agent's card orders them, with Allow once in ink as the card
				// puts its first choice.
				for _, decision := range ask.Decisions {
					d := decision
					i.action(c, "decision-"+d.Decision, d.Title, d.Decision == "allow", func() {
						store.AnswerPermission(ask.ChatID, ask.MessageID, d.Decision)
					})
				}
				return
			}
			i.action(c, "close", L("Close"), false, func() { app.openChat(ask.ChatID) })
		})
	})
}

// identity is the header: the bot's avatar and name, a chip for what it needs, and the question
// under them, as OpenBot's identity row.
func (i *island) identity(c *ui.Context, ask *islandAsk) {
	status, statusInk, statusBG := L("asks"), ui.Hex("#6cb8ff"), ui.RGBA(10, 132, 255, 0.18)
	switch ask.Kind {
	case islandAskPermission:
		status, statusInk, statusBG = L("needs approval"), islandWarn, ui.RGBA(255, 159, 10, 0.16)
	case islandAskElsewhere:
		status, statusInk, statusBG = L("needs you"), ui.Hex("#bf8cff"), ui.RGBA(191, 140, 255, 0.16)
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		islandAvatar(c, ask.BotName)
		ui.Text(c, ask.BotName).FontSize(12.5).FontWeight(600).TextColor(islandInk).Shrink(1).MinWidth(0).SingleLine()
		i.chip(c, status, statusInk, statusBG)
	})
	if ask.Kind != islandAskPermission && ask.Question != "" {
		ui.Text(c, ask.Question).FontSize(12).TextColor(islandDim).MaxLines(2).Shrink(1).MinWidth(0)
	}
}

// chip is a small tinted word in a header or a compact badge.
func (i *island) chip(c *ui.Context, text string, ink, fill ui.Color) {
	ui.Row(c).Padding(2, 7).Radius(7).Background(fill).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(10.5).FontWeight(600).TextColor(ink).SingleLine()
	})
}

// commandBlock is the command a permission would run, in monospace behind a warning bar.
func (i *island) commandBlock(c *ui.Context, command string) {
	ui.Row(c).FillWidth().Height(islandCommandHeight).Padding(2, 10).
		Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Width(2).Height(islandCommandHeight - 8).Radius(1).Background(islandWarn).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, L("Command")).FontSize(11).TextColor(islandDim).SingleLine()
			ui.Text(c, command).FontSize(12).TextColor(islandInk).SingleLine().Shrink(1).MinWidth(0)
		})
	})
}

// optionRow is one answer of the panel: a number or nothing, the words, and their description, as
// OpenBot's question rows draw them.
func (i *island) optionRow(c *ui.Context, key, index, label, description string, answer func()) {
	row := ui.ButtonBase(c.Key(key)).FillWidth().Height(islandOptionHeight).Padding(7, 10).
		Radius(12).Background(islandRowBG).AlignItems(ui.Center).Gap(8).
		Label(label).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(islandRowHov)
	}
	row.Children(func() {
		if index != "" {
			ui.Row(c).Width(islandOptionIndex).Height(islandOptionIndex).Radius(6).
				Background(ui.RGBA(10, 132, 255, 0.18)).Center().Children(func() {
				ui.Text(c, index).FontSize(12).FontWeight(600).TextColor(ui.Hex("#6cb8ff")).FontFeatures("tnum")
			})
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(islandInk).SingleLine().Shrink(1).MinWidth(0)
			if description != "" {
				ui.Text(c, description).FontSize(12).TextColor(islandDim).SingleLine().Shrink(1).MinWidth(0)
			}
		})
	})
	if row.Clicked() {
		answer()
		i.sync()
	}
}

// action is a small button on the panel's last row: ink for the choice a click is expected to
// take, a faint fill under the pointer for the others.
func (i *island) action(c *ui.Context, key, label string, primary bool, run func()) {
	button := ui.ButtonBase(c.Key("action-"+key)).Height(26).Padding(0, 12).
		Radius(8).AlignItems(ui.Center).Label(label).Cursor(ui.CursorPointer)
	ink := islandDim
	switch {
	case primary:
		button.Background(islandInk)
		ink = ui.RGBA(12, 12, 14, 1)
	case button.Hovered():
		button.Background(islandRowBG)
		ink = islandInk
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(ink).SingleLine()
	})
	if button.Clicked() {
		run()
	}
}

// islandPendingCount is how many questions wait on the user across the store: pending `ask_user`
// cards and coding agents asking or waiting for an approval. The panel shows the rest after the one
// it draws.
func islandPendingCount(s *model.Store) int {
	count := 0
	for _, r := range s.RunningAgents() {
		if r.Agent != nil && (r.Agent.Question != nil || r.Agent.Asking != "") {
			count++
		}
	}
	for _, chat := range s.Chats {
		for _, message := range chat.Messages {
			request := message.Body.Request
			if message.Body.Kind == model.BodyPermission && request != nil && request.IsPending() &&
				(request.IsAsk() || request.IsAgentAsk()) {
				count++
			}
		}
	}
	return count
}

// islandWorkingBots are the bots with a turn running, read from the store the same way the sidebar
// reads its green dots: a chat that is responding, and the bots working in it.
func islandWorkingBots(s *model.Store) []string {
	var out []string
	for _, chat := range s.Chats {
		if !s.IsResponding(chat.ID) {
			continue
		}
		out = append(out, s.WorkingBots(chat.ID)...)
	}
	return out
}

// islandBotName is the bot's name, as the sidebar's green dot names it, or "The bot" when the store
// no longer holds it.
func islandBotName(botID string) string {
	if bot := store.Bot(botID); bot != nil {
		return bot.Name
	}
	return L("The bot")
}
