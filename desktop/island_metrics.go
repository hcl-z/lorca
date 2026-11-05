package main

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The island's geometry and words, ported one to one from Cindy's Agent Island (makecindy/cindy,
// Apache-2.0): the SwiftUI helper apps/desktop/native/agent-island/macos-agent-island-helper.swift
// and its shared rules in apps/desktop/src/shared/agentIsland.ts. The numbers are Cindy's; the
// names say where each one is used.

const (
	// The compact island: at least this wide, and the extra width beside the notch.
	islandCompactBaseWidth         = 210
	islandCompactMinContentWidth   = 80
	islandCompactHardwareIdleExtra = 64
	islandCompactHardwareBusyExtra = 64
	islandCompactSimulatedBusy     = 88
	// The badge is hidden below this width of the island, the line's detail below the next.
	islandBadgeCollapseWidth  = 152
	islandDetailCollapseWidth = 190
	// The compact title line.
	islandStatusIcon          = 18
	islandTitleMinCJK         = 32
	islandTitleMinLatin       = 40
	islandTitleMaxFraction    = 0.44
	islandDetailMinWidth      = 64
	islandNotchRevealStart    = 44
	islandNotchRevealDistance = 38
	islandNotchBadgeInset     = 9
	// The expanded island.
	islandExpandedMinWidth        = 360
	islandExpandedDefaultWidth    = 440
	islandExpandedIdleWidth       = 340
	islandExpandedMaxWidth        = 920
	islandExpandedNotchSide       = 96
	islandExpandedNotchPadding    = 36
	islandScreenEdgeGutter        = 112
	islandExpandedMeasuredMin     = 118
	islandExpandedMaxHeight       = 560
	islandExpandedIdleHeight      = 154
	islandExpandedListMinHeight   = 176
	islandExpandedSingleHeight    = 128
	islandExpandedApprovalHeight  = 164
	islandMaxVisibleRows          = 5
	islandRowHeight               = 64
	islandRowGap                  = 6
	islandRowSlot                 = islandRowHeight + islandRowGap
	islandListVerticalPadding     = 60
	islandCardHorizontalPadding   = 18
	islandTopBarHeight            = 24
	islandTopPadding              = 7
	islandBodySpacing             = 4
	islandBottomPadding           = 16
	islandRowLeading              = 32
	islandExpandedContentPadding  = 38
	islandCollapseTrailingReserve = 100
	// The shape: how far the top edge reaches past the sides into the menu bar, and the bottom
	// corners' radius, closed or peeking and expanded.
	islandClosedTopExtension   = 3
	islandClosedBottomRadius   = 12
	islandExpandedTopExtension = 14
	islandExpandedBottomRadius = 24
	// The simulated notch of a display without one.
	islandSimulatedNotchRatio = 0.14
	islandSimulatedNotchMin   = 160
	islandSimulatedNotchMax   = 240
	// islandFallbackTopBar is the top bar's height when the display says nothing of a notch or a
	// menu bar.
	islandFallbackTopBar = 24
	// Text limits.
	islandCompactTitleMax = 28
	islandDetailMax       = 120
	islandToolDetailMax   = 80
	islandActivityMax     = 280
	islandActivityLines   = 3
)

// islandScreen is a display as the island lays itself out on it: whether it has a notch and how wide
// it is, and the height of the strip at its top the compact island fills.
type islandScreen struct {
	Width      float32
	HasNotch   bool
	NotchWidth float32
	TopBar     float32
}

// islandSimulatedNotchWidth is the notch a display without one gets: a share of its width, held
// between two bounds.
func islandSimulatedNotchWidth(screenWidth float32) float32 {
	return min(max(screenWidth*islandSimulatedNotchRatio, islandSimulatedNotchMin), islandSimulatedNotchMax)
}

// withoutNotch is the screen laid out as one without a notch: the simulated notch's width.
func (s islandScreen) withoutNotch() islandScreen {
	return islandScreen{Width: s.Width, NotchWidth: islandSimulatedNotchWidth(s.Width), TopBar: s.TopBar}
}

// islandScreenOf is a display's layout, from what the platform reads of its notch.
func islandScreenOf(width float32, notch islandNotch) islandScreen {
	top := notch.TopBar
	if top <= 5 {
		top = islandFallbackTopBar
	}
	if notch.Width > 0 {
		return islandScreen{Width: width, HasNotch: true, NotchWidth: notch.Width, TopBar: top}
	}
	return islandScreen{Width: width, NotchWidth: islandSimulatedNotchWidth(width), TopBar: top}
}

// islandBadgeRun is one colored run of the compact badge.
type islandBadgeRun struct {
	Text string
	Tone islandBadgeTone
}

type islandBadgeTone int

const (
	islandBadgeAttention islandBadgeTone = iota
	islandBadgeDot
	islandBadgeRunning
	islandBadgeCount
)

// islandBadgeRuns are the badge's runs, as Cindy's getAgentIslandCompactBadgeSegments: what waits
// on the user, a dot, and what runs; one of them alone; or the number of sessions.
func islandBadgeRuns(p islandPill) []islandBadgeRun {
	switch {
	case p.Attention > 0 && p.Running > 0:
		return []islandBadgeRun{{itoa(p.Attention), islandBadgeAttention}, {"·", islandBadgeDot}, {itoa(p.Running), islandBadgeRunning}}
	case p.Attention > 0:
		return []islandBadgeRun{{itoa(p.Attention), islandBadgeAttention}}
	case p.Running > 0:
		return []islandBadgeRun{{itoa(p.Running), islandBadgeRunning}}
	}
	return []islandBadgeRun{{itoa(max(1, p.Sessions)), islandBadgeCount}}
}

// islandBadgeWidth is the width the badge takes, from its characters rather than measured, as both
// sides of Cindy compute it: seven points a character and one of slack per run, a point between
// runs, two of inset each side, and at least 22, or 30 for a pair (24 and 34 outside the compact
// island).
func islandBadgeWidth(p islandPill, compact bool) float32 {
	runs := islandBadgeRuns(p)
	width := float32(0)
	for _, r := range runs {
		width += float32(utf8.RuneCountInString(r.Text))*7 + 1
	}
	width += float32(len(runs)-1) + 2*2
	least := float32(22)
	switch {
	case len(runs) > 1 && compact:
		least = 30
	case len(runs) > 1:
		least = 34
	case !compact:
		least = 24
	}
	return max(least, width)
}

// islandCompactWidth is the compact island's width: around the notch with room for the badge on both
// sides, or the simulated notch and a little more while something shows.
func islandCompactWidth(s islandScreen, hasSession bool, badge float32) float32 {
	if s.HasNotch {
		extra := float32(islandCompactHardwareIdleExtra)
		if hasSession {
			extra = max(float32(islandCompactHardwareBusyExtra), (badge+islandNotchBadgeInset)*2)
		}
		return max(islandCompactBaseWidth, s.NotchWidth+extra)
	}
	extra := float32(0)
	if hasSession {
		extra = islandCompactSimulatedBusy
	}
	return max(islandCompactBaseWidth, s.NotchWidth+extra)
}

// islandExpandedWidth is the expanded island's width, within what the display leaves.
func islandExpandedWidthFor(s islandScreen, hasSession bool, available float32) float32 {
	width := float32(islandExpandedIdleWidth)
	if hasSession {
		width = islandExpandedDefaultWidth
	}
	return min(width, available)
}

// islandExpandedHeightFor is the expanded island's height: the idle panel, the content's measured
// height, the list's rows, or one card, with room for its approval buttons.
func islandExpandedHeightFor(sessions int, list bool, measured float32, approval bool) float32 {
	switch {
	case sessions == 0:
		return islandExpandedIdleHeight
	case measured > 0:
		return min(islandExpandedMaxHeight, max(islandExpandedMeasuredMin, float32(math.Ceil(float64(measured+8)))))
	case list && sessions > 1:
		rows := min(sessions, islandMaxVisibleRows)
		return min(islandExpandedMaxHeight, max(islandExpandedListMinHeight, islandListVerticalPadding+float32(rows)*islandRowSlot))
	case approval:
		return islandExpandedApprovalHeight
	}
	return islandExpandedSingleHeight
}

// islandNotchTextOpacity fades a side's words in as the side widens past the notch.
func islandNotchTextOpacity(side float32) float32 {
	return min(1, max(0, (side-islandNotchRevealStart)/islandNotchRevealDistance))
}

// islandNotchSideInset is the inset of a side's content from the island's edge.
func islandNotchSideInset(side, content float32) float32 {
	return max(7, (min(side, 40)-content)/2)
}

// islandTitleMinWidth is the narrowest a title shrinks: less for words in CJK, which say more in
// fewer characters.
func islandTitleMinWidth(title string) float32 {
	for _, r := range title {
		if r >= 0x3400 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0x3040 && r <= 0x30FF || r >= 0xAC00 && r <= 0xD7AF {
			return islandTitleMinCJK
		}
	}
	return islandTitleMinLatin
}

// islandTitleMaxWidth is the widest the compact title grows beside its detail: under a share of the
// line, and leaving the detail its least.
func islandTitleMaxWidth(container, spacing float32, title string) float32 {
	available := max(0, container-islandStatusIcon-spacing*2)
	least := min(islandTitleMinWidth(title), available)
	detail := min(float32(islandDetailMinWidth), max(0, available-least))
	return min(max(least, available*islandTitleMaxFraction), max(least, available-detail))
}

// islandTruncate shortens text to at most max characters, an ellipsis of three dots last, after
// joining its whitespace.
func islandTruncate(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimRightFunc(string(runes[:max(0, limit-3)]), unicode.IsSpace) + "..."
}

// islandCompactText is text on one line: its lines trimmed, the empty ones dropped, joined by spaces.
func islandCompactText(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, " ")
}

