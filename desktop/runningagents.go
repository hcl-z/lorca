package main

import (
	"fmt"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// runningAgentsState is whether the running agents' popover is open.
type runningAgentsState struct {
	open bool
}

// runningAgentsButton is the top-right mark of the coding agents at work: a capsule with their
// tiles stacked, newest first, and a count past three. A click opens the custom popover that lists
// every agent; a row opens that agent's transcript.
func (m *mainWindow) runningAgentsButton(c *ui.Context) {
	p := colors(c)
	running := store.RunningAgents()
	s := &m.agents
	if len(running) == 0 {
		s.open = false
		return
	}
	shown := running
	if len(shown) > 3 {
		shown = shown[len(shown)-3:]
	}
	b := ui.ButtonBase(c.Key("running-agents")).Height(34).Padding(3, 8).Radius(17).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).TextColor(p.Label).Background(p.Card).Border(1, p.Separator).Shadow(0, 2, 8, 0, p.Shadow.Alpha(0.25))
	b.Label(L("Running agents (%d)", len(running))).Tooltip(L("Running agents"))
	if b.Hovered() || s.open {
		b.Background(p.Hover)
	}
	b.Transition(controlFade)
	b.Children(func() {
		ui.Row(c).Gap(0).AlignItems(ui.Center).Children(func() {
			for i, r := range shown {
				tile := ui.Row(c.Key("agent:"+r.Agent.ID)).Size(26, 26).Center().Radius(13).Background(p.Content).Border(1, p.Separator)
				if i > 0 {
					tile.Margin(0, 0, 0, -7)
				}
				tile.Children(func() { agentTile(c, p, r.Agent.Kind, 20) })
			}
		})
		if len(running) == 1 {
			ui.Text(c, running[0].Agent.Name()).FontSize(12).FontWeight(500).SingleLine()
		} else {
			ui.Row(c).MinWidth(16).Height(16).Padding(0, 4).Radius(8).Justify(ui.Center).Background(p.Accent).Children(func() {
				ui.Text(c, fmt.Sprint(len(running))).FontSize(10).FontWeight(700).TextColor(p.White).FontFeatures("tnum")
			})
		}
	})
	if b.Clicked() {
		s.open = !s.open
	}
	ui.PopoverBase(c, b, &s.open, func(panel ui.Element) {
		popoverPanel(c, panel).Width(380)
		m.runningAgentsList(c, running)
	})
}

// runningAgentsList is the custom popover's body: one row an agent, newest first, opening its
// transcript.
func (m *mainWindow) runningAgentsList(c *ui.Context, running []model.RunningAgent) {
	p := colors(c)
	s := &m.agents
	if len(running) == 0 {
		s.open = false
		return
	}
	ui.Scroll(c).MaxHeight(420).Padding(4, 6).Children(func() {
		for i := len(running) - 1; i >= 0; i-- {
			r := running[i]
			if i < len(running)-1 {
				ui.Box(c).FillWidth().Height(0.5).Margin(4, 8).Background(p.Separator)
			}
			row := ui.ButtonBase(c.Key("agent-row:"+r.Message.ID)).Row().FillWidth().AlignItems(ui.Center).Gap(10).Padding(9, 8).Radius(radiusMD).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(p.RowHover)
			}
			row.Children(func() {
				agentTile(c, p, r.Agent.Kind, 26)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, r.Agent.Name()).Shrink(1).MinWidth(0).FontSize(12.5).FontWeight(600).SingleLine()
						agentPill(c, p, r.Agent)
					})
					task := firstNonEmpty(r.Agent.Title, r.Agent.Task)
					if task == "" {
						task = botName(r.Message)
					}
					ui.Text(c, task).Shrink(1).MinWidth(0).FontSize(11).TextColor(p.Label2).SingleLine().Tooltip(task)
				})
				symbol(c, "chevron.right", 10, 2).TextColor(p.Label3)
			})
			if row.Clicked() {
				s.open = false
				m.presentAgentTranscript(r.Chat.ID, r.Message, r.Agent)
			}
		}
	})
}
