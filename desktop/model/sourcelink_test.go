package model

import "testing"

func TestSourceLabel(t *testing.T) {
	cases := []struct{ text, href, want string }{
		{"the guide", "https://www.example.com/guide", "the guide"},
		{"https://docs.rs/serde", "https://docs.rs/serde", "docs.rs"},
		{"www.example.com/a", "http://www.example.com/a", "example.com"},
		{"[1]", "https://a.dev/x", "a.dev"},
		{"2", "https://a.dev/x", "a.dev"},
		{"", "https://a.dev/x", "a.dev"},
		{"  two\n words ", "https://a.dev/x", "two words"},
		{"A very long title that would make the chip wider than a line", "https://a.dev/x", "A very long title that woul…"},
	}
	for _, c := range cases {
		if got := SourceLabel(c.text, c.href); got != c.want {
			t.Errorf("SourceLabel(%q, %q) = %q, want %q", c.text, c.href, got, c.want)
		}
	}
}

func TestSourceHost(t *testing.T) {
	cases := map[string]string{
		"https://www.Example.com/a?b=1": "example.com",
		"http://docs.rs:8080/x":         "docs.rs",
		"https://github.com":            "github.com",
		"mailto:a@b.co":                 "",
		"file:///etc/passwd":            "",
		"https:///nohost":               "",
		"lorca://chat/1":                "",
		"not a url":                     "",
	}
	for href, want := range cases {
		if got := SourceHost(href); got != want {
			t.Errorf("SourceHost(%q) = %q, want %q", href, got, want)
		}
	}
}
