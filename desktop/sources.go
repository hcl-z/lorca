package main

import (
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A web link in a message reads as a source chip: the site's icon and the link's words on a
// rounded fill, inside the paragraph. A paragraph holds only text, so the chip is text with room
// left at its start; the paragraph paints the fill under it and the icon in that room, where the
// chip's words were laid out.

const (
	sourceIconSize = 14
	sourceChipPad  = 7 // from the chip's start to its icon
	sourceChipGrow = 1 // the fill's reach above and below the words
	// sourceChipDrop is how far the fill's and the icon's center sit below the center of the line's
	// box: the words' weight is in the x-height, under the middle of a line that also holds the
	// room for ascenders and the leading.
	sourceChipDrop = 1.0
)

// chipWords is a chip's text: a no-break space and room for the icon (two figure spaces) ahead of
// the label, and the same behind it, so the fill has air around the icon and the label. The label's
// own spaces do not break, so the icon never parts from it.
func chipWords(label string) string {
	return "\u00a0\u00a0\u2007\u2007\u00a0" + strings.ReplaceAll(label, " ", "\u00a0") + "\u00a0\u00a0"
}

type faviconEntry struct{ bitmap *ui.Bitmap }

// favicons are the sites asked about this session, whether or not they had an icon. The CLI keeps
// the icons themselves, so a site is asked about once per run. Main thread only.
var favicons = map[string]*faviconEntry{}

// favicon is the icon of a site once the CLI has found it: the first ask starts the lookup and
// redraws the windows when it lands; until then, and for a site without one, it is nil.
func favicon(host string) *ui.Bitmap {
	if entry, ok := favicons[host]; ok {
		return entry.bitmap
	}
	entry := &faviconEntry{}
	favicons[host] = entry
	if store == nil {
		return nil
	}
	store.Favicon(host, func(image []byte) {
		if image == nil {
			return
		}
		if bitmap, err := ui.DecodeBitmap(image); err == nil {
			entry.bitmap = bitmap
			invalidateWindows()
		}
	})
	return nil
}

// joinLinkSpans makes the runs of one web link, which styled words inside it split, one span, so
// the link is one chip.
func joinLinkSpans(spans []model.Span) []model.Span {
	var out []model.Span
	for _, span := range spans {
		if n := len(out); n > 0 && span.Link != "" && out[n-1].Link == span.Link && model.SourceHost(span.Link) != "" {
			out[n-1].Text += span.Text
			continue
		}
		out = append(out, span)
	}
	return out
}

// sourceChip is a chip's words in a paragraph and the site whose icon belongs in front of them.
type sourceChip struct {
	text ui.Handle
	host string
}

// sourceChipText builds the words of a chip in the paragraph being built, and is clicked as a
// link is. The label stays on one line: its spaces do not break, so the icon never parts from it.
func sourceChipText(c *ui.Context, span model.Span, o markdownOptions, bold bool) *sourceChip {
	p := colors(c)
	host := model.SourceHost(span.Link)
	words := ui.Text(c, chipWords(model.SourceLabel(span.Text, span.Link))).FontSize(o.Size - 1.5).TextColor(p.Label).
		Cursor(ui.CursorPointer).Tooltip(span.Link)
	if bold {
		words.FontWeight(600)
	}
	if words.Clicked() {
		openLink(span.Link)
	}
	chip := &sourceChip{host: host}
	words.Bind(&chip.text)
	// Bounds are the last frame's: a chip new to the screen is painted once it has been laid out.
	if words.Bounds().W == 0 {
		c.Invalidate()
	}
	return chip
}

// paintSourceChips paints the chips of a paragraph: the fill, then the icon, or a globe for a
// site without one. It runs under the paragraph's words.
func paintSourceChips(c *ui.Context, chips []*sourceChip) func(*ui.Painter, ui.Rect) {
	p := colors(c)
	return func(painter *ui.Painter, _ ui.Rect) {
		for _, chip := range chips {
			b := chip.text.Bounds(c)
			if b.W == 0 {
				continue
			}
			// A white (dark: deep) fill on a hairline stands out of a bubble's gray, as a card does.
			fill := ui.Rect{X: b.X + 1, Y: b.Y + sourceChipDrop - sourceChipGrow, W: b.W - 2, H: b.H + 2*sourceChipGrow}
			painter.Fill(fill, p.Card, fill.H/2)
			painter.Stroke(fill, p.CardBorder, fill.H/2, 1)
			icon := ui.Rect{X: b.X + sourceChipPad, Y: b.Y + (b.H-sourceIconSize)/2 + sourceChipDrop*1.5, W: sourceIconSize, H: sourceIconSize}
			if bitmap := favicon(chip.host); bitmap != nil {
				painter.Clip(icon, 3, func() { painter.Image(bitmap, icon, ui.Cover) })
			} else {
				painter.Icon(symbolSVG("globe", 1.6), icon, p.Label2)
			}
		}
	}
}
