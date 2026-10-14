package main

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Message Markdown on screen, after the macOS app's MarkdownRenderer: every block a blank line's
// worth from the next (a code box sits closer), list items two points apart, a bar for quotes, a
// grid for a table (inside a list or quote its rows as lines, cells three spaces apart), and a
// rule as a line of box-drawing characters. A message is one selection: a drag runs across its
// paragraphs, lists, and code, and Copy joins them a line apart.

const (
	paragraphGap = 24
	codeGap      = 8
	itemGap      = 2
)

type markdownOptions struct {
	// OnUserBubble keeps links in the bubble's text color on the accent fill.
	OnUserBubble bool
	// Size is the body's text size, textMessage by default.
	Size float32
	// Menu adds items above Copy and Select All in the text's context menu, as a message's Reply.
	Menu func(m *ui.Menu)
	// Mentionable are the bots whose @Name reads in their color: the other members of a group, in
	// a member's message.
	Mentionable []*model.Bot
	// Document sets the text as a page to read rather than a message: roomier lines, headings that
	// step down in size, a quote on a tinted ground, a hairline for a rule, a table in a frame.
	Document bool
}

// openLink opens a link from a message: a web or mail link in the browser, anything else not at all.
func openLink(href string) {
	u, err := url.Parse(href)
	if err != nil {
		return
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return
		}
	case "mailto":
	default:
		return
	}
	go mygo.Shell.OpenExternal(u.String())
}

// markdownView builds a message body.
func markdownView(c *ui.Context, text string, o markdownOptions) ui.Element {
	if o.Size == 0 {
		o.Size = textMessage
	}
	blocks := model.MarkdownBlocks(text)
	body := ui.Column(c).Selectable().MinWidth(0).FontSize(o.Size).LineHeight(1.55)
	if o.Document {
		body.LineHeight(1.7)
	}
	if o.Menu != nil {
		body.ContextMenu(func(m *ui.Menu) {
			o.Menu(m)
			m.Separator()
			m.EditItems()
		})
	}
	body.Children(func() { markdownBlocks(c, blocks, o, false) })
	return body
}

// blockGap is the room between a block and the one before it.
func blockGap(previous, current model.Block, o markdownOptions) float32 {
	if o.Document {
		switch {
		case current.Kind == model.BlockHeading && current.Level <= 2:
			return 34
		case current.Kind == model.BlockHeading:
			return 26
		case previous.Kind == model.BlockHeading:
			return 10
		case previous.Kind == model.BlockCode || current.Kind == model.BlockCode:
			return 12
		}
		return 16
	}
	if previous.Kind == model.BlockCode || current.Kind == model.BlockCode {
		return codeGap
	}
	return paragraphGap
}

func markdownBlocks(c *ui.Context, blocks []model.Block, o markdownOptions, nested bool) {
	for i, block := range blocks {
		top := float32(0)
		if i > 0 {
			top = blockGap(blocks[i-1], block, o)
			if nested && o.Document {
				top = min(top, 8)
			}
		}
		markdownBlock(c, block, o, nested).Margin(top, 0, 0, 0)
	}
}

// spansView builds a paragraph of spans, its links clickable.
func spansView(c *ui.Context, spans []model.Span, o markdownOptions, bold bool) ui.Element {
	p := colors(c)
	para := ui.RichText(c).MinWidth(0)
	var chips []*sourceChip
	if !o.OnUserBubble {
		spans = joinLinkSpans(spans)
		spans = model.SplitMentions(spans, o.Mentionable)
	}
	para.Children(func() {
		for _, span := range spans {
			// An @Name of a member reads in the member's color, bold, as it does in the composer.
			if bot := store.Bot(span.Mention); span.Mention != "" && bot != nil {
				ui.Text(c, span.Text).FontWeight(600).TextColor(p.accentColor(bot.Accent))
				continue
			}
			// A web link is a source chip; in the user's own bubble, which the chip's fill does not
			// suit, and for any other link, it stays link text.
			if model.SourceHost(span.Link) != "" && !o.OnUserBubble {
				chips = append(chips, sourceChipText(c, span, o, bold || span.Bold))
				continue
			}
			t := ui.Text(c, span.Text)
			if span.Bold || bold {
				t.FontWeight(600)
			}
			if span.Italic {
				t.Italic()
			}
			if span.Strike {
				t.Strikethrough()
			}
			if span.Code {
				t.Font(monoFont).FontSize(o.Size - 1).TextBackground(p.Code)
			}
			if span.Link != "" {
				href := span.Link
				t.Underline().Cursor(ui.CursorPointer).Tooltip(href)
				if !o.OnUserBubble {
					t.TextColor(p.Link)
				}
				if t.Clicked() {
					openLink(href)
				}
			}
		}
	})
	if len(chips) > 0 {
		para.Draw(paintSourceChips(c, chips))
	}
	return para
}

func taskMarker(ordered bool, number int, checked *bool) string {
	if checked != nil {
		if *checked {
			return "☑"
		}
		return "☐"
	}
	if ordered {
		return strconv.Itoa(number) + "."
	}
	return "•"
}

// cellsLine is a table row as one line, its cells three spaces apart.
func cellsLine(cells [][]model.Span) []model.Span {
	var line []model.Span
	for i, cell := range cells {
		if i > 0 {
			line = append(line, model.PlainSpan("   "))
		}
		line = append(line, cell...)
	}
	return line
}

