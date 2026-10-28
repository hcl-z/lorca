package main

import (
	"fmt"
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A coding agent at work has a window of its own that floats over the app, as a picture in
// picture does: its card, smaller, with how it stands, its last lines, Stop, and what it asks. It
// comes up when the agent starts working, goes where the user drags it by its title, stays over
// whatever chat is on screen, and goes away when the agent is done. A click on its body fills the
// app window with the agent: its whole transcript, Stop, and its questions.

const (
	pipWidth     = 340
	pipMargin    = 16
	pipEdge      = 8 // the least room it keeps from the window's edges
	pipStagger   = 28
	pipGuess     = 260 // its height before it was drawn, for where it first sits
	pipLines     = 5
	pipBarHeight = 38
)

// pipState is one agent's window.
type pipState struct {
	x, y   float32
	height float32 // as it was last drawn, to keep it inside the window
	placed bool
	// closed keeps it away until the agent works again; its card in the chat stays.
	closed bool
	full   bool
	// view follows the agent's transcript while it fills the window.
	view *agentTranscript
}

func (s *pipState) release() {
	if s.view != nil {
		s.view.close()
		s.view = nil
	}
}

// agentPips are the windows of the agents at work, by the id of the row of each one's card.
type agentPips struct {
	byID map[string]*pipState
	// off leaves them out, as the tests that draw a chat do.
	off bool
}

func (a *agentPips) state(id string) *pipState {
	if a.byID == nil {
		a.byID = map[string]*pipState{}
	}
	st := a.byID[id]
	if st == nil {
		st = &pipState{}
		a.byID[id] = st
	}
	return st
}

// agentWindows draws the floating windows of the agents at work, or the one that fills the window.
func (m *mainWindow) agentWindows(c *ui.Context) {
	pips := &m.pips
	var running []model.RunningAgent
	if !pips.off {
		running = store.RunningAgents()
	}
	alive := map[string]bool{}
	var full *model.RunningAgent
	for i := range running {
		id := running[i].Message.ID
		alive[id] = true
		if st := pips.state(id); st.full && !st.closed && full == nil {
			full = &running[i]
		}
	}
	// An agent that is no longer at work gives its window up, and a new run starts afresh.
	for id, st := range pips.byID {
		if !alive[id] {
			st.release()
			delete(pips.byID, id)
		}
	}
	if full != nil {
		m.agentFull(c, *full, pips.state(full.Message.ID))
		return
	}
	shown := 0
	for _, r := range running {
		st := pips.state(r.Message.ID)
		st.release()
		if st.closed {
			continue
		}
		m.agentWindow(c, r, st, shown)
		shown++
	}
}

// agentAskMessage is the card of the question an agent asks in the chat, with its chat, when it
// has loaded.
func agentAskMessage(chatID, cardID string) (*model.Chat, *model.Message) {
	chat := store.Chat(chatID)
	if chat == nil || cardID == "" {
		return nil, nil
	}
	for _, message := range chat.Messages {
		if message.ID == cardID && message.Body.Kind == model.BodyPermission && message.Body.Request.IsAgentAsk() {
			return chat, message
		}
	}
	return nil, nil
}

// lastLines is the last `count` lines of `text`.
func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, "\n")
}

