package main

import (
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A coding agent's question, on a card of its own in the chat (`agent_ask`): the agent's mark and
// "Claude Code asks", its message, then its form, field by field. A pick is a row to click, with a
// radio for one and a checkbox for several, as the bot's own questions are (`ask_card.go`); Other
// opens a field for the user's own words; yes or no is a pair of rows; text and numbers are
// fields. A form that asks for one pick sends on its click; the rest send with Submit, once every
// required field has an answer. Decline leaves the agent to go on without one. Answered, the card
// keeps the question and says who answered and what, or that it was declined, went unanswered, or
// the agent stopped first.

// agentAskState is a question card's own until it is answered: the picks by field, Other and its
// words, what was typed, a send under way, and why one failed.
type agentAskState struct {
	picks   map[string][]bool
	otherOn map[string]bool
	words   map[string]*string
	busy    bool
	err     string
}

func (s *agentAskState) word(key string) *string {
	if s.words[key] == nil {
		s.words[key] = new(string)
	}
	return s.words[key]
}

func (m *mainWindow) agentAskCard(c *ui.Context, chat *model.Chat, message *model.Message, cardAvatar *avatarContent, groupStart bool) {
	p := colors(c)
	request := message.Body.Request
	ask := request.AgentAsk
	chatID, cardID := chat.ID, message.ID
	holder := ui.Box(c.Key("agent-ask:" + message.ID))
	st := ui.Local(holder, "card", func() agentAskState {
		return agentAskState{picks: map[string][]bool{}, otherOn: map[string]bool{}, words: map[string]*string{}}
	})
	for _, field := range ask.Fields {
		if len(st.picks[field.Key]) != len(fieldChoices(field)) {
			st.picks[field.Key] = make([]bool, len(fieldChoices(field)))
		}
	}
	pending := request.IsPending()
	values, answers, ready := agentAskValues(ask, st)
	send := func() {
		if st.busy || !ready {
			return
		}
		st.busy, st.err = true, ""
		store.AnswerAgentAsk(chatID, cardID, values, answers, func(err error) {
			st.busy = false
			if err != nil {
				st.err = model.ErrorText(err)
			}
		})
	}
	// One pick and nothing else to fill in: it sends on its click.
	single := len(ask.Fields) == 1 && (ask.Fields[0].Kind == "one" || ask.Fields[0].Kind == "flag") && !st.otherOn[ask.Fields[0].Key]
	choose := func(field model.AgentAskField, i int) {
		if !pending || st.busy {
			return
		}
		st.err = ""
		picks := st.picks[field.Key]
		if field.Kind == "many" {
			picks[i] = !picks[i]
			return
		}
		for j := range picks {
			picks[j] = j == i
		}
		st.otherOn[field.Key] = false
		if single {
			values, answers, ready = agentAskValues(ask, st)
			send()
		}
	}
	holder.Children(func() {
		cardFrame(c, cardAvatar, groupStart, agentAskSpokenText(ask, request.Decision), func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).Gap(10).MinWidth(0).AlignItems(ui.Center).Children(func() {
					agentTile(c, p, ask.Kind, 30)
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
						ui.Text(c, L("%@ asks", ask.Name)).FontSize(13.5).FontWeight(600).FixedLineHeight(18).SingleLine()
						ui.Text(c, L("For %@ to answer, or you", botName(message))).FontSize(11).TextColor(p.Label3).SingleLine()
					})
					if pending {
						warningBadge(c, L("Waiting for your answer"))
					}
				})
				if strings.TrimSpace(ask.Message) != "" {
					ui.Box(c).FillWidth().Margin(10, 0, 0, 0).Children(func() { markdownView(c, ask.Message, markdownOptions{Size: 13.5}) })
				}
				if !pending {
					agentAskOutcome(c, p, ask, request.Decision, botName(message))
					return
				}
				ui.Column(c).FillWidth().Gap(space3).Margin(space3, 0, 0, 0).Children(func() {
					for i, field := range ask.Fields {
						agentAskField(c.Key(i), p, ask, field, st, choose, len(ask.Fields) == 1)
					}
				})
				ui.Row(c).Gap(6).Margin(space3, 0, 0, 0).AlignItems(ui.Center).Children(func() {
					if !single {
						if pushButton(c.Key("submit"), L("Submit"), pushOptions{Kind: buttonPrimary, Disabled: st.busy || !ready}).Clicked() {
							send()
						}
					}
					if pushButton(c.Key("decline"), L("Decline"), pushOptions{Disabled: st.busy}).Tooltip(L("%@ goes on without an answer", ask.Name)).Clicked() && !st.busy {
						st.busy, st.err = true, ""
						store.DeclineAgentAsk(chatID, cardID, func(err error) {
							st.busy = false
							if err != nil {
								st.err = model.ErrorText(err)
							}
						})
					}
					if st.busy {
						spinner(c, 14).Margin(0, 0, 0, 4)
					}
				})
				if st.err != "" {
					ui.Text(c, st.err).Margin(space2, 0, 0, 0).FontSize(11.5).LineHeight(1.35).TextColor(p.Red).MaxLines(3)
				}
			})
		})
	})
}