func markdownBlock(c *ui.Context, block model.Block, o markdownOptions, nested bool) ui.Element {
	p := colors(c)
	switch block.Kind {
	case model.BlockParagraph:
		return spansView(c, block.Spans, o, false)
	case model.BlockHeading:
		h := spansView(c, block.Spans, o, true).FontWeight(700)
		if o.Document {
			// A page's headings step down from large to the body's size, the last two quieter.
			scales := [...]float32{1.9, 1.5, 1.22, 1.06, 1, 1}
			h.FontSize(o.Size * scales[min(max(block.Level, 1), 6)-1]).LineHeight(1.3)
			if block.Level > 2 {
				h.FontWeight(600)
			}
			if block.Level > 4 {
				h.TextColor(p.Label2)
			}
			return h
		}
		if block.Level <= 2 {
			h.FontSize(o.Size + 2)
		}
		return h
	case model.BlockCode:
		// A header of its language and a Copy that takes the whole block, apart from the selection.
		code := ui.Column(c).Radius(radiusMD).Background(p.Code).MinWidth(0).Clip()
		codeSize := float32(12)
		if o.Document {
			code.Border(1, p.Separator)
			codeSize = 12.5
		}
		code.Children(func() {
			ui.Row(c).Height(26).Padding(0, 4, 0, 10).Gap(6).MinWidth(0).Unselectable().Children(func() {
				ui.Text(c, block.Language).Grow(1).Shrink(1).MinWidth(0).Font(monoFont).FontSize(11).TextColor(p.Label2).SingleLine()
				copyButton(c, block.Text, copyOptions{Symbol: "doc.on.doc", Tooltip: L("Copy code")})
			})
			text := block.Text
			if text == "" {
				text = " "
			}
			ui.Box(c).Padding(0, 10, 9, 10).MinWidth(0).Children(func() {
				ui.Text(c, text).Font(monoFont).FontSize(codeSize).LineHeight(1.42)
			})
		})
		return code
	case model.BlockList:
		gap := float32(itemGap)
		if o.Document {
			gap = 6
		}
		list := ui.Column(c).Gap(gap).MinWidth(0)
		list.Children(func() {
			for i, item := range block.Items {
				ui.Row(c).AlignItems(ui.Start).MinWidth(0).Children(func() {
					// The marker stays out of the selection, so a copied list is its items' text.
					ui.Text(c, taskMarker(block.Ordered, block.Start+i, item.Checked)).MinWidth(18).Padding(0, 4, 0, 0).NoWrap().Unselectable()
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						markdownBlocks(c, item.Blocks, o, true)
					})
				})
			}
		})
		return list
	case model.BlockQuote:
		if o.Document {
			// A tinted ground with a bar at its edge, the words a shade quieter.
			quote := ui.Row(c).AlignItems(ui.Stretch).MinWidth(0).Radius(radiusMD).Clip().Background(p.Code)
			quote.Children(func() {
				ui.Box(c).Width(3).Background(p.Label3)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(10, 14).TextColor(p.Label2).Children(func() {
					markdownBlocks(c, block.Blocks, o, true)
				})
			})
			return quote
		}
		quote := ui.Row(c).AlignItems(ui.Stretch).Gap(8).MinWidth(0)
		quote.Children(func() {
			ui.Box(c).Width(2).Radius(1).Background(p.Label3)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				markdownBlocks(c, block.Blocks, o, true)
			})
		})
		return quote
	case model.BlockTable:
		if nested {
			rows := ui.Column(c).Gap(itemGap).MinWidth(0)
			rows.Children(func() {
				spansView(c, cellsLine(block.Header), o, true)
				for _, row := range block.Rows {
					spansView(c, cellsLine(row), o, false)
				}
			})
			return rows
		}
		wrap := ui.ScrollHorizontal(c).MaxWidthPercent(100)
		if o.Document {
			wrap.Radius(radiusMD).Clip().Border(1, p.Separator)
		}
		wrap.Children(func() {
			tracks := make([]ui.Track, len(block.Header))
			for i := range tracks {
				tracks[i] = ui.FitContent()
			}
			ui.Grid(c).ColumnTracks(tracks...).FontSize(o.Size - 0.5).LineHeight(1.35).Children(func() {
				cell := func(spans []model.Span, column int, header, first bool) {
					box := ui.Box(c).Padding(6, 10).MinWidth(44)
					if header {
						box.BorderWidth(0, 0, 1, 0).BorderColor(p.Label3)
						if o.Document {
							box.Background(p.Code).BorderColor(p.Separator)
						}
					} else if !first {
						box.BorderWidth(1, 0, 0, 0).BorderColor(p.Separator)
					}
					align := ui.Start
					if column < len(block.Alignments) {
						switch block.Alignments[column] {
						case model.AlignCenter:
							align = ui.Center
						case model.AlignRight:
							align = ui.End
						}
					}
					box.Children(func() {
						if len(spans) == 0 {
							spans = []model.Span{model.PlainSpan(" ")}
						}
						spansView(c, spans, o, header).TextAlign(align)
					})
				}
				for column, spans := range block.Header {
					cell(spans, column, true, true)
				}
				for r, row := range block.Rows {
					for column := range block.Header {
						var spans []model.Span
						if column < len(row) {
							spans = row[column]
						}
						cell(spans, column, false, r == 0)
					}
				}
			})
		})
		return wrap
	case model.BlockRule:
		if o.Document {
			return ui.Box(c).FillWidth().Height(1).Background(p.Separator)
		}
		return ui.Text(c, strings.Repeat("─", 24)).TextColor(p.Label3).NoWrap()
	}
	return ui.Box(c)
}