// agentWindow is one floating window: a title to drag it by, with the agent's tile, name, and pill,
// Full Screen, Stop, and Close; then what it works on and does now on its console, which open it
// full screen; what it asks; and along its foot, a hairline of how full its context is.
func (m *mainWindow) agentWindow(c *ui.Context, r model.RunningAgent, st *pipState, index int) {
	p := colors(c)
	agent, id := r.Agent, r.Message.ID
	width, height := c.Size()
	if !st.placed {
		// The first one sits in the corner; the next ones a little off it, so each shows.
		st.placed = true
		st.x = width - pipWidth - pipMargin - float32(index)*pipStagger
		st.y = height - pipGuess - pipMargin - float32(index)*pipStagger
	}
	// It stays whole inside the window, as tall as it was last drawn, and as it grows it moves up
	// rather than out of the window.
	st.x = clamp(st.x, pipEdge, max(pipEdge, width-pipWidth-pipEdge))
	own := st.height
	if own == 0 {
		own = pipGuess
	}
	maxY := max(height-own-pipEdge, pipEdge)
	st.y = clamp(st.y, min(headerHeight, maxY), maxY)
	ui.Overlay(c, func() {
		panel := ui.Column(c.Key("pip:"+id)).Absolute().Left(st.x).Top(st.y).Width(pipWidth).MaxHeight(height-2*pipEdge).Radius(radiusXL).Clip().
			Background(p.Popover).Border(0.5, p.ShadowEdge).Shadow(0, 14, 44, -6, p.Shadow)
		if h := panel.LayoutBounds().H; h > 0 {
			st.height = h
		}
		asking, run := m.agentAsker(panel, agent)
		panel.Children(func() {
			ui.Row(c).Height(pipBarHeight).AlignItems(ui.Center).Padding(0, 6, 0, 10).Gap(2).Children(func() {
				// The title is the handle: a press on it and a drag move the window.
				handle := ui.Row(c.Key("pip-handle")).Grow(1).Shrink(1).MinWidth(0).Height(pipBarHeight).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorGrab).Label(L("Move"))
				if dx, dy, held := handle.Dragged(); held {
					st.x, st.y = st.x+dx, st.y+dy
					handle.Cursor(ui.CursorGrabbing)
				}
				handle.Children(func() {
					agentTile(c, p, agent.Kind, 22)
					ui.Text(c, agent.Name()).Shrink(1).MinWidth(0).FontSize(12.5).FontWeight(600).FixedLineHeight(17).SingleLine()
					agentPill(c, p, agent)
					ui.Box(c).Grow(1)
				})
				if hoverButton(c, hoverButtonOptions{Symbol: "arrow.up.left.and.arrow.down.right", Size: 13, Tooltip: L("Full Screen")}).Clicked() {
					st.full = true
				}
				if hoverButton(c, hoverButtonOptions{Symbol: "xmark", Size: 13, Tooltip: L("Close")}).Clicked() {
					st.closed = true
				}
			})
			// Its task and console; a click fills the window with it.
			body := ui.ButtonBase(c.Key("pip-body")).Column().AlignItems(ui.Stretch).FillWidth().Padding(2, 12, 12, 12).Cursor(ui.CursorPointer).Label(L("Full Screen")).TextColor(p.Label)
			body.Children(func() {
				if agent.Task != "" {
					ui.Text(c, agent.Task).FontSize(12).LineHeight(1.4).MaxLines(2).Tooltip(agent.Task)
				}
				if text := agentOutput(agent); text != "" {
					consoleTail(c, text, pipLines, agentBusy(agent)).Margin(9, 0, 0, 0)
				}
				ui.Row(c).FillWidth().Gap(8).Margin(9, 0, 0, 0).AlignItems(ui.Center).Children(func() {
					short, full := pipSettings(agent)
					if short != "" {
						ui.Text(c, short).Shrink(1).MinWidth(0).FontSize(11).TextColor(p.Label3).SingleLine().Tooltip(full)
					}
					ui.Box(c).Grow(1)
					if agent.IsRunning() && stopButton(c, p, asking.busy).Clicked() {
						run(func(done func(error)) { store.StopAgent(r.Chat.ID, id, done) }, L("Could not stop %@", agent.Name()))
					}
				})
			})
			if body.Clicked() {
				st.full = true
			}
			if agent.Question != nil || agent.Asking != "" {
				ui.Column(c).FillWidth().Padding(0, 12, 12, 12).Margin(-12, 0, 0, 0).Children(func() {
					m.agentAsk(c, p, agent, r.Chat.ID, id, botName(r.Message), asking, run)
					agentEnding(c, p, agent, r.Chat.ID)
				})
			}
			// How full its context is, as a hairline along its foot.
			if share, text, ok := agentContext(agent); ok {
				ui.Box(c.Key("pip-context")).FillWidth().Height(2).Background(p.Label4).Tooltip(L("Context: %@", text)).Children(func() {
					tint := p.Accent
					if share > 0.9 {
						tint = p.Red
					}
					ui.Box(c).Height(2).WidthPercent(max(share*100, 2)).Background(tint)
				})
			}
		})
	})
}

