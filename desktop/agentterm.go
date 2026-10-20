package main

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
)

// A command's output in a coding agent's transcript is drawn by MyGo's terminal plugin, as its
// terminal wrote it: the agent runs on its Runner, so the terminal has no program of its own and
// shows what the app feeds it, and a click or a key reaches nothing (selecting and copying work).
// What the output adds goes on after what the terminal has, as a terminal's output does. Where the
// plugin's library cannot load, the output is plain text.

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

// agentScreen is a read-only terminal showing one command's output.
type agentScreen struct {
	term *terminal.Terminal
	// fed is what it was given, to add only what is new.
	fed []byte
}

// Reset, clear the scrollback, hide the cursor; and hide the cursor alone.
const (
	screenReset  = "\x1bc\x1b[3J\x1b[?25l"
	screenHidden = "\x1b[?25l"
)

// newAgentScreen makes a screen, or nil where the terminal cannot start, which the caller draws as
// text.
func newAgentScreen(fontSize float32) *agentScreen {
	if !agentTerminalsOn {
		return nil
	}
	term, err := terminal.New(terminal.Options{
		Conn:        newScreenConn(),
		Font:        terminal.Font{Family: monoFont, Size: fontSize, LineHeight: 1.15},
		NoBlink:     true,
		Transparent: true,
		Scrollback:  512 * 1024,
		// Links in a command's output come from the agent and what it ran: only web and mail
		// links open, as everywhere in the app.
		OpenLink: func(link string) bool { openExternal(link); return true },
	})
	if err != nil {
		return nil
	}
	term.Feed([]byte(screenHidden))
	return &agentScreen{term: term}
}

// show puts text on the screen: what it adds to what the screen has goes on after it, and other
// text replaces it.
func (s *agentScreen) show(text string) {
	b := terminalBytes(text)
	switch {
	case bytes.HasPrefix(b, s.fed):
		if len(b) > len(s.fed) {
			s.term.Feed(b[len(s.fed):])
		}
	default:
		s.term.Feed(append([]byte(screenReset), b...))
	}
	s.fed = b
}

func (s *agentScreen) close() {
	if s != nil {
		s.term.Close()
	}
}

// terminalBytes is a command's output as the terminal is fed it. The Runner keeps only its colors,
// and so does this again, since the output comes from an agent and what it ran: a color or style
// sequence (ESC [ … m) passes, any other escape sequence and every control character but a
// newline, a carriage return, or a tab is dropped, so no clipboard write, link, or title gets
// through. A line ends as a terminal's does, with a carriage return before its newline.
func terminalBytes(text string) []byte {
	var b strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == 0x1b:
			if i+1 < len(runes) && runes[i+1] == '[' {
				j := i + 2
				for j < len(runes) && (runes[j] >= '0' && runes[j] <= '9' || runes[j] == ';' || runes[j] == ':') {
					j++
				}
				if j < len(runes) && runes[j] == 'm' {
					b.WriteString(string(runes[i : j+1]))
				}
				for j < len(runes) && !(runes[j] >= 0x40 && runes[j] <= 0x7e) {
					j++
				}
				i = j
			} else if i+1 < len(runes) && strings.ContainsRune("]P_^X", runes[i+1]) {
				// A string sequence (OSC, DCS, APC, PM, SOS) runs to BEL or ST, all of it dropped.
				j := i + 2
				for j < len(runes) && runes[j] != 0x07 && !(runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\') {
					j++
				}
				if j < len(runes) && runes[j] == 0x1b {
					j++
				}
				i = j
			} else {
				i++
			}
		case r == '\n':
			if i == 0 || runes[i-1] != '\r' {
				b.WriteByte('\r')
			}
			b.WriteByte('\n')
		case r == '\r' || r == '\t':
			b.WriteRune(r)
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
		default:
			b.WriteRune(r)
		}
	}
	return []byte(b.String())
}

// outputPlain is a command's output without its colors, as plain text shows it: a line a carriage
// return rewrote reads as what it ended as.
func outputPlain(text string) string {
	var b strings.Builder
	rest := text
	for {
		at := strings.Index(rest, "\x1b[")
		if at < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:at])
		rest = rest[at+2:]
		end := strings.IndexByte(rest, 'm')
		if end < 0 {
			break
		}
		rest = rest[end+1:]
	}
	lines := strings.Split(strings.ReplaceAll(b.String(), "\r\n", "\n"), "\n")
	for i, line := range lines {
		if at := strings.LastIndexByte(line, '\r'); at >= 0 {
			lines[i] = line[at+1:]
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
