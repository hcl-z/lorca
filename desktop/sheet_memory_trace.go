package main

import (
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// Shows what a bot's memory did lately, newest first: which remembered lines a turn was given,
// the facts it saved, and those the write gate refused. Lorca Dev alone has it, from the Memory
// section, to watch memory change while working on it; it reads the trace `bots.memory` carries.

// presentMemoryTrace shows the bot's memory trace, read again each time it opens.
func (w *appWindow) presentMemoryTrace(bot *model.Bot) {
	var (
		events []model.MemoryEvent
		err    string
		loaded bool
	)
	reload := func() {
		store.BotMemory(bot.ID, func(memory model.BotMemory, e error) {
			events, loaded, err = memory.Trace, true, ""
			if e != nil {
				err = model.ErrorText(e)
			}
		})
	}
	reload()
	w.present(func(c *ui.Context, s *sheet) {
		p := colors(c)
		result := sheetFrame(c, sheetOptions{
			Title:    L("%@'s memory trace", bot.Name),
			Subtitle: L("What the bot was reminded of, saved, and refused, newest first. Lorca Dev only."),
			Width:    600,
			Confirm:  L("Done"),
			NoCancel: true,
			Leading: func() {
				if pushButton(c, L("Reload"), pushOptions{Small: true, Symbol: "arrow.clockwise"}).Clicked() {
					reload()
				}
			},
		}, func() {
			switch {
			case !loaded:
				ui.Text(c, L("Loading…")).FontSize(textBody).TextColor(p.Label2)
			case err != "":
				ui.Text(c, err).FontSize(textBody).LineHeight(1.4).TextColor(p.tone(model.ToneRed))
			case len(events) == 0:
				ui.Text(c, L("Nothing yet. Write to the bot, or have it remember something.")).FontSize(textBody).TextColor(p.Label2)
			default:
				ui.Scroll(c).MaxHeight(460).Children(func() {
					ui.Column(c).Gap(14).Children(func() {
						for i, event := range events {
							memoryEventRow(c.Key(i), event)
						}
					})
				})
			}
		})
		if result.Confirmed || result.Cancelled {
			s.dismiss()
		}
	}, nil)
}

// memoryEventRow is one event: what kind, when, what it was about, and what came of it.
func memoryEventRow(c *ui.Context, event model.MemoryEvent) {
	p := colors(c)
	symbolName, tint := memoryEventLook(p, event)
	title := memoryEventTitle(event)
	ui.Row(c).Gap(10).AlignItems(ui.Start).FillWidth().Children(func() {
		ui.Box(c).Padding(2, 0, 0, 0).TextColor(tint).Children(func() { symbol(c, symbolName, 15, 2) })
		ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, title).FontSize(textCaption).FontWeight(600).TextColor(tint)
				ui.Text(c, model.Stamp(time.Unix(event.At, 0))).FontSize(textCaption).TextColor(p.Label3)
			})
			ui.Text(c, event.Text).FontSize(textBody).LineHeight(1.4).TextColor(p.Label).Selectable()
			if detail := memoryEventDetail(event); detail != "" {
				ui.Text(c, detail).FontSize(textCaption).LineHeight(1.45).TextColor(p.Label2).Selectable()
			}
		})
	})
}

// memoryEventLook is an event's symbol and color.
func memoryEventLook(p *palette, event model.MemoryEvent) (string, ui.Color) {
	switch event.Kind {
	case model.MemoryRecalled:
		if len(event.Detail) == 0 {
			return "magnifyingglass", p.Label3
		}
		return "magnifyingglass", p.Accent
	case model.MemorySaved:
		return "checkmark.circle", p.Green
	default:
		return "xmark.circle", p.Orange
	}
}

// memoryEventTitle is what an event was, in a few words.
func memoryEventTitle(event model.MemoryEvent) string {
	switch event.Kind {
	case model.MemoryRecalled:
		switch len(event.Detail) {
		case 0:
			return L("Reminded of nothing")
		case 1:
			return L("Reminded of 1 line")
		}
		return L("Reminded of %d lines", len(event.Detail))
	case model.MemorySaved:
		return L("Saved")
	}
	return L("Refused")
}

// memoryTraceSummary is the Memory section's Trace row: the newest event, or that there is none.
func memoryTraceSummary(events []model.MemoryEvent) string {
	if len(events) == 0 {
		return L("Nothing yet")
	}
	return L("%@ · %@", memoryEventTitle(events[0]), model.Stamp(time.Unix(events[0].At, 0)))
}

// memoryEventDetail is what came of an event, as the row shows it under the event's text.
func memoryEventDetail(event model.MemoryEvent) string {
	switch event.Kind {
	case model.MemoryRecalled:
		return strings.Join(event.Detail, "\n")
	case model.MemorySaved:
		if len(event.Detail) > 0 {
			return L("in %@", event.Detail[0])
		}
	default:
		return strings.Join(event.Detail, " ")
	}
	return ""
}
