package main

import (
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// Shows the system prompt a bot's next turn would send, section by section, with each section's
// size, and then the notes that follow the transcript. It is read-only: the bot's Runner builds
// the prompt from the bot's profile, its chat, and what is installed there, and runs nothing.

type promptState struct {
	prompt model.BotPrompt
	err    string
	loaded bool
}

// presentPrompt shows a bot's prompt for a chat; chatID may be empty for its direct chat.
func (w *appWindow) presentPrompt(bot *model.Bot, chatID string) {
	st := &promptState{}
	store.Prompt(bot.ID, chatID, func(prompt model.BotPrompt, err error) {
		st.prompt, st.loaded = prompt, true
		if err != nil {
			st.err = model.ErrorText(err)
		}
	})
	w.present(func(c *ui.Context, s *sheet) {
		p := colors(c)
		ready := st.loaded && st.err == ""
		result := sheetFrame(c, sheetOptions{
			Title:    L("%@'s prompt", bot.Name),
			Subtitle: L("What the next turn sends, in the order the model reads it. The notes after the transcript change as work goes on, so they stay out of the cached prompt."),
			Width:    680,
			Confirm:  L("Done"),
			NoCancel: true,
			Leading: func() {
				if ready {
					copyButton(c, st.prompt.Text(), copyOptions{Title: L("Copy"), Bordered: true})
				}
			},
		}, func() {
			switch {
			case !st.loaded:
				ui.Text(c, L("Loading…")).FontSize(textBody).TextColor(p.Label2)
				return
			case st.err != "":
				ui.Text(c, st.err).FontSize(textBody).LineHeight(1.4).TextColor(p.tone(model.ToneRed))
				return
			}
			ui.Text(c, L("System prompt · %@ · about %d tokens", model.Kilobytes(st.prompt.TotalBytes), st.prompt.ApproxTokens)).
				FontSize(textCaption).LineHeight(1.4).TextColor(p.Label2)
			ui.Scroll(c).MaxHeight(440).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					for _, section := range st.prompt.Sections {
						promptBlock(c.Key(section.ID), L("%@ · %@", section.Title, model.Kilobytes(section.Bytes)), section.Text)
					}
					if len(st.prompt.Notes) > 0 {
						promptBlock(c.Key("notes"), L("After the transcript · %@", model.Kilobytes(st.prompt.NotesBytes)), strings.Join(st.prompt.Notes, "\n\n"))
					}
				})
			})
		})
		if result.Confirmed || result.Cancelled {
			s.dismiss()
		}
	}, nil)
}

// promptBlock is one labelled part of the prompt, as selectable monospaced text.
func promptBlock(c *ui.Context, label, text string) {
	p := colors(c)
	ui.Column(c).Gap(4).FillWidth().Children(func() {
		ui.Text(c, label).FontSize(textCaption).FontWeight(600).TextColor(p.Label2)
		ui.Column(c).FillWidth().Padding(8, 10).Radius(8).Background(p.Code).Children(func() {
			ui.Text(c, strings.TrimSpace(text)).Font(monoFont).FontSize(12).LineHeight(1.45).TextColor(p.Label).Selectable()
		})
	})
}
