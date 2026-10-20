package main

import (
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
)

// A file output's card in the transcript shows the start of what the file holds, as a page peeking
// up from the card's top: a Markdown file's first heading and paragraph, an HTML page's title in a
// small browser window, a table's first rows, the first lines of any other text. The start is read once per file, off the
// main thread, and kept for the windows' lifetime.

const (
	// peekBytes is how much of a file the card reads.
	peekBytes = 8 << 10
	peekLines = 7
	// peekColumns is where a line of the peek is cut.
	peekColumns = 90
)

// filePeek is what the card shows of a file: a title and a paragraph for a document, lines for
// text. It is empty when the file holds no UTF-8 text.
type filePeek struct {
	title, body string
	lines       []string
	rows        [][]string
}

func (p *filePeek) empty() bool {
	return p.title == "" && p.body == "" && len(p.lines) == 0 && len(p.rows) == 0
}

// peekable is whether a kind of file shows its start on its card.
func peekable(kind model.PreviewKind) bool {
	return kind == model.PreviewMarkdown || kind == model.PreviewHTML || kind == model.PreviewText || kind == model.PreviewTable
}

type peekEntry struct {
	peek    *filePeek
	loading bool
}

var peeks = struct {
	sync.Mutex
	entries map[string]*peekEntry
}{entries: map[string]*peekEntry{}}

// loadPeek is the start of the file at path, named name, once it is read: the first ask starts reading it off
// the main thread and redraws the windows when it lands; until then it is nil.
func loadPeek(path, name string, kind model.PreviewKind) *filePeek {
	if path == "" || !peekable(kind) {
		return nil
	}
	peeks.Lock()
	entry, ok := peeks.entries[path]
	if !ok {
		entry = &peekEntry{loading: true}
		peeks.entries[path] = entry
	}
	peeks.Unlock()
	if ok {
		return entry.peek
	}
	go func() {
		peek := readPeek(path, name, kind)
		mygo.RunOnMain(func() {
			peeks.Lock()
			entry.loading, entry.peek = false, peek
			peeks.Unlock()
			invalidateWindows()
		})
	}()
	return nil
}

// readPeek reads the start of a file for its card.
func readPeek(path, name string, kind model.PreviewKind) *filePeek {
	file, err := os.Open(path)
	if err != nil {
		return &filePeek{}
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, peekBytes))
	// The cut may split the last character.
	for cut := 0; cut < utf8.UTFMax && len(data) > 0 && !utf8.Valid(data); cut++ {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return &filePeek{}
	}
	peek := peekOf(string(data), kind)
	if kind == model.PreviewTable {
		peek.rows = peekRows(string(data), strings.EqualFold(filepath.Ext(name), ".tsv"))
	}
	return peek
}

// peekRows are a table's first rows and columns, for its card.
func peekRows(text string, tabs bool) [][]string {
	rows := readTable(text, tabs, true)
	if len(rows) > 5 {
		rows = rows[:5]
	}
	for i, row := range rows {
		if len(row) > 4 {
			rows[i] = row[:4]
		}
	}
	return rows
}

var (
	htmlTitle   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlHeading = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	htmlTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	markdownRun = strings.NewReplacer("**", "", "__", "", "`", "", "~~", "")
	markdownURL = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
)

// peekOf is what the card shows of a file that starts with text.
func peekOf(text string, kind model.PreviewKind) *filePeek {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\t", "    ")
	peek := &filePeek{}
	switch kind {
	case model.PreviewHTML:
		plain := func(s string) string {
			return strings.Join(strings.Fields(html.UnescapeString(htmlTag.ReplaceAllString(s, " "))), " ")
		}
		if m := htmlTitle.FindStringSubmatch(text); m != nil {
			peek.title = plain(m[1])
		}
		if m := htmlHeading.FindStringSubmatch(text); m != nil {
			peek.body = plain(m[1])
		}
		if peek.title == "" {
			peek.title, peek.body = peek.body, ""
		}
		if peek.body == peek.title {
			peek.body = ""
		}
	case model.PreviewMarkdown:
		var paragraph []string
		inFence := false
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence || trimmed == "" || trimmed == "---" {
				if len(paragraph) > 0 && peek.title != "" {
					break
				}
				continue
			}
			clean := markdownRun.Replace(markdownURL.ReplaceAllString(trimmed, "$1"))
			if strings.HasPrefix(clean, "#") {
				heading := strings.TrimSpace(strings.TrimLeft(clean, "#"))
				if peek.title == "" && len(paragraph) == 0 {
					peek.title = heading
					continue
				}
				break
			}
			clean = strings.TrimSpace(strings.TrimLeft(clean, ">-*+ "))
			paragraph = append(paragraph, clean)
		}
		peek.body = strings.Join(paragraph, " ")
	default:
		for _, line := range strings.Split(text, "\n") {
			if len(peek.lines) == 0 && strings.TrimSpace(line) == "" {
				continue
			}
			line = strings.TrimRight(line, " ")
			if utf8.RuneCountInString(line) > peekColumns {
				line = string([]rune(line)[:peekColumns])
			}
			peek.lines = append(peek.lines, line)
			if len(peek.lines) == peekLines {
				break
			}
		}
	}
	return peek
}
