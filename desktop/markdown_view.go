package main

import (
	"net/url"
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// Message Markdown on screen: MyGo's markdown view draws CommonMark and the
// GitHub extensions, incrementally and, for a reply still being written, at a
// readable pace. A paragraph that holds a web link or an @mention is drawn by
// the app instead, so the link keeps its source chip and the mention its bot's
// color; every other paragraph stays with MyGo's own paragraph cache.
//
// A message is one selection: a drag runs across its paragraphs, lists, and
// code, and Copy joins them a line apart.

const (
	// paragraphGap is how far a message's blocks stand apart, in ems. A reply
	// often runs to several short paragraphs, where a wider gap reads as an
	// empty line of its own.
	paragraphGap = 0.65
	// documentParagraphGap is a page's roomier spacing.
	documentParagraphGap = 1.0
)

type markdownOptions struct {
	// OnUserBubble keeps links in the bubble's text color on the ink fill, and
	// leaves the paragraph to MyGo: a chip's fill does not suit the bubble.
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

// messageReveal is how a reply being written comes in: a word at a time, at an
// even pace, catching up with each chunk within CatchUp.
var messageReveal = &markdown.Reveal{
	Unit:    markdown.ByWord,
	CatchUp: 400 * time.Millisecond,
	Fade:    350 * time.Millisecond,
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

// markdownView builds a message body that is finished.
func markdownView(c *ui.Context, text string, o markdownOptions) ui.Element {
	return markdownBody(c, text, o, false)
}

// markdownStream builds a message body still being written. Show it with
// markdownView once it is complete, in the same place, so its parse carries on
// and what is still being revealed comes in.
func markdownStream(c *ui.Context, text string, o markdownOptions) ui.Element {
	return markdownBody(c, text, o, true)
}

func markdownBody(c *ui.Context, text string, o markdownOptions, streaming bool) ui.Element {
	if o.Size == 0 {
		o.Size = textMessage
	}
	body := ui.Column(c).Selectable().MinWidth(0).FontSize(o.Size).LineHeight(markdownLineHeight(o))
	if o.Menu != nil {
		body.ContextMenu(func(m *ui.Menu) {
			o.Menu(m)
			m.Separator()
			m.EditItems()
		})
	}
	opts := markdownOptionsFor(c, o)
	body.Children(func() {
		if streaming {
			markdown.Stream(c, text, opts)
			return
		}
		markdown.View(c, text, opts)
	})
	return body
}

func markdownLineHeight(o markdownOptions) float32 {
	if o.Document {
		return 1.7
	}
	return 1.55
}

// markdownOptionsFor is the view's configuration for one message: its theme,
// and the paragraph hook that keeps source chips and mentions. The options are
// built per view because a message's role and mentionable bots differ; MyGo
// keeps the parse itself in the element's state.
func markdownOptionsFor(c *ui.Context, o markdownOptions) *markdown.Options {
	opts := &markdown.Options{
		Theme:          markdownTheme(c, o),
		Extensions:     parser.Default,
		OnLink:         openLink,
		ShareSelection: true,
		Reveal:         messageReveal,
		CodeBlock: func(c *ui.Context, b *markdown.CodeBlockView) {
			markdownCodeBlock(c, b, o)
		},
	}
	opts.Block = func(c *ui.Context, d *parser.Document, i int) bool {
		return markdownParagraph(c, d, i, o)
	}
	return opts
}

// markdownParagraph draws a paragraph with the app's spans when it holds a web
// link or an @mention, and lets MyGo draw every other paragraph.
func markdownParagraph(c *ui.Context, d *parser.Document, i int, o markdownOptions) bool {
	if d.Block(i).Kind != parser.Paragraph {
		return false
	}
	inlines := d.Inlines(i)
	if !paragraphNeedsSpans(inlines, o.Mentionable, o.OnUserBubble) {
		return false
	}
	spans := spansFromInlines(inlines)
	if len(spans) == 0 {
		return false
	}
	spansView(c, spans, o, false).FontSize(o.Size).LineHeight(markdownLineHeight(o))
	return true
}

// paragraphNeedsSpans reports whether a paragraph holds a source link (in a
// bot's message) or a mention, and nothing the span model cannot draw (inline
// HTML, math, a picture, a footnote).
func paragraphNeedsSpans(inlines []parser.Inline, mentionable []*model.Bot, user bool) bool {
	for _, in := range inlines {
		switch in.Kind {
		case parser.HTML, parser.Math, parser.Image, parser.FootnoteRef:
			return false
		}
	}
	for _, in := range inlines {
		switch in.Kind {
		case parser.Link:
			if !user && model.SourceHost(in.Dest) != "" {
				return true
			}
		case parser.Text:
			for _, bot := range mentionable {
				if strings.Contains(in.Text, "@"+bot.Name) {
					return true
				}
			}
		}
	}
	return false
}

// spansFromInlines folds MyGo's inlines into the shared Span shape, so a
// paragraph can be drawn with the app's own text, chips, and mention colors.
func spansFromInlines(inlines []parser.Inline) []model.Span {
	var out []model.Span
	var walk func(k int, s model.Span)
	walk = func(k int, s model.Span) {
		in := inlines[k]
		switch in.Kind {
		case parser.Text:
			s.Text = in.Text
			addSpan(&out, s)
		case parser.SoftBreak:
			s.Text = " "
			addSpan(&out, s)
		case parser.LineBreak:
			s.Text = "\n"
			addSpan(&out, s)
		case parser.Code:
			s.Code = true
			s.Text = in.Text
			addSpan(&out, s)
		case parser.Emphasis:
			s.Italic = true
			for j := k + 1; j < in.Next; j = inlines[j].Next {
				walk(j, s)
			}
		case parser.Strong:
			s.Bold = true
			for j := k + 1; j < in.Next; j = inlines[j].Next {
				walk(j, s)
			}
		case parser.Strikethrough:
			s.Strike = true
			for j := k + 1; j < in.Next; j = inlines[j].Next {
				walk(j, s)
			}
		case parser.Link:
			s.Link = in.Dest
			start := len(out)
			for j := k + 1; j < in.Next; j = inlines[j].Next {
				walk(j, s)
			}
			if len(out) == start {
				s.Text = in.Dest
				addSpan(&out, s)
			}
		case parser.Image:
			start := len(out)
			for j := k + 1; j < in.Next; j = inlines[j].Next {
				walk(j, s)
			}
			if len(out) == start {
				s.Text = in.Title
				if s.Text == "" {
					s.Text = in.Dest
				}
				addSpan(&out, s)
			}
		case parser.HTML, parser.Math:
			s.Text = in.Text
			addSpan(&out, s)
		case parser.FootnoteRef:
			s.Text = "[" + in.Text + "]"
			addSpan(&out, s)
		}
	}
	for k := 0; k < len(inlines); k = inlines[k].Next {
		walk(k, model.Span{})
	}
	return out
}

func addSpan(out *[]model.Span, s model.Span) {
	if s.Text == "" {
		return
	}
	if n := len(*out); n > 0 && sameSpan((*out)[n-1], s) {
		(*out)[n-1].Text += s.Text
		return
	}
	*out = append(*out, s)
}

func sameSpan(a, b model.Span) bool {
	return a.Bold == b.Bold && a.Italic == b.Italic && a.Code == b.Code && a.Strike == b.Strike && a.Link == b.Link && a.Mention == b.Mention
}

// markdownCodeBlock is a code box with its language and a Copy, drawn with the
// code MyGo highlighted: the same box the app draws elsewhere, with the
// tokenizer's colors.
func markdownCodeBlock(c *ui.Context, b *markdown.CodeBlockView, o markdownOptions) {
	p := colors(c)
	code := ui.Column(c).Radius(radiusMD).Background(p.Code).MinWidth(0).Clip()
	if o.Document {
		code.Border(1, p.Separator)
	}
	code.Children(func() {
		ui.Row(c).Height(26).Padding(0, 4, 0, 10).Gap(6).MinWidth(0).Unselectable().Children(func() {
			ui.Text(c, b.Language).Grow(1).Shrink(1).MinWidth(0).Font(monoFont).FontSize(11).TextColor(p.Label2).SingleLine()
			if !b.Open {
				copyButton(c, b.Code(), copyOptions{Symbol: "doc.on.doc", Tooltip: L("Copy code")})
			}
		})
		ui.Box(c).Padding(0, 10, 9, 10).MinWidth(0).Children(func() {
			b.Body(c)
		})
	})
}

// spansView builds a paragraph of spans, its links clickable.
func spansView(c *ui.Context, spans []model.Span, o markdownOptions, bold bool) ui.Element {
	p := colors(c)
	para := ui.RichText(c).MinWidth(0)
	var chips []*sourceChip
	if !o.OnUserBubble {
		spans = joinLinkSpans(spans)
	}
	spans = model.SplitMentions(spans, o.Mentionable)
	para.Children(func() {
		for _, span := range spans {
			// An @Name of a member reads in the member's color, bold, as it does in the composer.
			if span.Mention != "" {
				var tint ui.Color
				found := false
				for _, member := range o.Mentionable {
					if member != nil && member.ID == span.Mention {
						tint, found = p.accentColor(member.Accent), true
						break
					}
				}
				if !found && store != nil {
					if bot := store.Bot(span.Mention); bot != nil {
						tint, found = p.accentColor(bot.Accent), true
					}
				}
				if found {
					ui.Text(c, span.Text).FontWeight(600).TextColor(tint)
					continue
				}
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

// markdownTheme is the markdown look of a message or a page, from the app's
// palette. Sizes are in ems, as MyGo asks.
func markdownTheme(c *ui.Context, o markdownOptions) *markdown.Theme {
	p := colors(c)
	th := &markdown.Theme{
		FontSize:            o.Size,
		CodeFont:            monoFont,
		LineHeight:          markdownLineHeight(o),
		CodeLineHeight:      1.42,
		Text:                p.Label,
		Muted:               p.Label2,
		Link:                p.Link,
		Strong:              p.Label,
		StrongWeight:        600,
		Code:                p.Label,
		CodeBackground:      p.Code,
		CodeSize:            0.875,
		BlockSpacing:        paragraphGap,
		ItemSpacing:         0.2,
		ListIndent:          1.6,
		QuoteBar:            p.Label3,
		QuoteBarWidth:       2,
		QuoteText:           p.Label2,
		Rule:                p.Separator,
		CodeBlockBackground: p.Code,
		CodeBlockBorder:     ui.Transparent,
		CodeBlockRadius:     radiusMD,
		CodeBlockPadding:    0.85,
		TableBorder:         p.Separator,
		TableHeader:         ui.Transparent,
		TableStripe:         ui.Transparent,
		TableCellPadding:    [2]float32{0.55, 0.35},
		TableMaxColumn:      24,
		Mark:                p.Accent.Alpha(0.3),
		Checkbox:            p.Accent,
	}
	if o.Document {
		th.BlockSpacing = documentParagraphGap
		th.ItemSpacing = 0.4
		scales := [...]float32{1.9, 1.5, 1.22, 1.06, 1, 1}
		for i := range th.Headings {
			th.Headings[i] = markdown.HeadingStyle{Size: scales[i], Weight: 700, Color: p.Label, Top: 1.4, Bottom: 0.55}
			if i >= 2 {
				th.Headings[i].Weight = 600
			}
			if i >= 4 {
				th.Headings[i].Color = p.Label2
			}
		}
	} else {
		for i := range th.Headings {
			th.Headings[i] = markdown.HeadingStyle{Size: 1, Weight: 600, Color: p.Label, Top: 0.8, Bottom: 0.4}
		}
		th.Headings[0] = markdown.HeadingStyle{Size: 1.14, Weight: 700, Color: p.Label, Top: 0.8, Bottom: 0.4}
		th.Headings[1] = th.Headings[0]
	}
	if o.OnUserBubble {
		th.Text = p.AccentText
		th.Muted = p.AccentText.Alpha(0.65)
		th.Link = p.AccentText
		th.Strong = p.AccentText
		th.Code = p.AccentText
		th.CodeBackground = p.AccentText.Alpha(0.14)
		th.QuoteBar = p.AccentText.Alpha(0.35)
		th.QuoteText = p.AccentText.Alpha(0.8)
		th.Rule = p.AccentText.Alpha(0.3)
		th.CodeBlockBackground = p.AccentText.Alpha(0.1)
		th.TableBorder = p.AccentText.Alpha(0.25)
		th.Mark = p.Accent.Alpha(0.3)
		th.Checkbox = p.AccentText
		for i := range th.Headings {
			th.Headings[i].Color = p.AccentText
		}
	}
	return th
}
