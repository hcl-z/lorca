package main

import (
	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A message's toolbar: Reply, Copy, and Edit or Regenerate on a small floating pill beside
// the bubble, shown while the pointer is over the message. It is laid out of the flow, absolute in
// the message's row, so showing it moves nothing, and placeActions keeps it clear of what it would
// cover: the transcript's edges, the header above, and the composer below.

const (
	// actionsGap is the room between the bubble and its toolbar.
	actionsGap = 6
	// actionsEdge is how close the toolbar comes to the transcript's sides and its safe area.
	actionsEdge = 4
)

// messageActions is the toolbar's pill. The caller places it.
func (m *mainWindow) messageActions(c *ui.Context, chatID string, message *model.Message, s *chatState, text string) ui.Element {
	p := colors(c)
	pill := ui.Row(c.Key("actions:"+message.ID)).Padding(3).Gap(2).Radius(radiusCapsule).Background(p.Popover).
		Border(1, p.CardBorder).Shadow(0, 2, 10, 0, p.Shadow.Alpha(0.35)).Label(L("Message actions")).Role(ui.RoleToolbar).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0, Scale: 0.96}, Duration: 120_000_000})
	pill.Children(func() {
		if message.CanBeQuoted() {
			if hoverButton(c, hoverButtonOptions{Symbol: "arrowshape.turn.up.left", Size: 14, Tooltip: L("Reply")}).Height(26).MinWidth(26).Radius(radiusCapsule).Clicked() {
				m.startReply(s, message)
			}
		}
		if text != "" {
			copyButton(c, text, copyOptions{Symbol: "doc.on.doc", Tooltip: L("Copy message")}).Height(26).MinWidth(26).Radius(radiusCapsule)
		}
		// The latest exchange of a direct chat runs again: Edit on the user's message, Regenerate on
		// the bot's reply to it.
		if store.CanEdit(chatID, message) {
			if hoverButton(c, hoverButtonOptions{Symbol: "pencil", Size: 14, Tooltip: L("Edit")}).Height(26).MinWidth(26).Radius(radiusCapsule).Clicked() {
				s.composer.startEdit(message)
			}
		}
		if store.CanRegenerate(chatID, message) {
			if hoverButton(c, hoverButtonOptions{Symbol: "arrow.triangle.2.circlepath", Size: 14, Tooltip: L("Regenerate")}).Height(26).MinWidth(26).Radius(radiusCapsule).Clicked() {
				if asked, _ := store.Rerunnable(chatID); asked != nil {
					s.list.ScrollToEnd()
					store.Rerun(chatID, asked.ID, "", func(err error) {
						if err != nil {
							s.stoppedNotice = model.ErrorText(err)
						}
					})
				}
			}
		}
	})
	return pill
}

// actionsSize is the pill's size as the last frame laid it out, or its size with three buttons
// on the frame it first shows, so the first placement is close.
func actionsSize(pill ui.Element) actionsSizeOf {
	if b := pill.Bounds(); b.W > 0 {
		return actionsSizeOf{W: b.W, H: b.H}
	}
	return actionsSizeOf{W: 3*26 + 2*2 + 6 + 2, H: 26 + 6 + 2}
}

// safeArea is the part of the transcript nothing covers: below the header, above the composer.
func (s *chatState) safeArea() ui.Rect {
	v := s.viewport
	top := v.Y + headerHeight
	bottom := v.Y + v.H - s.composerHeight
	return ui.Rect{X: v.X, Y: top, W: v.W, H: max(0, bottom-top)}
}

// placeActions is where a message's toolbar goes, relative to its row, every box in the window's
// coordinates. In order of preference:
//
//  1. Beside the bubble, at its trailing side for a bot's message (which sits at the leading
//     edge) and its leading side for the user's, its top level with the bubble's top.
//  2. Without room beside it (a wide bubble, a narrow window), over the bubble's top corner on
//     that side, straddling its edge, as a floating bar does.
//
// Then, vertically, it stays inside the safe area: a bubble whose top has scrolled under the header
// takes the toolbar at its bottom instead, and the toolbar never goes past the composer's top. It
// is clamped to the transcript's sides last, so it is always on screen.
func placeActions(row, bubble ui.Rect, size actionsSizeOf, isUser bool, safe ui.Rect) ui.Point {
	left, right := safe.X+actionsEdge, safe.X+safe.W-actionsEdge
	var x float32
	beside := true
	if isUser {
		x = bubble.X - actionsGap - size.W
		if x < left {
			beside, x = false, bubble.X+actionsGap
		}
	} else {
		x = bubble.X + bubble.W + actionsGap
		if x+size.W > right {
			beside, x = false, bubble.X+bubble.W-actionsGap-size.W
		}
	}
	y := bubble.Y
	if !beside {
		// Over the corner: half above the bubble's top edge.
		y = bubble.Y - size.H/2
	}
	top, bottom := safe.Y+actionsEdge, safe.Y+safe.H-actionsEdge
	if y < top {
		// The top is under the header: the bubble's bottom, beside it or straddling it.
		if beside {
			y = bubble.Y + bubble.H - size.H
		} else {
			y = bubble.Y + bubble.H - size.H/2
		}
	}
	y = min(max(y, top), bottom-size.H)
	x = min(max(x, left), right-size.W)
	return ui.Point{X: x - row.X, Y: y - row.Y}
}

// actionsSizeOf is the toolbar's width and height.
type actionsSizeOf struct{ W, H float32 }