// islandStripPrompt drops a leading "$ " or "> ", which the summary line does without.
func islandStripPrompt(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "$ ") || strings.HasPrefix(text, "> ") {
		return text[2:]
	}
	return text
}

// islandGenericDetail is a detail that says nothing the state does not: a generic running status.
func islandGenericDetail(text string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(text), ".")) {
	case "generating", "thinking", "running", "still running":
		return true
	}
	return false
}

// islandActivityText bounds streamed text for the activity line: on one line, its whitespace
// joined, at most 280 characters.
func islandActivityText(text string) string {
	text = strings.Join(strings.Fields(islandCompactText(text)), " ")
	if runes := []rune(text); len(runes) > islandActivityMax {
		text = strings.TrimSpace(string(runes[:islandActivityMax]))
	}
	return text
}

// islandToolText is a tool call's line: its description beside the command it runs, the command
// alone, or what the call says it does.
func islandToolText(description, command, summary string) string {
	description = strings.TrimSpace(description)
	command = strings.TrimSpace(firstLine(command))
	switch {
	case command != "" && description != "" && !strings.Contains(description, command):
		return islandTruncate(description, islandToolDetailMax) + " · $ " + command
	case command != "" && description == "":
		return "$ " + command
	case description != "":
		return islandTruncate(description, islandToolDetailMax)
	}
	return islandTruncate(summary, islandToolDetailMax)
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
