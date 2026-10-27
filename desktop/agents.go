package main

import (
	"fmt"
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A coding agent's card, on the row of the `coding_agent` call that started it, from Auto-review's
// question to how it ended, drawn from the agent kit (`agentkit.go`): the agent's tile, its name
// over where it works, how it stands as a pill, and Stop while it runs; what it was asked; while it
// works, what it does on a console; the facts of its session; and a footer that opens its
// transcript. When it asks: Allow once, Always allow, and Deny before it starts or runs a command. A
// question the agent itself asks is a card of its own further down the chat (`agent_ask_card.go`),
// which the status points to. A click on what it works on opens its transcript.

// agentTitle is "Chef wants to start Claude Code on Workbench", "Claude Code wants to run a
// command", "Claude Code asks", or the agent's name.
func agentTitle(agent *model.AgentRun, botName string) string {
	if q := agent.Question; q != nil {
		switch q.Kind {
		case "start":
			if agent.Device != "" {
				return L("%@ wants to start %@ on %@", botName, agent.Name(), agent.Device)
			}
			return L("%@ wants to start %@", botName, agent.Name())
		case "command":
			return L("%@ wants to run a command", agent.Name())
		default:
			return L("%@ asks", agent.Name())
		}
	}
	return agent.Name()
}

// agentPlace is where it works, as its header says it: its branch, else its folder's name, and
// the Runner.
func agentPlace(agent *model.AgentRun) string {
	parts := []string{}
	if place := agent.Place(); place != "" {
		parts = append(parts, place)
	}
	if agent.Device != "" {
		parts = append(parts, agent.Device)
	}
	return strings.Join(parts, " · ")
}

// agentOutput is its last lines with something on them, while it works, as the CLI keeps them.
func agentOutput(agent *model.AgentRun) string {
	return strings.Join(agent.Tail, "\n")
}

func agentNote(agent *model.AgentRun) string {
	if q := agent.Question; q != nil && q.IsPermission() && q.HasRule {
		return L("Always allow adds the rule “%@”.", q.Rule)
	}
	return ""
}

func agentSpokenText(agent *model.AgentRun, botName string) string {
	parts := []string{agentTitle(agent, botName), agent.Task, agent.Status()}
	if place := agent.Place(); place != "" {
		parts = append(parts, place)
	}
	if text := agentOutput(agent); text != "" {
		parts = append(parts, text)
	}
	if q := agent.Question; q != nil && q.Command != "" {
		parts = append(parts, q.Command)
	}
	return strings.Join(parts, ": ")
}

// agentCardState is a card's own: a request under way, and what went wrong, until the question
// changes.
type agentCardState struct {
	busy bool
	err  string
	seen *model.AgentQuestion
}

// agentHeader is the top of every agent surface: its tile, its name over where it works, how it
// stands, and what goes at the end, such as Stop.
func agentHeader(c *ui.Context, p *palette, agent *model.AgentRun, tile, nameSize float32, trailing func()) {
	ui.Row(c.Key("header")).FillWidth().Gap(10).AlignItems(ui.Center).MinWidth(0).Children(func() {
		agentTile(c, p, agent.Kind, tile)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, agent.Name()).FontSize(nameSize).FontWeight(600).FixedLineHeight(nameSize + 5).SingleLine()
			if place := agentPlace(agent); place != "" {
				ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Children(func() {
					icon := "folder"
					if agent.Branch != "" {
						icon = "arrow.triangle.branch"
					}
					symbol(c, icon, 10, 2).TextColor(p.Label3)
					ui.Text(c, place).Shrink(1).MinWidth(0).FontSize(11).TextColor(p.Label3).SingleLine().Tooltip(agent.Folder)
				})
			}
		})
		agentPill(c, p, agent)
		if trailing != nil {
			trailing()
		}
	})
}