// agentAskField draws one field: its label, then its picks, its yes and no, or its text field.
// The label is left out where the field is the whole question and says what the message says.
func agentAskField(c *ui.Context, p *palette, ask *model.AgentAsk, field model.AgentAskField, st *agentAskState, choose func(model.AgentAskField, int), only bool) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		label := field.Label
		if !field.Required && field.Kind != "flag" {
			label = L("%@ (optional)", label)
		}
		if !(only && strings.EqualFold(strings.TrimSpace(field.Label), strings.TrimSpace(ask.Message))) {
			ui.Text(c, label).FontSize(12).FontWeight(600).LineHeight(1.35).TextColor(p.Label).MaxLines(3)
		}
		switch field.Kind {
		case "one", "many", "flag":
			choices := fieldChoices(field)
			options := ui.Column(c).FillWidth().Gap(6).Focusable().FocusRing(false).Label(field.Label)
			// 1 to 9 pick while the picks have the keyboard and no field does.
			if only {
				for i := range min(len(choices), len(chatNumberKeys)) {
					if options.Shortcut(0, chatNumberKeys[i]) {
						choose(field, i)
					}
				}
			}
			options.Children(func() {
				for i, choice := range choices {
					place := 0
					if only {
						place = i + 1
					}
					if askOptionRow(c.Key(i), askOptionLook{
						label: choice.Title, description: choice.Description, place: place,
						multiple: field.Kind == "many", on: st.picks[field.Key][i], disabled: st.busy,
					}).Clicked() {
						choose(field, i)
					}
				}
				if !field.Other {
					return
				}
				if askOptionRow(c.Key("other"), askOptionLook{
					label: L("Other"), description: L("Write your own answer…"), multiple: field.Kind == "many", on: st.otherOn[field.Key], disabled: st.busy,
				}).Clicked() {
					st.otherOn[field.Key], st.err = !st.otherOn[field.Key], ""
					if st.otherOn[field.Key] && field.Kind != "many" {
						clear(st.picks[field.Key])
					}
				}
				if st.otherOn[field.Key] {
					textField(c.Key("other-field"), st.word(field.Key), fieldOptions{Placeholder: L("Write your own answer…"), Label: field.Label, AutoFocus: true, Disabled: st.busy})
				}
			})
		default:
			placeholder := L("Type your answer")
			switch field.Kind {
			case "integer":
				placeholder = L("A whole number")
			case "number":
				placeholder = L("A number")
			}
			textField(c.Key("field"), st.word(field.Key), fieldOptions{Placeholder: placeholder, Label: field.Label, Disabled: st.busy})
		}
	})
}

// fieldChoices is what a field offers to pick: its choices, or Yes and No.
func fieldChoices(field model.AgentAskField) []model.AgentAskChoice {
	if field.Kind == "flag" {
		return []model.AgentAskChoice{{Value: "yes", Title: L("Yes")}, {Value: "no", Title: L("No")}}
	}
	if field.Kind == "one" || field.Kind == "many" {
		return field.Choices
	}
	return nil
}