// pipSettings is what the small window says of the session in one line: the model's short name, how hard it thinks, and the context in use; and
// all of it for the tooltip.
func pipSettings(agent *model.AgentRun) (short, full string) {
	full = agent.SettingsLine()
	var parts []string
	for _, setting := range agent.Settings {
		switch setting.Name {
		case "Model":
			if agent.Model != "" {
				parts = append(parts, agent.Model)
			} else {
				parts = append(parts, setting.Value)
			}
		case "Effort", "Context":
			parts = append(parts, setting.Value)
		}
	}
	if len(parts) == 0 {
		return full, full
	}
	return strings.Join(parts, " · "), full
}

// agentFull fills the window with one agent: a header with its tile, its name and the title of its
// session, its pill, Stop, and the way back; its transcript in a column of reading width, with what
// it asks under it; and beside it, what it works on, where, its session, its use, and its plan.
// Escape and the button in its corner return it to its floating window.
func (m *mainWindow) agentFull(c *ui.Context, r model.RunningAgent, st *pipState) {
	p := colors(c)
	agent, id := r.Agent, r.Message.ID
	if st.view == nil {
		st.view = newAgentTranscript(r.Chat.ID, r.Message)
	}
	view := st.view
	view.maxWidth = 760
	view.fill = p.Content
	ui.Overlay(c, func() {
		root := ui.Column(c.Key("pip-full:" + id)).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(p.Content).Modal()
		asking, run := m.agentAsker(root, agent)
		if root.OverlayShortcut(0, ui.KeyEscape) {
			st.full = false
		}
		root.Children(func() {
			bar := c.TitleBar()
			ui.Row(c).Height(headerHeight+8).Gap(10).Padding(0, max(16, bar.Right+12), 0, max(20, bar.Left+12)).AlignItems(ui.Center).DragWindow().Children(func() {
				agentTile(c, p, agent.Kind, 28)
				ui.Column(c).Shrink(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, agent.Name()).FontSize(14.5).FontWeight(600).FixedLineHeight(19).SingleLine()
					if sub := firstNonEmpty(agent.Title, agent.Task); sub != "" {
						ui.Text(c, sub).Shrink(1).MinWidth(0).FontSize(11.5).TextColor(p.Label2).SingleLine().Tooltip(sub)
					}
				})
				agentPill(c, p, agent)
				ui.Box(c).Grow(1)
				if agent.IsRunning() {
					if pushButton(c.Key("full-stop"), L("Stop"), pushOptions{Small: true, Symbol: "stop.fill", Disabled: asking.busy}).Clicked() {
						run(func(done func(error)) { store.StopAgent(r.Chat.ID, id, done) }, L("Could not stop %@", agent.Name()))
					}
				}
				if hoverButton(c, hoverButtonOptions{Symbol: "arrow.down.right.and.arrow.up.left", Size: 14, Tooltip: L("Exit Full Screen")}).Clicked() {
					st.full = false
				}
			})
			ui.Box(c).FillWidth().Height(0.5).Background(p.Separator)
			ui.Box(c.Key("full-body")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				ui.Column(c).Absolute().Left(0).Top(0).Bottom(0).Right(agentSidebarWidth).Padding(18, 28, 18, 28).Children(func() {
					view.view(c).Grow(1).Basis(0).MinHeight(0)
					ui.Row(c).FillWidth().MaxWidth(view.maxWidth).AlignSelf(ui.Center).Children(func() {
						agentFooter(c, view.agent(agent))
					})
					// What the agent asks, answered here as in the chat.
					ui.Column(c).FillWidth().MaxWidth(view.maxWidth).AlignSelf(ui.Center).Children(func() {
						if chat, card := agentAskMessage(r.Chat.ID, agent.Asking); card != nil {
							ui.Column(c).FillWidth().Margin(12, 0, 0, -horizontalInset).Children(func() { m.agentAskCard(c, chat, card, nil, true) })
						}
						m.agentAsk(c, p, agent, r.Chat.ID, id, botName(r.Message), asking, run)
					})
				})
				m.agentSidebar(c, p, r, view, asking, run)
			})
		})
	})
}

