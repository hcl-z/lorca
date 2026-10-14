package model

import (
	"slices"
	"testing"
)

func TestSplitMentionsMakesEachNamedBotARunOfItsOwn(t *testing.T) {
	scout, writer := &Bot{ID: "b2", Name: "Scout"}, &Bot{ID: "b3", Name: "Writer"}
	bots := []*Bot{scout, writer}
	words := func(spans []Span) []string {
		out := []string{}
		for _, span := range spans {
			text := span.Text
			if span.Mention != "" {
				text = "[" + span.Mention + ":" + text + "]"
			}
			out = append(out, text)
		}
		return out
	}

	got := words(SplitMentions([]Span{{Text: "Stop here. @scout wraps up, then @Writer drafts. Thanks!", Bold: true}}, bots))
	want := []string{"Stop here. ", "[b2:@scout]", " wraps up, then ", "[b3:@Writer]", " drafts. Thanks!"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	// The style of the run goes with each piece.
	for _, span := range SplitMentions([]Span{{Text: "@Scout go", Bold: true}}, bots) {
		if !span.Bold {
			t.Errorf("%q lost its style", span.Text)
		}
	}

	// Left as written: an email address, a longer word, code, a link, and a name two bots share.
	for _, span := range []Span{
		{Text: "write to ops@scout.dev"},
		{Text: "@Scouting is another word"},
		{Text: "@Scout", Code: true},
		{Text: "@Scout", Link: "https://example.com"},
	} {
		if got := SplitMentions([]Span{span}, bots); len(got) != 1 || got[0] != span {
			t.Errorf("%+v became %+v", span, got)
		}
	}
	twins := []*Bot{scout, {ID: "b4", Name: "scout"}}
	if got := SplitMentions([]Span{{Text: "@Scout go"}}, twins); len(got) != 1 || got[0].Mention != "" {
		t.Errorf("a shared name is ambiguous: %+v", got)
	}
	// Two mentions side by side, and a name with a hyphen, as bots are named.
	got = words(SplitMentions([]Span{{Text: "@browser-operator @Scout"}}, []*Bot{{ID: "b5", Name: "browser-operator"}, scout}))
	if want := []string{"[b5:@browser-operator]", " ", "[b2:@Scout]"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