// agentAskValues is the form as it is filled in: the values the agent is sent, by field key, the
// answer in words, by field label, and whether every required field has one.
func agentAskValues(ask *model.AgentAsk, st *agentAskState) (map[string]any, []model.AgentSetting, bool) {
	values := map[string]any{}
	var answers []model.AgentSetting
	ready := true
	for _, field := range ask.Fields {
		words := ""
		if w := st.words[field.Key]; w != nil {
			words = strings.TrimSpace(*w)
		}
		choices := fieldChoices(field)
		var picked []model.AgentAskChoice
		for i, on := range st.picks[field.Key] {
			if on && i < len(choices) {
				picked = append(picked, choices[i])
			}
		}
		own := ""
		if field.Other && st.otherOn[field.Key] {
			own = words
		}
		shown := ""
		switch field.Kind {
		case "one":
			switch {
			case own != "":
				values[field.Key], shown = own, own
			case len(picked) > 0:
				values[field.Key], shown = picked[0].Value, picked[0].Title
			}
		case "many":
			var list, titles []string
			for _, choice := range picked {
				list, titles = append(list, choice.Value), append(titles, choice.Title)
			}
			if own != "" {
				list, titles = append(list, own), append(titles, own)
			}
			if len(list) > 0 {
				values[field.Key], shown = list, strings.Join(titles, L(", "))
			}
		case "flag":
			if len(picked) > 0 {
				values[field.Key], shown = picked[0].Value == "yes", picked[0].Title
			}
		default:
			if words != "" {
				values[field.Key], shown = words, words
			}
		}
		if shown != "" {
			answers = append(answers, model.AgentSetting{Name: field.Label, Value: shown})
		} else if field.Required {
			ready = false
		}
	}
	return values, answers, ready && len(values) > 0
}

// agentAskOutcome is an answered card's end: who answered and each field's answer, or why there is
// none.
func agentAskOutcome(c *ui.Context, p *palette, ask *model.AgentAsk, decision model.PermissionDecision, bot string) {
	text, tint := L("%@ stopped before it was answered", ask.Name), p.Label3
	switch {
	case decision == model.DecisionAllowed && ask.By == "bot":
		text, tint = L("Answered by %@", bot), p.Green
	case decision == model.DecisionAllowed:
		tint = p.Green
	case decision == model.DecisionDenied && ask.By == "bot":
		text = L("%@ declined it", bot)
	case decision == model.DecisionDenied:
		text = L("You declined it")
	case decision == model.DecisionExpired:
		text = L("No answer in time")
	}
	// Answered, the answer is a reply beside a bar in the tint, each field's label small over its
	// answer, and a bot's answer says whose it is; without one, why there is none.
	ui.Column(c).FillWidth().Gap(6).Margin(12, 0, 0, 0).Children(func() {
		if len(ask.Answers) == 0 {
			ui.Text(c, text).FontSize(12).TextColor(p.Label3).MaxLines(2)
			return
		}
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Width(2.5).Radius(1.25).Background(tint.Alpha(0.7))
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Padding(1, 0).Children(func() {
				one := len(ask.Answers) == 1 && len(ask.Fields) == 1
				for i, answer := range ask.Answers {
					ui.Column(c.Key(i)).FillWidth().Gap(1).Children(func() {
						if !one {
							ui.Text(c, answer.Name).FontSize(11).TextColor(p.Label3).MaxLines(2)
						}
						ui.Text(c, answer.Value).FontSize(14).FontWeight(600).LineHeight(1.35).TextColor(p.Label).MaxLines(6).Selectable()
					})
				}
			})
		})
		if ask.By == "bot" {
			ui.Text(c, text).Margin(0, 0, 0, 12.5).FontSize(11).TextColor(p.Label3)
		}
	})
}

// agentAskSpokenText is the card as assistive technology reads it.
func agentAskSpokenText(ask *model.AgentAsk, decision model.PermissionDecision) string {
	parts := []string{L("%@ asks", ask.Name), ask.Message}
	if decision == model.DecisionAllowed {
		parts = append(parts, ask.AnswersText())
	}
	return strings.Join(parts, ": ")
}