func (m *mainWindow) agentCard(c *ui.Context, chat *model.Chat, message *model.Message, cardAvatar *avatarContent, groupStart bool) {
	p := colors(c)
	agent := message.Body.Tool.Agent
	name := botName(message)
	chatID, messageID := chat.ID, message.ID
	holder := ui.Box(c.Key("agent:" + message.ID))
	st, run := m.agentAsker(holder, agent)
	holder.Children(func() {
		cardFrameAgent(c, groupStart, agentSpokenText(agent, name), func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				agentHeader(c, p, agent, 30, 13.5, func() {
					if agent.IsRunning() {
						if stopButton(c, p, st.busy).Clicked() {
							run(func(done func(error)) { store.StopAgent(chatID, messageID, done) }, L("Could not stop %@", agent.Name()))
						}
					}
				})
				// What it works on, and what it does now; a click opens its transcript.
				body := func() {
					if agent.Task != "" {
						ui.Text(c, agent.Task).Margin(10, 0, 0, 0).FontSize(13).LineHeight(1.45).MaxLines(3).Tooltip(agent.Task)
					}
					if text := agentOutput(agent); text != "" {
						consoleTail(c, text, 5, agentBusy(agent)).Margin(10, 0, 0, 0)
					}
				}
				if agent.Started() {
					b := ui.ButtonBase(c).Column().AlignItems(ui.Stretch).FillWidth().Cursor(ui.CursorPointer).Label(L("Show the transcript")).TextColor(p.Label)
					b.Children(body)
					if b.Clicked() {
						m.presentAgentTranscript(chatID, message, agent)
					}
				} else {
					ui.Column(c).FillWidth().Children(body)
				}
				agentFacts(c, p, agent, chatID, messageID, st, run, 72)
				m.agentAsk(c, p, agent, chatID, messageID, name, st, run)
				agentEnding(c, p, agent, chatID)
				if agent.Started() {
					ui.Box(c).FillWidth().Height(0.5).Margin(12, 0, 0, 0).Background(p.Separator)
					ui.Row(c).FillWidth().Margin(9, 0, 0, 0).AlignItems(ui.Center).Children(func() {
						if since := agentSince(c, agent); since != "" {
							ui.Text(c, L("Started %@ ago", since)).FontSize(11).FontFeatures("tnum").TextColor(p.Label3)
						}
						ui.Box(c).Grow(1)
						link := ui.ButtonBase(c.Key("open")).Row().Gap(3).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(L("Transcript")).TextColor(p.Label2)
						if link.Hovered() {
							link.TextColor(p.Label)
						}
						link.Children(func() {
							ui.Text(c, L("Transcript")).FontSize(11.5).FontWeight(500)
							symbol(c, "chevron.right", 10, 2.2)
						})
						if link.Clicked() {
							m.presentAgentTranscript(chatID, message, agent)
						}
					})
				}
			})
		})
	})
}

