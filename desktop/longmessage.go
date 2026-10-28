package main

import (
	"strings"
	"unicode"
)

const (
	// foldedHeight is how tall a message's text stands before its Show more.
	foldedHeight = 340
	// foldedChars is the most Markdown a folded message lays out, in runes.
	foldedChars = 1400
	// foldedLineHeight is the line height the Markdown view gives text, over its size.
	foldedLineHeight = 1.42
)

// foldedText is what a message's text comes to for a folded bubble: its head and whether it is
// longer than a folded one shows. It is kept by the view between frames, so a long message is
// measured once, not on every frame it is drawn.
type foldedText struct {
	text, prefix string
	folded       bool
}

// foldText lays a message's folding out: whether it stands taller than height at width, and the
// head it lays out while folded.
func foldText(text string, width, size, height float32) foldedText {
	return foldedText{text: text, prefix: foldedPrefix(text), folded: foldsAt(text, width, size, height)}
}

// foldsAt reports whether text stands taller than height at width and size, estimated from its
// characters: laying the whole of a long message out is what folding exists to avoid.
func foldsAt(text string, width, size, height float32) bool {
	perLine := max(int(width/(size*0.55)), 6)
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			lines++
			continue
		}
		units := 0
		for _, r := range line {
			units += runeRoom(r)
		}
		lines += max((units+perLine-1)/perLine, 1)
	}
	return float32(lines)*size*foldedLineHeight > height
}

// runeRoom is a character's room in a line: CJK, full-width, and emoji take about two Latin
// letters.
func runeRoom(r rune) int {
	switch {
	case r >= 0x1100 && (r <= 0x115f ||
		r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd)):
		return 2
	}
	return 1
}

// foldedPrefix is the head of text a folded message lays out: cut at a line where it can be, and
// a code fence left open closed, so the tail reads as the code it is.
func foldedPrefix(text string) string {
	runes := []rune(text)
	if len(runes) <= foldedChars {
		return text
	}
	cut := foldedChars
	// The last line break near the limit, rather than a cut through a word.
	for at := foldedChars; at > foldedChars*6/10; at-- {
		if runes[at] == '\n' {
			cut = at
			break
		}
	}
	prefix := strings.TrimRightFunc(string(runes[:cut]), unicode.IsSpace)
	if openFences(prefix)%2 == 1 {
		prefix += "\n```"
	}
	return prefix
}

// openFences counts the lines that open or close a code fence.
func openFences(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			count++
		}
	}
	return count
}
