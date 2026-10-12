package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// The question card: a bot asking the user to choose (`ask_user`). Each option is a row to click,
// with a radio for one pick or a checkbox for several, its description under it, and the bot's
// recommendation marked; Other opens a field for the user's own words. One pick sends at once;
// several, or Other, send with Submit. Skip leaves the choice to the bot. Once answered, the card
// keeps the question and says what was picked.

// askCardState is a question card's own until it is answered: the options checked, Other and its
// words, a send under way, and why one failed.
type askCardState struct {
	picked  []bool
	otherOn bool
	other   string
	busy    bool
	err     string
}

func (m *mainWindow) askCard(c *ui.Context, chat *model.Chat, message *model.Message, cardAvatar *avatarContent, groupStart bool) {
	p := colors(c)
	request := message.Body.Request
	ask := request.Ask
	chatID, messageID := chat.ID, message.ID
	holder := ui.Box(c.Key("ask:" + message.ID))
	st := ui.Local(holder, "card", func() askCardState { return askCardState{} })
	if len(st.picked) != len(ask.Options) {
		st.picked = make([]bool, len(ask.Options))
	}
	pending := request.IsPending()
	send := func(choices []int, other string) {
		if st.busy {
			return
		}
		st.busy, st.err = true, ""
		store.AnswerAsk(chatID, messageID, choices, other, func(err error) {
			st.busy = false
			if err != nil {
				st.err = model.ErrorText(err)
			}
		})
	}
	picks := func() []int {
		var choices []int
		for i, on := range st.picked {
			if on {
				choices = append(choices, i)
			}
		}
		return choices
	}
	other := strings.TrimSpace(st.other)
	ready := len(picks()) > 0 || (st.otherOn && other != "")
	// Several to pick, or the user's own words: they send with Submit rather than on a click.
	needsSubmit := ask.Multiple || st.otherOn
	choose := func(i int) {
		if !pending || st.busy {
			return
		}
		st.err = ""
		if ask.Multiple {
			st.picked[i] = !st.picked[i]
			return
		}
		for j := range st.picked {
			st.picked[j] = j == i
		}
		st.otherOn = false
		send([]int{i}, "")
	}
	submit := func() {
		if !ready {
			return
		}
		words := ""
		if st.otherOn {
			words = other
		}
		choices := picks()
		if !ask.Multiple && words != "" {
			choices = nil
		}
		send(choices, words)
	}
	holder.Children(func() {
		cardFrame(c, cardAvatar, groupStart, askSpokenText(ask), func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Row(c).AlignItems(ui.Start).Gap(space2).MinWidth(0).Children(func() {
					ui.Text(c, ask.Question).Grow(1).Shrink(1).MinWidth(0).FontSize(13).FontWeight(600).LineHeight(1.35).MaxLines(4)
					if pending {
						warningBadge(c, L("Waiting for your answer"))
					}
				})
				if !pending {
					askOutcome(c, request)
					return
				}
				hint := L("Pick one")
				if ask.Multiple {
					hint = L("Pick any that apply")
				}
				ui.Text(c, hint).Margin(2, 0, 0, 0).FontSize(11.5).TextColor(p.Label2)
				options := ui.Column(c).Gap(6).Margin(space3, 0, 0, 0).Focusable().FocusRing(false).Label(ask.Question)
				// 1 to 9 pick an option while the card has the keyboard and its field does not.
				for i := range min(len(ask.Options), len(chatNumberKeys)) {
					if options.Shortcut(0, chatNumberKeys[i]) {
						choose(i)
					}
				}
				options.Children(func() {
					for i, option := range ask.Options {
						if askOptionRow(c.Key(i), askOptionLook{
							label: option.Label, description: option.Description, place: i + 1,
							multiple: ask.Multiple, on: st.picked[i], recommended: ask.Recommended == i, disabled: st.busy,
						}).Clicked() {
							choose(i)
						}
					}
					if ask.AllowOther {
						if askOptionRow(c.Key("other"), askOptionLook{
							label: L("Other"), description: L("Write your own answer…"), multiple: ask.Multiple, on: st.otherOn, disabled: st.busy,
						}).Clicked() {
							st.otherOn, st.err = !st.otherOn, ""
							if st.otherOn && !ask.Multiple {
								clear(st.picked)
							}
						}
						if st.otherOn {
							field := textField(c.Key("other-field"), &st.other, fieldOptions{Placeholder: L("Write your own answer…"), Label: L("Other"), AutoFocus: true, Disabled: st.busy})
							if field.Submitted() {
								submit()
							}
						}
					}
				})
				ui.Row(c).Gap(6).Margin(space3, 0, 0, 0).Children(func() {
					if needsSubmit {
						if pushButton(c.Key("submit"), L("Submit"), pushOptions{Kind: buttonPrimary, Disabled: st.busy || !ready}).Clicked() {
							submit()
						}
					}
					if pushButton(c.Key("skip"), L("Skip"), pushOptions{Disabled: st.busy}).Clicked() {
						store.SkipAsk(chatID, messageID)
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

// askOutcome is an answered card's last line: what was picked, or why there is no answer.
func askOutcome(c *ui.Context, request *model.PermissionRequest) {
	p := colors(c)
	text, name, tint := "", "minus.circle", p.Label2
	switch request.Decision {
	case model.DecisionAllowed, model.DecisionAlways:
		text, name, tint = L("You chose %@", request.Ask.AnswerText()), "checkmark.circle.fill", p.Green
	case model.DecisionDismissed:
		text = L("Answered in the chat instead")
	case model.DecisionExpired:
		text = L("No answer in time")
	default:
		text = L("Skipped")
	}
	ui.Row(c).Gap(6).Margin(space2, 0, 0, 0).MinWidth(0).AlignItems(ui.Start).Children(func() {
		symbol(c, name, 14, 2).TextColor(tint).Margin(1, 0, 0, 0)
		ui.Text(c, text).Grow(1).Shrink(1).MinWidth(0).FontSize(12.5).TextColor(p.Label).LineHeight(1.35).MaxLines(4).Selectable()
	})
}

// askOptionLook is how one option's row shows.
type askOptionLook struct {
	label, description string
	// place is its number, which picks it from the keyboard; 0 for none.
	place                               int
	multiple, on, recommended, disabled bool
}

// askOptionRow is one option: its radio or checkbox, its words over its description, and its
// number, in a bordered row that fills under the pointer and outlines in ink while picked.
func askOptionRow(c *ui.Context, o askOptionLook) ui.Element {
	p := colors(c)
	row := ui.ButtonBase(c).FillWidth().MinHeight(40).Padding(9, space3).Gap(10).AlignItems(ui.Start).Radius(radiusLG).
		Background(p.ButtonBG).Border(1, p.ButtonBorder).Label(o.label).Cursor(ui.CursorPointer)
	if o.multiple {
		row.Role(ui.RoleCheckBox).Checked(o.on)
	} else {
		row.Role(ui.RoleRadio).Checked(o.on)
	}
	switch {
	case o.disabled:
		row.Disabled(true)
	case o.on:
		row.Border(1.5, p.Label).Background(p.ButtonHover)
	case row.Pressed():
		row.Background(p.ButtonPressed)
	case row.Hovered():
		row.Background(p.ButtonHover).BorderColor(p.FieldBorder)
	}
	row.Children(func() {
		askMark(c, o.multiple, o.on).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(6).MinWidth(0).Children(func() {
				ui.Text(c, o.label).Shrink(1).MinWidth(0).FontSize(13).FontWeight(500).TextColor(p.Label).MaxLines(2)
				if o.recommended {
					ui.Text(c, L("Recommended")).Padding(1, 6).Radius(radiusXS).Background(p.Chip).FontSize(10.5).FontWeight(600).TextColor(p.Label2).Shrink(0)
				}
			})
			if o.description != "" {
				ui.Text(c, o.description).FontSize(11.5).LineHeight(1.35).TextColor(p.Label2).MaxLines(3)
			}
		})
		if o.place > 0 && o.place <= len(chatNumberKeys) {
			ui.Text(c, fmt.Sprint(o.place)).Margin(1, 0, 0, 0).FontSize(11).FontFeatures("tnum").TextColor(p.Label3).Shrink(0)
		}
	})
	return row
}

// askMark is a radio (one pick) or a checkbox (several): an empty ring or box, or one filled with
// ink around a dot or a check.
func askMark(c *ui.Context, multiple, on bool) ui.Element {
	p := colors(c)
	size := float32(16)
	mark := ui.Box(c).Size(size, size).Shrink(0).Center()
	if multiple {
		mark.Radius(radiusXS)
	} else {
		mark.Radius(size / 2)
	}
	if !on {
		return mark.Border(1.5, p.Label3)
	}
	mark.Background(p.Ink).TextColor(p.InkText)
	mark.Children(func() {
		if multiple {
			symbol(c, "checkmark", 11, 3)
		} else {
			ui.Box(c).Size(6, 6).Radius(3).Background(p.InkText)
		}
	})
	return mark
}

// askSpokenText is a question card as assistive technology reads it: the question, the options,
// and the answer once there is one.
func askSpokenText(ask *model.AskQuestion) string {
	labels := make([]string, 0, len(ask.Options))
	for _, option := range ask.Options {
		labels = append(labels, option.Label)
	}
	parts := []string{ask.Question, strings.Join(labels, L(", "))}
	if ask.Answer != nil {
		parts = append(parts, L("You chose %@", ask.AnswerText()))
	}
	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), ": ")
}
