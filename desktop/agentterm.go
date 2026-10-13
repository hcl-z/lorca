package main

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// A coding agent's output is drawn by MyGo's terminal plugin: the card's last lines while it works
// and the transcript sheet. The agent runs on its Runner, so the terminal has no program of its
// own; it shows what the app feeds it, the transcript as the CLI sends it, and a click or a key
// reaches nothing (selecting and copying work). Where the plugin's library cannot load, the views
// fall back to plain text.

// agentTerminalsOn lets tests that cannot fetch the plugin's native library draw the text fallback.
var agentTerminalsOn = true

// screenConn is the terminal's connection to nothing: reads wait until it closes, and what is typed
// goes nowhere.
type screenConn struct {
	once   sync.Once
	closed chan struct{}
}

func newScreenConn() *screenConn { return &screenConn{closed: make(chan struct{})} }

func (c *screenConn) Read([]byte) (int, error)    { <-c.closed; return 0, io.EOF }
func (c *screenConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *screenConn) Close() error                { c.once.Do(func() { close(c.closed) }); return nil }

// agentScreen is a read-only terminal showing one agent's transcript or last lines.
type agentScreen struct {
	term *terminal.Terminal
	text string
	used time.Time
}

// newAgentScreen makes a screen, or nil where the terminal cannot start, which the caller draws as
// text.
func newAgentScreen(fontSize float32) *agentScreen {
	if !agentTerminalsOn {
		return nil
	}
	term, err := terminal.New(terminal.Options{
		Conn:        newScreenConn(),
		Font:        terminal.Font{Size: fontSize},
		NoBlink:     true,
		Transparent: true,
		// Links in a transcript come from the agent and the pages it read: only web and mail links
		// open, as everywhere in the app.
		OpenLink: func(link string) bool { openExternal(link); return true },
	})
	if err != nil {
		return nil
	}
	return &agentScreen{term: term, used: time.Now()}
}

// show replaces the screen's text when it changed.
func (s *agentScreen) show(text string) {
	s.used = time.Now()
	if text == s.text {
		return
	}
	s.text = text
	s.term.Feed(screenBytes(text))
}

func (s *agentScreen) close() {
	if s != nil {
		s.term.Close()
	}
}

// screenBytes is the escape sequences that clear the terminal and write the transcript on it. The
// text comes from an agent and what it read, so every control character is dropped before the
// terminal sees it: only the styling added here reaches the emulator, and no sequence an agent
// printed (a clipboard write, a link, a title) does anything.
func screenBytes(text string) []byte {
	var b strings.Builder
	// Reset, clear the scrollback, hide the cursor.
	b.WriteString("\x1bc\x1b[3J\x1b[?25l")
	for i, line := range strings.Split(plainText(text), "\n") {
		if i > 0 {
			b.WriteString("\r\n")
		}
		style := ""
		switch {
		case strings.HasPrefix(line, "> "):
			style = "\x1b[36m"
		case strings.HasPrefix(line, "● "):
			style = "\x1b[1m"
		case strings.HasPrefix(line, "  ⎿"):
			style = "\x1b[2m"
		}
		b.WriteString(style)
		b.WriteString(line)
		if style != "" {
			b.WriteString("\x1b[0m")
		}
	}
	return []byte(b.String())
}

// plainText drops the control characters of text, other than newlines, and turns tabs into spaces.
func plainText(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
			return -1
		}
		return r
	}, strings.ReplaceAll(text, "\r\n", "\n"))
}

// cardScreens are the terminals of the cards whose agents are at work, by message id. A card whose
// agent stops working, and one nobody has drawn for a while, gives its terminal up.
var cardScreens = map[string]*agentScreen{}

const cardScreenIdle = 30 * time.Second

// cardScreen is the message's terminal, made when first drawn.
func cardScreen(id string) *agentScreen {
	if s, ok := cardScreens[id]; ok {
		return s
	}
	for other, s := range cardScreens {
		if time.Since(s.used) > cardScreenIdle {
			s.close()
			delete(cardScreens, other)
		}
	}
	s := newAgentScreen(11)
	if s != nil {
		cardScreens[id] = s
	}
	return s
}

func releaseCardScreen(id string) {
	if s, ok := cardScreens[id]; ok {
		s.close()
		delete(cardScreens, id)
	}
}

// agentScreenView draws text on a terminal of the height given, or as text where there is none.
func agentScreenView(c *ui.Context, s *agentScreen, text string, height float32) ui.Element {
	p := colors(c)
	s.show(text)
	box := ui.Box(c).FillWidth().Height(height).Padding(5, 8).Radius(radiusSM).Background(p.Code).Clip()
	box.Children(func() { terminal.View(c, s.term).Fill() })
	return box
}
