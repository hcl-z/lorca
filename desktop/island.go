package main

import (
	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The island is the coding agents' window at the top of the screen: a window of its own, over
// every app, so the main window can be hidden and what an agent asks is still there to answer. It
// is not one of the app's windows (window.go) and not part of the main window, so showing,
// hiding, or closing the main window leaves it alone.
//
// It shows one of three states: an agent or a bot is only at work, an agent asks something, or
// nothing is at work and it is hidden. The first is a folded block that takes no clicks; the
// second unfolds to the width of the decisions a permission question offers.

const (
	// islandTopInset is the gap between the top of the display and the island. It clears the
	// menu bar; the real gap is to be measured on device.
	islandTopInset = 32
	// islandCollapsedWidth and islandCollapsedHeight are the folded size, an agent at work.
	islandCollapsedWidth  = 300
	islandCollapsedHeight = 36
	// islandAskingWidth and islandAskingHeight hold the words and up to three decisions of a
	// permission question.
	islandAskingWidth  = 520
	islandAskingHeight = 48
)

// islandState is what the island shows.
type islandState int

const (
	islandIdle islandState = iota
	islandWorking
	islandAsking
)

func (s islandState) String() string {
	switch s {
	case islandWorking:
		return "working"
	case islandAsking:
		return "asking"
	}
	return "idle"
}

// islandStateOf is what the island shows for the turns at work: a coding agent's, and a bot's. An
// agent that asks or waits for an approval comes before one that is only at work, and a coding
// agent at work comes before a bot's turn. No turn at work hides the island. It is pure, for the
// tests to read the three states without a window.
func islandStateOf(agents []model.RunningAgent, workingBots []string) islandState {
	for _, r := range agents {
		if r.Agent != nil && (r.Agent.Question != nil || r.Agent.Asking != "") {
			return islandAsking
		}
	}
	if len(agents) > 0 {
		return islandWorking
	}
	if len(workingBots) > 0 {
		return islandWorking
	}
	return islandIdle
}

// islandAgentOf is the agent the island speaks for: the first one asking while it asks, else the
// first one at work.
func islandAgentOf(agents []model.RunningAgent, state islandState) *model.RunningAgent {
	for i := range agents {
		r := &agents[i]
		if r.Agent == nil {
			continue
		}
		switch state {
		case islandAsking:
			if r.Agent.Question != nil || r.Agent.Asking != "" {
				return r
			}
		case islandWorking:
			return r
		}
	}
	return nil
}

// island is the window: its native window and the bounds last set.
type island struct {
	win    *mygo.Window
	bounds mygo.Rectangle
}

// newIsland creates the island's window, hidden: sync shows it once an agent has something to
// say. It is created with Content, so it draws native UI rather than a web page, and its window
// is transparent and frameless, so only the block in view shows.
func newIsland() *island {
	i := &island{}
	r := islandBounds(islandCollapsedWidth, islandCollapsedHeight)
	i.bounds = r
	i.win = mygo.NewWindow(mygo.WindowOptions{
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
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		DisableShadow:     true,
		SkipTaskbar:       true,
		Content:           ui.View(i.view),
	})
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

// islandBounds centers an island of width and height on the primary display, under its top edge.
// The first version follows the main screen only.
func islandBounds(width, height int) mygo.Rectangle {
	b := mygo.Screen.PrimaryDisplay().Bounds
	return mygo.Rectangle{
		X:      b.X + (b.Width-width)/2,
		Y:      b.Y + islandTopInset,
		Width:  width,
		Height: height,
	}
}

// islandSize is the window's width and height in each visible state.
func islandSize(state islandState) (width, height int) {
	if state == islandAsking {
		return islandAskingWidth, islandAskingHeight
	}
	return islandCollapsedWidth, islandCollapsedHeight
}

// islandWorkingBots are the bots with a turn running, read from the store the same way the sidebar
// reads its green dots: a chat that is responding, and the bots working in it, in the order they
// started. It touches the store, so islandStateOf stays pure and reads what this returns.
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

// sync shows, hides, sizes, and redraws the island for the agents and bots now at work. The
// store's changes call it on the main thread, where the store lives.
func (i *island) sync() {
	if i == nil || i.win == nil || i.win.IsDestroyed() || store == nil {
		return
	}
	state := islandStateOf(store.RunningAgents(), islandWorkingBots(store))
	if state == islandIdle {
		if i.win.IsVisible() {
			i.win.Hide()
		}
		return
	}
	width, height := islandSize(state)
	if r := islandBounds(width, height); r != i.bounds {
		i.bounds = r
		i.win.SetBounds(r)
	}
	if !i.win.IsVisible() {
		// ShowInactive brings the window to the front without activating the app or taking the
		// keyboard from the field the user is typing in. Show would do both (darwin's
		// makeKeyAndOrderFront: and activateIgnoringOtherApps:).
		i.win.ShowInactive()
	}
	i.invalidate()
}

// invalidate redraws the island, after the turns' state it shows changed. It is the island's own
// redraw: the window is not in windows(), so invalidateWindows leaves it out.
func (i *island) invalidate() {
	if i == nil || i.win == nil || i.win.IsDestroyed() {
		return
	}
	i.win.Invalidate()
}

// view draws the island: a solid block with the state's icon and words, and the decisions of a
// permission question. The window is transparent, so the root is too; only the block paints.
func (i *island) view(c *ui.Context) {
	// The island follows the app's theme as its windows do, so colors(c) has a palette. It has no
	// sheets, so it does not go through appWindow.frame.
	applyTheme(c)
	c.Root().Background(ui.Transparent)

	agents := store.RunningAgents()
	workingBots := islandWorkingBots(store)
	state := islandStateOf(agents, workingBots)
	if state == islandIdle {
		return
	}
	p := colors(c)
	var tint ui.Color
	var text, symbolName string
	r := islandAgentOf(agents, state)
	if r != nil && r.Agent != nil {
		agent := r.Agent
		words := ""
		tint, words = agentLook(p, agent)
		text = agent.Name() + " · " + words
		symbolName = islandSymbol(state, agent)
	} else if len(workingBots) > 0 {
		// A bot's turn is work too: the same icon and tint as a coding agent at work, with the
		// bot's name and that it is working.
		tint = p.Accent
		text = L("%@ is working…", islandBotName(workingBots[0]))
		symbolName = islandSymbol(state, nil)
	} else {
		return
	}

	block := ui.Row(c.Key("island")).Fill().Padding(0, 14).Gap(8).AlignItems(ui.Center).
		Radius(0, 0, 14, 14).Background(p.Popover).Clip()
	if state == islandWorking {
		// The folded island takes no clicks: the pointer goes to what is under it. PassThrough is
		// on this node, which is the one that would take the press; an outer element's would be
		// inherited by neither the children nor this block.
		block.PassThrough()
	}
	block.Children(func() {
		symbol(c, symbolName, 13, 2).TextColor(tint)
		ui.Text(c, text).Grow(1).Shrink(1).MinWidth(0).FontSize(12.5).
			FontWeight(600).SingleLine().TextColor(p.Label)
		if state != islandAsking {
			return
		}
		q := r.Agent.Question
		if q == nil || !q.IsPermission() {
			return
		}
		// The same decisions, in the same order, as the agent's card in the chat (agents.go).
		for n, choice := range q.Decisions() {
			kind := buttonDefault
			if n == 0 {
				kind = buttonPrimary
			}
			if pushButton(c.Key("island-"+choice.Decision), choice.Title, pushOptions{Small: true, Kind: kind}).Clicked() {
				store.AnswerPermission(r.Chat.ID, r.Message.ID, choice.Decision)
			}
		}
	})
}

// islandBotName is the bot's name, as the sidebar's green dot names it, or "The bot" when the
// store no longer holds it.
func islandBotName(botID string) string {
	if bot := store.Bot(botID); bot != nil {
		return bot.Name
	}
	return L("The bot")
}

// islandSymbol is the state's icon: a raised hand for a permission question, a question mark for
// anything else an agent asks, and a spark for work.
func islandSymbol(state islandState, agent *model.AgentRun) string {
	if state != islandAsking {
		return "sparkles"
	}
	if q := agent.Question; q != nil && q.IsPermission() {
		return "hand.raised"
	}
	return "questionmark.circle"
}
