package model

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// Favicon asks the CLI for a site's icon: its image bytes, or nil when the site has none, in
// `done` on the main thread. The demo has no CLI to ask: a few of its sites have their real icon in the app, the rest none.
func (s *Store) Favicon(host string, done func(image []byte)) {
	if s.IsMock {
		done(mockFavicon(host))
		return
	}
	Async(s, func() ([]byte, error) {
		reply, err := call[struct {
			Icon *struct {
				Data string `json:"data"`
			} `json:"icon"`
		}](s, "links.favicon", map[string]any{"host": host})
		if err != nil || reply.Icon == nil {
			return nil, err
		}
		data, err := base64.StdEncoding.DecodeString(reply.Icon.Data)
		if err != nil {
			return nil, err
		}
		return FaviconImage(data)
	}, func(image []byte, err error) { done(image) })
}

// SourceHost is the host a web link is shown under on its source chip, without a leading `www.`.
// It is empty for a link that is not http(s) with a host, which stays plain link text.
func SourceHost(href string) string {
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	return strings.TrimPrefix(host, "www.")
}

// sourceLabelRunes is the longest label a source chip shows before it ends in an ellipsis.
const sourceLabelRunes = 28

// SourceLabel is the words on a source chip: the link's own words when they say more than its
// address does, else its site. A bare address, a number such as [1], and an empty text all read
// as the site.
func SourceLabel(text, href string) string {
	host := SourceHost(href)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || text == href || SourceHost(text) != "" || strings.HasPrefix(strings.ToLower(text), "www.") || isCitationMark(text) {
		return host
	}
	if runes := []rune(text); len(runes) > sourceLabelRunes {
		return strings.TrimRight(string(runes[:sourceLabelRunes-1]), " ") + "…"
	}
	return text
}

// isCitationMark is a link's text that is only a number, such as [1] or 1.
func isCitationMark(text string) bool {
	text = strings.Trim(text, "[]() ")
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