// agentFacts is its session as a spec strip: a cell each for its model, how hard it thinks, and
// its mode, a label over its value, as pop-ups where they can change now; and under them a meter
// of the context it has used, with what it cost.
func agentFacts(c *ui.Context, p *palette, agent *model.AgentRun, chatID, messageID string, st *agentCardState, run agentWork, meter float32) {
	// A card that asks keeps to its question.
	if agent.Question != nil {
		return
	}
	cost := agentSetting(agent, "Cost")
	share, used, hasContext := agentContext(agent)
	controls := map[string]model.AgentControl{}
	for _, control := range agent.Controls {
		controls[control.Name] = control
	}
	var cells []model.AgentSetting
	for _, setting := range agent.Settings {
		if setting.Name != "Context" && setting.Name != "Cost" {
			cells = append(cells, setting)
		}
	}
	if len(cells) == 0 && !hasContext && cost == "" {
		return
	}
	ui.Column(c.Key("facts")).FillWidth().Margin(12, 0, 0, 0).Radius(radiusLG).Background(p.Code).Clip().Children(func() {
		if len(cells) > 0 {
			ui.Row(c.Key("cells")).FillWidth().AlignItems(ui.Stretch).Children(func() {
				for i, setting := range cells {
					if i > 0 {
						ui.Box(c.Key(fmt.Sprintf("rule:%d", i))).Width(0.5).Margin(9, 0).Background(p.Separator)
					}
					cell := ui.Column(c.Key("cell:"+setting.Name)).Shrink(1).MinWidth(0).Gap(2).Padding(8, 11, 9, 11)
					// The model's name is the long one: it gets the room.
					if setting.Name == "Model" {
						cell.Grow(2).Basis(0)
					} else {
						cell.Grow(1).Basis(0)
					}
					cell.Children(func() {
						ui.Text(c, setting.Name).FontSize(10.5).FontWeight(500).TextColor(p.Label3).SingleLine()
						control, live := controls[setting.Name]
						if live {
							options := make([]popUpOption, len(control.Choices))
							for j, choice := range control.Choices {
								options[j] = popUpOption{Value: choice.Value, Label: choice.Name}
							}
							picked, changed, _ := popUpButton(c.Key("pop"), popUp{Options: options, Value: control.Value, Style: popUpPlain, Label: control.Name, Tooltip: setting.Value, Disabled: st.busy, MaxWidth: 220})
							if changed {
								run(func(done func(error)) { store.SetAgentOption(chatID, messageID, control.What, picked, done) }, L("Could not change %@", control.Name))
							}
							return
						}
						value := setting.Value
						if setting.Name == "Model" && agent.Model != "" {
							value = agent.Model
						}
						ui.Text(c, value).FontSize(12.5).FontWeight(500).TextColor(p.Label).SingleLine().Tooltip(setting.Value)
					})
				}
			})
		}
		if hasContext || cost != "" {
			if len(cells) > 0 {
				ui.Box(c).FillWidth().Height(0.5).Background(p.Separator)
			}
			ui.Row(c.Key("use")).FillWidth().Gap(10).Padding(8, 11).AlignItems(ui.Center).Children(func() {
				if hasContext {
					tint := p.Green
					switch {
					case share > 0.9:
						tint = p.Red
					case share > 0.66:
						tint = p.Orange
					}
					ui.Text(c, L("Context")).FontSize(10.5).FontWeight(500).TextColor(p.Label3).Shrink(0)
					ui.Box(c.Key("meter")).Grow(1).Shrink(1).MinWidth(24).Height(4).Radius(2).Background(p.Label4).Clip().Tooltip(L("Context: %@", used)).Children(func() {
						ui.Box(c).Height(4).WidthPercent(max(share*100, 1.5)).Radius(2).Background(tint)
					})
					ui.Text(c, fmt.Sprintf("%s · %d%%", used, int(share*100+0.5))).FontSize(11).FontFeatures("tnum").TextColor(p.Label2).SingleLine().Shrink(0)
				} else {
					ui.Box(c).Grow(1)
				}
				if cost != "" {
					if hasContext {
						ui.Box(c).Width(0.5).Height(12).Background(p.Separator)
					}
					ui.Text(c, cost).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(p.Label).Tooltip(L("Cost so far")).Shrink(0)
				}
			})
		}
	})
}

// agentEnding is what the card says once there is nothing to decide: the question it asks in the
// chat, why it failed, or the pull request it opened.
func agentEnding(c *ui.Context, p *palette, agent *model.AgentRun, chatID string) {
	switch {
	case agent.Asking != "":
		agentCallout(c, p, p.Orange, "questionmark.circle", L("Asks you in the chat"), func() {
			if _, card := agentAskMessage(chatID, agent.Asking); card != nil && card.Body.Request.AgentAsk.Message != "" {
				ui.Text(c, model.FirstLine(card.Body.Request.AgentAsk.Message)).FontSize(12).LineHeight(1.4).TextColor(p.Label2).MaxLines(2).Margin(0, 0, 0, 21)
			}
		})
	case agent.State == model.AgentFailed && agent.Outcome != "":
		agentCallout(c, p, p.Red, "xmark.circle.fill", L("Failed"), func() {
			ui.Text(c, agent.Outcome).FontSize(12).LineHeight(1.4).TextColor(p.Label2).MaxLines(4).Selectable().Margin(0, 0, 0, 21)
		})
	}
	if agent.PullRequest != "" {
		pr := ui.ButtonBase(c.Key("pr")).Row().Gap(6).Margin(10, 0, 0, 0).Height(26).Padding(0, 10, 0, 8).Radius(radiusSM).Background(p.Green.Alpha(0.1)).Border(0.5, p.Green.Alpha(0.25)).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(L("Open the pull request")).Tooltip(agent.PullRequest)
		pr.Children(func() {
			symbol(c, "arrow.triangle.pull", 12, 2).TextColor(p.Green)
			ui.Text(c, pullRequestName(agent)).FontSize(12).FontWeight(600).TextColor(p.Label)
			symbol(c, "arrow.up.right", 10, 2).TextColor(p.Label3)
		})
		if pr.Clicked() {
			openExternal(agent.PullRequest)
		}
	}
}

// pullRequestName is "acme/shop #7", as the CLI names its pull request, else "Pull request".
func pullRequestName(agent *model.AgentRun) string {
	if agent.PullRequestName != "" {
		return agent.PullRequestName
	}
	return L("Pull request")
}

