package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// The pieces every coding agent's surface is drawn from, so that its card in the chat, its
// floating window, its full view, and its question read as one object: its mark on a tile in its
// own color, how it stands as a pill whose dot breathes while it works, the facts of its session as
// chips with a meter for the context it has used, and what it does now on a console of its own.

// agentBrand is an agent's own color: Claude's terracotta, OpenAI's ink for Codex, Gemini's blue,
// and the accent for the rest.
func agentBrand(p *palette, kind string) ui.Color {
	switch kind {
	case "claude", "claude-acp":
		return ui.Hex("#D97757")
	case "codex", "codex-acp":
		return p.Label
	case "gemini":
		return ui.Hex("#4C8DF6")
	}
	return p.Accent
}

// agentTile is the agent's mark on a rounded square tinted in its color.
func agentTile(c *ui.Context, p *palette, kind string, size float32) ui.Element {
	brand := agentBrand(p, kind)
	tile := ui.Box(c).Size(size, size).Shrink(0).Radius(size*0.3).Background(brand.Alpha(0.13)).Border(0.5, brand.Alpha(0.22)).Justify(ui.Center).AlignItems(ui.Center).TextColor(brand)
	tile.Children(func() {
		mark := size * 0.58
		ui.Box(c).Size(mark, mark).Children(func() { ui.Image(c, agentMark(kind)).Fill() })
	})
	return tile
}

// agentLook is how an agent's state reads: its tint and the words of its pill.
func agentLook(p *palette, agent *model.AgentRun) (ui.Color, string) {
	switch {
	case agent.Question != nil || agent.Asking != "":
		return p.Orange, Lc("Needs you", "coding agent")
	case agent.Stalled:
		return p.Orange, agent.Status()
	case agent.State == model.AgentStarting || agent.State == model.AgentChecking || agent.State == model.AgentWorking:
		return p.Accent, agent.Status()
	case agent.State == model.AgentIdle:
		return p.Green, agent.Status()
	case agent.State == model.AgentFailed:
		return p.Red, agent.Status()
	}
	return p.Label3, agent.Status()
}

// agentBusy is an agent that is doing something now.
func agentBusy(agent *model.AgentRun) bool {
	return agent.Question == nil && agent.Asking == "" && (agent.State == model.AgentStarting || agent.State == model.AgentWorking) && !agent.Stalled
}

