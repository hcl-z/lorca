package model

import (
	"strings"
	"unicode"
)

// SplitMentions cuts the plain text of `spans` at every `@Name` of one of `bots`, so a run that
// names a bot is a span of its own, with the bot's id in Mention: a group member's message that
// hands a part to another member reads it as a chip. Code and links stay as they are, and a name
// two of `bots` share is left alone, since the text does not say which one.
func SplitMentions(spans []Span, bots []*Bot) []Span {
	var named []*Bot
	for _, bot := range bots {
		if bot == nil || bot.Name == "" {
			continue
		}
		shared := false
		for _, other := range bots {
			if other != nil && other != bot && strings.EqualFold(other.Name, bot.Name) {
				shared = true
			}
		}
		if !shared {
			named = append(named, bot)
		}
	}
	if len(named) == 0 {
		return spans
	}
	var out []Span
	for _, span := range spans {
		if span.Code || span.Link != "" || span.Mention != "" || !strings.Contains(span.Text, "@") {
			out = append(out, span)
			continue
		}
		text := span.Text
		for text != "" {
			at, bot := nextMention(text, named)
			if bot == nil {
				break
			}
			if at > 0 {
				before := span
				before.Text = text[:at]
				out = append(out, before)
			}
			end := at + 1 + len(bot.Name)
			mention := span
			mention.Text, mention.Mention = text[at:end], bot.ID
			out = append(out, mention)
			text = text[end:]
		}
		if text != "" {
			rest := span
			rest.Text = text
			out = append(out, rest)
		}
	}
	return out
}

// nextMention is where the first `@Name` of `bots` starts in text, and whose it is; the longest
// name wins where two start together. The bot is nil when none does.
func nextMention(text string, bots []*Bot) (int, *Bot) {
	best, found := -1, (*Bot)(nil)
	for _, bot := range bots {
		at := mentionIndex(text, bot.Name)
		if at < 0 {
			continue
		}
		if best < 0 || at < best || (at == best && len(bot.Name) > len(found.Name)) {
			best, found = at, bot
		}
	}
	return best, found
}

// mentionIndex is where `@name` first stands in text as a word of its own, ignoring case: not
// inside an email address and not the start of a longer name. It is -1 when it does not.
func mentionIndex(text, name string) int {
	if name == "" {
		return -1
	}
	for from := 0; from < len(text); {
		offset := strings.IndexByte(text[from:], '@')
		if offset < 0 {
			return -1
		}
		at := from + offset
		from = at + 1
		rest := text[at+1:]
		if len(rest) < len(name) || !strings.EqualFold(rest[:len(name)], name) {
			continue
		}
		if before := []rune(text[:at]); len(before) > 0 && isWordRune(before[len(before)-1]) {
			continue
		}
		if after := []rune(rest[len(name):]); len(after) > 0 && (isWordRune(after[0]) || after[0] == '_') {
			continue
		}
		return at
	}
	return -1
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