// agentWork runs a request for a card, keeping it from running twice at once and saying what went
// wrong: on the card where it has a note, else in an alert.
type agentWork func(work func(done func(error)), failure string)

// agentAsker is the state behind an agent's question and Stop, and the runner of their requests,
// kept in `holder`: the card in the chat, its floating window, and its full-screen view each have
// their own.
func (m *mainWindow) agentAsker(holder ui.Element, agent *model.AgentRun) (*agentCardState, agentWork) {
	st := ui.Local(holder, "card", func() agentCardState { return agentCardState{} })
	// Another question starts afresh.
	if st.seen != agent.Question {
		st.seen, st.err = agent.Question, ""
	}
	run := func(work func(done func(error)), failure string) {
		if st.busy {
			return
		}
		st.busy, st.err = true, ""
		work(func(err error) {
			st.busy = false
			if err == nil {
				return
			}
			if agent.Question != nil {
				st.err = model.ErrorText(err)
			} else {
				m.showAlert(alertOptions{Message: failure, Informative: model.ErrorText(err)}, nil)
			}
		})
	}
	return st, run
}

// stopButton is Stop: a small square on a round fill, which darkens under the pointer.
func stopButton(c *ui.Context, p *palette, busy bool) ui.Element {
	stop := ui.ButtonBase(c).Size(24, 24).Radius(radiusCapsule).Justify(ui.Center).Background(p.Code).TextColor(p.Label2).Label(L("Stop")).Tooltip(L("Stop"))
	switch {
	case busy:
		stop.Disabled(true)
	case stop.Pressed():
		stop.Background(p.Pressed).TextColor(p.Label)
	case stop.Hovered():
		stop.Background(p.Red.Alpha(0.14)).TextColor(p.Red)
	}
	stop.Transition(controlFade)
	stop.Children(func() { symbol(c, "stop.fill", 10, 1.75) })
	return stop
}

// agentAsk draws what an agent's card asks, as a callout: whether it may start or run a command,
// the command, why Auto-review asked, the buttons that answer it, and the rule Always allow adds.
func (m *mainWindow) agentAsk(c *ui.Context, p *palette, agent *model.AgentRun, chatID, messageID, who string, st *agentCardState, run agentWork) {
	q := agent.Question
	if q == nil || !q.IsPermission() {
		return
	}
	agentCallout(c, p, p.Orange, "hand.raised", agentTitle(agent, who), func() {
		if q.Kind == "command" {
			if commandBlock(c, "$ "+model.FirstLine(q.Command), 1) {
				m.presentCommandSheet(L("%@'s command", agent.Name()), q.Command)
			}
		}
		if q.Reason != "" {
			ui.Text(c, q.Reason).FontSize(11.5).LineHeight(1.4).TextColor(p.Label2).MaxLines(4)
		}
		ui.Row(c).Wrap().Gap(6).Margin(2, 0, 0, 0).Children(func() {
			for i, choice := range q.Decisions() {
				kind := buttonDefault
				if i == 0 {
					kind = buttonPrimary
				}
				if pushButton(c.Key(choice.Decision), choice.Title, pushOptions{Small: true, Kind: kind}).Clicked() {
					store.AnswerPermission(chatID, messageID, choice.Decision)
				}
			}
		})
		if note := firstNonEmpty(st.err, agentNote(agent)); note != "" {
			t := ui.Text(c, note).FontSize(11).LineHeight(1.35).TextColor(p.Label3)
			if st.err != "" {
				t.TextColor(p.Red).Tooltip(st.err)
			}
		}
	})
}

// MARK: - The transcript

// presentAgentTranscript shows what a coding agent was sent, said, and did.
func (w *appWindow) presentAgentTranscript(chatID string, message *model.Message, agent *model.AgentRun) {
	t := newAgentTranscript(chatID, message)
	title := agent.Name()
	if agent.Title != "" {
		title += " · " + agent.Title
	}
	if place := agent.Place(); place != "" {
		title += " · " + place
	}
	w.present(func(c *ui.Context, s *sheet) {
		options := sheetOptions{Title: title, Width: 680, Confirm: L("Done"), NoCancel: true}
		result := sheetFrame(c, options, func() {
			t.view(c).Height(460)
		})
		if result.Confirmed || result.Cancelled {
			s.dismiss()
		}
	}, t.close)
}