// agentPill is how the agent stands: a capsule in its state's tint, with a dot that breathes while
// it works, and how long it has been at it.
func agentPill(c *ui.Context, p *palette, agent *model.AgentRun) ui.Element {
	tint, words := agentLook(p, agent)
	if since := agentSince(c, agent); since != "" && agentBusy(agent) {
		words += " · " + since
	}
	text := tint
	if tint == p.Label3 {
		text = p.Label2
	}
	pill := ui.Row(c.Key("pill")).Height(20).Gap(5).Padding(0, 8, 0, 7).Radius(radiusCapsule).Background(tint.Alpha(0.12)).AlignItems(ui.Center).Shrink(0)
	pill.Children(func() {
		dot := ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Background(tint)
		if agentBusy(agent) {
			breath := dot.Loop("breath", 1600*time.Millisecond, ui.EaseInOut)
			dot.Opacity(0.35 + 0.65*(1-abs32(2*breath-1)))
		}
		ui.Text(c, words).FontSize(11).FontWeight(600).FontFeatures("tnum").TextColor(text).SingleLine()
	})
	return pill
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// agentSince is how long ago it started, "4m" or "1h 12m", ticking while it works.
func agentSince(c *ui.Context, agent *model.AgentRun) string {
	if agent.StartedAt.IsZero() {
		return ""
	}
	gone := c.Now().Sub(agent.StartedAt)
	if gone < 0 {
		gone = 0
	}
	if agentBusy(agent) {
		c.After(time.Second)
	}
	switch {
	case gone < time.Minute:
		return fmt.Sprintf("%ds", int(gone.Seconds()))
	case gone < time.Hour:
		return fmt.Sprintf("%dm", int(gone.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(gone.Hours()), int(gone.Minutes())%60)
}

// agentSetting is the value of one of its settings by name: "Context", "Cost".
func agentSetting(agent *model.AgentRun, name string) string {
	for _, setting := range agent.Settings {
		if setting.Name == name {
			return setting.Value
		}
	}
	return ""
}

// agentContext is how much of its context it has used, from "34k / 200k": the share, and the words.
func agentContext(agent *model.AgentRun) (float32, string, bool) {
	words := agentSetting(agent, "Context")
	used, size, ok := strings.Cut(words, "/")
	if !ok {
		return 0, "", false
	}
	u, s := tokenCount(used), tokenCount(size)
	if s <= 0 || u < 0 {
		return 0, "", false
	}
	return float32(min(u/s, 1)), words, true
}

// tokenCount reads "34k", "1.2M", or "512".
func tokenCount(text string) float64 {
	text = strings.TrimSpace(text)
	scale := 1.0
	switch {
	case strings.HasSuffix(text, "k"):
		scale, text = 1e3, strings.TrimSuffix(text, "k")
	case strings.HasSuffix(text, "M"):
		scale, text = 1e6, strings.TrimSuffix(text, "M")
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return -1
	}
	return n * scale
}

// contextMeter is a bar of how full its context is, green, then orange past two thirds, and red
// past nine tenths, with its words beside it.
func contextMeter(c *ui.Context, p *palette, agent *model.AgentRun, width float32, words bool) {
	share, text, ok := agentContext(agent)
	if !ok {
		return
	}
	tint := p.Green
	switch {
	case share > 0.9:
		tint = p.Red
	case share > 0.66:
		tint = p.Orange
	}
	ui.Row(c.Key("context")).Gap(6).AlignItems(ui.Center).Shrink(0).Tooltip(L("Context: %@", text)).Children(func() {
		ui.Box(c).Width(width).Height(4).Radius(2).Background(p.Label4).Clip().Children(func() {
			ui.Box(c).Height(4).Width(max(width*share, 3)).Radius(2).Background(tint)
		})
		if words {
			ui.Text(c, text).FontSize(10.5).FontFeatures("tnum").TextColor(p.Label3).SingleLine()
		}
	})
}

// agentChip is a fact of its session on a quiet capsule: an icon and words.
func agentChip(c *ui.Context, p *palette, icon, text, tooltip string, mono bool) ui.Element {
	chip := ui.Row(c).Height(20).Gap(4).Padding(0, 7, 0, 6).Radius(radiusXS + 1).Background(p.Chip).AlignItems(ui.Center).MinWidth(0).Shrink(1)
	if tooltip != "" {
		chip.Tooltip(tooltip)
	}
	chip.Children(func() {
		symbol(c, icon, 11, 2).TextColor(p.Label3)
		t := ui.Text(c, text).FontSize(11).TextColor(p.Label2).SingleLine().Shrink(1).MinWidth(0)
		if mono {
			t.Font(monoFont).FontSize(10.5)
		}
	})
	return chip
}

// agentNow is what it does now: its last tool line, without the mark.
func agentNow(agent *model.AgentRun) string {
	lines := strings.Split(outputText(agent.Output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); strings.HasPrefix(line, "● ") {
			return strings.TrimPrefix(line, "● ")
		}
	}
	return ""
}

// console is the console palette: a dark well in both appearances, as a terminal is.
type console struct{ bg, edge, text, dim, mark, prompt ui.Color }

func consoleColors(c *ui.Context) console {
	if c.Theme().Dark {
		return console{ui.Hex("#0d0d0f"), ui.RGBA(255, 255, 255, 0.08), ui.Hex("#e4e4e7"), ui.RGBA(228, 228, 231, 0.45), ui.Hex("#f4f4f5"), ui.Hex("#7cb7ff")}
	}
	return console{ui.Hex("#18181b"), ui.RGBA(0, 0, 0, 0.2), ui.Hex("#e4e4e7"), ui.RGBA(228, 228, 231, 0.48), ui.Hex("#fafafa"), ui.Hex("#7cb7ff")}
}

// consoleTail draws the last `lines` of what it did on the console: what was sent in blue, each
// tool's line bright with its mark, and what came back dimmed, the oldest fading out at the top.
// With `now`, a spinner leads the line of what it does now.
func consoleTail(c *ui.Context, text string, lines int, now bool) ui.Element {
	k := consoleColors(c)
	shown := strings.Split(lastLines(text, lines), "\n")
	well := ui.Column(c.Key("console")).FillWidth().Padding(8, 10).Radius(radiusMD).Background(k.bg).Border(0.5, k.edge).Clip()
	well.Children(func() {
		last := -1
		for i, line := range shown {
			if strings.HasPrefix(line, "● ") {
				last = i
			}
		}
		for i, line := range shown {
			row := ui.Row(c.Key(i)).Gap(6).AlignItems(ui.Center).MinWidth(0)
			// The oldest lines fade, so the newest reads first.
			if fade := len(shown) - 1 - i; fade >= 3 {
				row.Opacity(0.55)
			}
			row.Children(func() {
				spinning := now && i == last
				if spinning {
					ui.Box(c).Size(10, 10).TextColor(k.mark).Children(func() { spinner(c, 10) })
					line = strings.TrimPrefix(line, "● ")
				}
				words := ui.Text(c, line).Font(monoFont).FontSize(10.5).FixedLineHeight(15).SingleLine().Shrink(1).MinWidth(0)
				switch {
				case spinning || strings.HasPrefix(line, "● "):
					words.TextColor(k.mark).FontWeight(600)
				case strings.HasPrefix(line, "> "):
					words.TextColor(k.prompt)
				default:
					words.TextColor(k.dim)
				}
			})
		}
	})
	return well
}

// agentCallout is a panel that asks something of the user or says how it ended: an icon and a
// title in its tint over a faint wash of it, then what goes under it.
func agentCallout(c *ui.Context, p *palette, tint ui.Color, icon, title string, content func()) {
	ui.Column(c.Key("callout")).FillWidth().Gap(8).Margin(12, 0, 0, 0).Padding(10, 12, 12, 12).Radius(radiusLG).Background(tint.Alpha(0.08)).Border(0.5, tint.Alpha(0.22)).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Start).MinWidth(0).Children(func() {
			symbol(c, icon, 14, 2).TextColor(tint).Margin(1, 0, 0, 0)
			ui.Text(c, title).Grow(1).Shrink(1).MinWidth(0).FontSize(12.5).FontWeight(600).LineHeight(1.35).TextColor(p.Label).MaxLines(3)
		})
		if content != nil {
			content()
		}
	})
}
