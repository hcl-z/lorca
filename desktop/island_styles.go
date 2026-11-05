package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The failed session's look: its own avatar, the bot's or the coding agent's, inside a red ring
// with a ring of the island's black between them; and its card washed in red, outlined, with a red
// pill for its word.

// islandFailedMark is a failed session's mark, `side` across: the avatar inset by the ring's width
// and its gap, and a thin, soft red ring around it that breathes slowly, from 35% to 75% over 2.4
// seconds, so a failure is noticed without alarm.
func islandFailedMark(c *ui.Context, s *islandSession, side float32) {
	ring := max(1.2, side*0.05)
	gap := max(1.5, side*0.07)
	inner := side - 2*(ring+gap)
	box := ui.Box(c).Size(side, side).Shrink(0).Justify(ui.Center).AlignItems(ui.Center)
	t := box.Loop("fail-breath", 2400*time.Millisecond, ui.Linear)
	phase := float32(0.5 - 0.5*math.Cos(float64(t)*2*math.Pi))
	box.Children(func() {
		if s.AgentKind != "" {
			agentTile(c, colors(c), s.AgentKind, inner).Radius(inner / 2)
			return
		}
		content := avatarContent{Kind: avatarSystem}
		if bot := store.Bot(s.BotID); bot != nil {
			content = botAvatar(bot)
		}
		avatar(c, content, inner, false)
	})
	box.DrawOver(func(p *ui.Painter, r ui.Rect) {
		var circle ui.Path
		circle.Circle(r.X+r.W/2, r.Y+r.H/2, r.W/2-ring/2)
		p.StrokePath(&circle, ring, islandSoftRed.Alpha(0.35+0.40*phase))
	})
}

// islandSoftRed is the failure's red on the island's black: lighter than the alert red, so a mark
// or a word reads as red without glaring.
var islandSoftRed = ui.RGBA(248, 113, 113, 1)

// islandFailCard is a failed card's fill and outline: a faint red wash and outline.
func islandFailCard() (fill ui.Color, base, stroke float32) {
	return islandSoftRed, 0.06, 0.16
}

// islandFailLabel is the word at a failed row's right: a red pill, as a question's Review is a
// blue one.
func islandFailLabel(c *ui.Context, label string) {
	ui.Row(c).Height(20).Padding(0, 8).Radius(10).Background(islandSoftRed.Alpha(0.12)).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(islandSoftRed.Alpha(0.9)).SingleLine()
	})
}