// agentSidebarWidth is the width of the full view's column of facts.
const agentSidebarWidth = 300

// agentSidebar is the full view's column of facts: what it works on, where, its session with its
// model and effort to change, its use, its plan, and its pull request.
func (m *mainWindow) agentSidebar(c *ui.Context, p *palette, r model.RunningAgent, view *agentTranscript, st *agentCardState, run agentWork) {
	agent, id := r.Agent, r.Message.ID
	section := func(key, title string, content func()) {
		ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
			ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(p.Label3)
			content()
		})
	}
	fact := func(icon, text, tooltip string, mono bool) {
		ui.Row(c).Gap(7).AlignItems(ui.Center).MinWidth(0).Tooltip(tooltip).Children(func() {
			symbol(c, icon, 12, 2).TextColor(p.Label3)
			t := ui.Text(c, text).Shrink(1).MinWidth(0).FontSize(12).TextColor(p.Label).SingleLine()
			if mono {
				t.Font(monoFont).FontSize(11.5)
			}
		})
	}
	ui.Scroll(c.Key("agent-sidebar")).Absolute().Right(0).Top(0).Bottom(0).Width(agentSidebarWidth).Background(p.Inspector).Children(func() {
		ui.Column(c).FillWidth().Gap(22).Padding(20, 18, 24, 18).Children(func() {
			if agent.Task != "" {
				section("task", L("Task"), func() {
					ui.Text(c, agent.Task).FontSize(12.5).LineHeight(1.45).TextColor(p.Label).Selectable()
				})
			}
			section("place", L("Workspace"), func() {
				if agent.Branch != "" {
					fact("arrow.triangle.branch", agent.Branch, L("Branch"), true)
				}
				if agent.Folder != "" {
					fact("folder", agent.Folder, agent.Folder, true)
				}
				if agent.Device != "" {
					fact("desktopcomputer", agent.Device, L("Runner"), false)
				}
				if since := agentSince(c, agent); since != "" {
					fact("clock", L("Started %@ ago", since), "", false)
				}
			})
			if len(agent.Settings) > 0 || len(agent.Controls) > 0 {
				section("session", L("Session"), func() {
					agentFacts(c, p, agent, r.Chat.ID, id, st, run, 120)
				})
			}
			if steps := view.plan(); len(steps) > 0 {
				done := 0
				for _, step := range steps {
					if step.Done {
						done++
					}
				}
				section("plan", L("Plan · %@ of %@", fmt.Sprint(done), fmt.Sprint(len(steps))), func() {
					ui.Box(c).FillWidth().Height(4).Radius(2).Background(p.Label4).Clip().Children(func() {
						ui.Box(c).Height(4).WidthPercent(float32(done) * 100 / float32(len(steps))).Radius(2).Background(p.Green)
					})
					for i, step := range steps {
						ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Start).MinWidth(0).Children(func() {
							mark, tint := "circle", p.Label3
							if step.Done {
								mark, tint = "checkmark.circle.fill", p.Green
							}
							symbol(c, mark, 13, 1.8).TextColor(tint).Margin(1, 0, 0, 0)
							t := ui.Text(c, step.Text).Grow(1).Shrink(1).MinWidth(0).FontSize(12).LineHeight(1.4).TextColor(p.Label)
							if step.Done {
								t.TextColor(p.Label2)
							}
						})
					}
				})
			}
			if agent.PullRequest != "" {
				section("pr", L("Pull request"), func() {
					agentEnding(c, p, &model.AgentRun{PullRequest: agent.PullRequest, PullRequestName: agent.PullRequestName}, r.Chat.ID)
				})
			}
		})
	})
}
