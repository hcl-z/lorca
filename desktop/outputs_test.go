package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type outputTestTransport struct {
	request func(string, any) (json.RawMessage, error)
}

func (f outputTestTransport) Request(method string, params any) (json.RawMessage, error) {
	return f.request(method, params)
}
func (outputTestTransport) Reconnect() {}

// outputFixture is a chat with outputs a fake CLI lists and serves: a link check in two versions,
// an after screenshot, a document link, and a screenshot the relay no longer has.
type outputFixture struct {
	t        *testing.T
	posts    chan func()
	files    map[string]string
	messages []map[string]any
	mu       sync.Mutex
	fetches  map[string]int
	named    []string
}

func newOutputFixture(t *testing.T) *outputFixture {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	picture := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := range 360 {
		for x := range 640 {
			value := color.RGBA{247, 247, 247, 255}
			if y < 60 {
				value = color.RGBA{41, 92, 242, 255}
			} else if y > 150 && y < 166 && x > 24 && x < 520 {
				value = color.RGBA{217, 217, 217, 255}
			}
			picture.SetRGBA(x, y, value)
		}
	}
	var shot strings.Builder
	png.Encode(&shot, picture)
	f := &outputFixture{t: t, posts: make(chan func(), 64), fetches: map[string]int{}, files: map[string]string{
		"att-links1":  write("Link check.txt", []byte("Checked 14 pages and 23 links.\n\nFAIL https://example.com/download/windows → 404\nFAIL https://example.com/download/linux-arm64 → 404\n")),
		"att-links2":  write("Link check 2.txt", []byte("Checked 14 pages and 23 links.\n\nok  https://example.com/\nok  https://example.com/download\nok  https://example.com/download/windows\n…\n\nAll 23 links answer.\n")),
		"att-header":  write("Header after.png", []byte(shot.String())),
		"att-missing": write("Before.png", []byte(shot.String())),
	}}
	now := time.Now()
	add := func(id, bot string, minutesAgo int, text string, attachment map[string]any, output map[string]any) {
		body := map[string]any{"kind": "text", "text": text}
		if attachment != nil {
			body["attachments"] = []any{attachment}
		}
		f.messages = append(f.messages, map[string]any{"id": id, "chat_id": "chat-relay", "author": map[string]any{"kind": "bot", "bot_id": bot}, "body": body,
			"state": map[string]any{"kind": "complete"}, "created_at": float64(now.Add(-time.Duration(minutesAgo) * time.Minute).Unix()), "output": output})
	}
	check := func(kind, status, summary, command string) map[string]any {
		return map[string]any{"kind": kind, "status": status, "summary": summary, "command": command}
	}
	add("msg-links1", "bot-patch", 50, "Test result · Failed: 2 of 23 download links return 404.\n`bun run check:links`",
		map[string]any{"id": "att-links1", "name": "Link check.txt", "mime": "text/plain", "size": 140},
		map[string]any{"id": "out-links", "name": "Link check.txt", "mime": "text/plain", "bot_id": "bot-patch", "chat_id": "chat-relay", "version": 1, "evidence": check("test_result", "failed", "2 of 23 download links return 404.", "bun run check:links")})
	add("msg-header", "bot-patch", 20, "After screenshot · Passed: The header sits below the toolbar at every width.",
		map[string]any{"id": "att-header", "name": "Header after.png", "mime": "image/png", "size": 4096, "width": 640, "height": 360},
		map[string]any{"id": "out-header", "name": "Header after.png", "mime": "image/png", "bot_id": "bot-patch", "chat_id": "chat-relay", "version": 1, "evidence": check("after_screenshot", "passed", "The header sits below the toolbar at every width.", "")})
	add("msg-links2", "bot-patch", 6, "Test result · Passed: All 23 links answer.\n`bun run check:links`",
		map[string]any{"id": "att-links2", "name": "Link check.txt", "mime": "text/plain", "size": 160},
		map[string]any{"id": "out-links", "name": "Link check.txt", "mime": "text/plain", "bot_id": "bot-patch", "chat_id": "chat-relay", "version": 2, "previous_message_id": "msg-links1", "evidence": check("test_result", "passed", "All 23 links answer.", "bun run check:links")})
	add("msg-notes", "bot-nova", 3, "[Release notes](https://docs.example.com/lorca/release-notes)", nil,
		map[string]any{"id": "out-notes", "name": "Release notes", "mime": "text/html", "bot_id": "bot-nova", "chat_id": "chat-relay", "version": 1, "url": "https://docs.example.com/lorca/release-notes"})
	add("msg-missing", "bot-patch", 1, "",
		map[string]any{"id": "att-missing", "name": "Before.png", "mime": "image/png", "size": 48000, "width": 640, "height": 360},
		map[string]any{"id": "out-missing", "name": "Before.png", "mime": "image/png", "bot_id": "bot-patch", "chat_id": "chat-relay", "version": 1})
	return f
}

// install puts a store over the fake CLI in place of the demo's, with the demo's roster.
func (f *outputFixture) install(inTranscript bool) {
	old := store
	store = model.NewStore(outputTestTransport{f.request}, func(fn func()) { f.posts <- fn }, false)
	store.Chats, store.Bots, store.Devices = old.Chats, old.Bots, old.Devices
	store.IsStarting, store.HasIdentity, store.IsConnected = false, old.HasIdentity, true
	if inTranscript {
		chat := store.Chat("chat-relay")
		for _, wire := range f.messages {
			data, _ := json.Marshal(wire)
			var message model.WireMessage
			json.Unmarshal(data, &message)
			chat.Messages = append(chat.Messages, model.ToMessage(message))
		}
	}
}

func (f *outputFixture) request(method string, params any) (json.RawMessage, error) {
	switch method {
	case "outputs.list":
		return json.Marshal(map[string]any{"outputs": f.messages})
	case "files.path":
		p := params.(map[string]any)
		id := p["attachment"].(map[string]any)["id"].(string)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.fetches[id]++
		if id == "att-missing" && f.fetches[id] == 1 {
			return nil, errors.New("the relay no longer has Before.png")
		}
		path, ok := f.files[id]
		if !ok {
			return nil, errors.New("the relay no longer has it")
		}
		if p["named"] == true {
			f.named = append(f.named, id)
		}
		return json.Marshal(map[string]any{"path": path})
	}
	return json.RawMessage(`null`), nil
}

// pump runs what the fake CLI's replies posted, framing between them, until `done` or a timeout.
func (f *outputFixture) pump(tt *ui.Tester, what string, done func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case fn := <-f.posts:
			fn()
		case <-time.After(20 * time.Millisecond):
		}
		tt.Frame()
		if done() {
			settle(tt)
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
		}
	}
}

func TestOutputsInTheInspectorAndTheirSheet(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("Release notes") })
	// The three latest, newest first, and View all for the fourth.
	// Each row says the file's size, not what the bot says it checked.
	wantText(t, tt, L("Outputs"), "Before.png", "Release notes", "Link check.txt", "160 B", L("View all"))
	if tt.HasText(L("Passed")) {
		t.Fatal("a row shows the bot's check")
	}
	if tt.HasText("Header after.png") {
		t.Fatal("a fourth output took a row")
	}
	first, _ := tt.Find("Before.png")
	last, _ := tt.Find("Link check.txt")
	if first.Y >= last.Y {
		t.Fatal("the latest output is not first")
	}
	renderBoth(t, tt, "outputs-inspector")

	// A row opens the output over the whole window: its preview, and its versions and the file's
	// facts in the side pane, without the bot's check.
	if err := tt.Click("Link check.txt"); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "the text preview", func() bool {
		return tt.HasText("All 23 links answer.\n") || strings.Contains(strings.Join(tt.Texts(), "\n"), "ok  https://example.com/")
	})
	wantText(t, tt, L("Versions"), L("File"), L("Size"), L("Version %d", 2), L("Version %d", 1), L("Save As…"), L("Open"), L("Close"), L("Hide Info"))
	for _, check := range []string{L("Test result"), L("Passed"), "bun run check:links"} {
		if tt.HasText(check) {
			t.Fatalf("the viewer shows the bot's check %q", check)
		}
	}
	if !m.sheets[0].fullWindow {
		t.Fatal("a file output opened in a sheet")
	}
	renderBoth(t, tt, "output-viewer")

	// An earlier version shows in its place.
	if err := tt.Click(L("Version %d", 1)); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "version 1", func() bool { return tt.HasText("FAIL https://example.com/download/windows") })
	if tt.HasText(L("Failed")) || tt.HasText("2 of 23 download links return 404.") {
		t.Fatalf("version 1 shows the bot's check: %q", tt.Texts())
	}

	// The side pane closes and opens again.
	if err := tt.Click(L("Hide Info")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if tt.HasText(L("Version %d", 2)) || !tt.HasText(L("Show Info")) {
		t.Fatalf("the side pane stayed: %q", tt.Texts())
	}
	if err := tt.Click(L("Show Info")); err != nil {
		t.Fatal(err)
	}
	settle(tt)

	// Save As… copies the shown version; Open hands its named copy to the system.
	destination := filepath.Join(t.TempDir(), "Saved.txt")
	saveDestination := outputSaveDestination
	outputSaveDestination = func(_ *mygo.Window, name string) (string, error) {
		if name != "Link check.txt" {
			return "", errors.New("wrong name " + name)
		}
		return destination, nil
	}
	t.Cleanup(func() { outputSaveDestination = saveDestination })
	if err := tt.Click(L("Save As…")); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "the saved copy", func() bool { _, err := os.Stat(destination); return err == nil })
	if saved, _ := os.ReadFile(destination); !strings.Contains(string(saved), "FAIL") {
		t.Fatalf("saved %q, not version 1", saved)
	}
	opened := ""
	openPath := outputOpenPath
	outputOpenPath = func(path string) { opened = path }
	t.Cleanup(func() { outputOpenPath = openPath })
	if err := tt.Click(L("Open")); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "Open", func() bool { return opened != "" })
	if opened != f.files["att-links1"] || len(m.sheets) != 0 {
		t.Fatalf("Open used %q and left %d sheets", opened, len(m.sheets))
	}

	// View all lists every output.
	if err := tt.Click(L("View all")); err != nil {
		t.Fatal(err)
	}
	settleTransitions(tt)
	wantText(t, tt, "Header after.png")
	renderBoth(t, tt, "outputs-all")
	tt.Key(0, ui.KeyEscape)
	settle(tt)
	if m.hasSheet() {
		t.Fatal("Escape left View all up")
	}
}

func TestOutputLinkAndUnavailableFile(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("Release notes") })

	m.presentOutput("chat-relay", "out-notes")
	settleTransitions(tt)
	wantText(t, tt, L("Link"), "docs.example.com", "https://docs.example.com/lorca/release-notes")
	renderBoth(t, tt, "output-link")
	linked := ""
	openURL := outputOpenURL
	outputOpenURL = func(link string) { linked = link }
	t.Cleanup(func() { outputOpenURL = openURL })
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if linked != "https://docs.example.com/lorca/release-notes" || m.hasSheet() {
		t.Fatalf("Open opened %q", linked)
	}

	m.presentOutput("chat-relay", "out-missing")
	f.pump(tt, "the failed fetch", func() bool { return tt.HasText(L("This file couldn't be downloaded.")) })
	wantText(t, tt, "The relay no longer has Before.png", L("Try Again"))
	renderBoth(t, tt, "output-unavailable")
	if err := tt.Click(L("Try Again")); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "the retried fetch", func() bool { return !tt.HasText(L("This file couldn't be downloaded.")) })
	if f.fetches["att-missing"] != 2 || !m.hasSheet() {
		t.Fatalf("Try Again fetched %d times", f.fetches["att-missing"])
	}
}

// addFileOutput puts one more file output in the fixture's chat.
func (f *outputFixture) addFileOutput(id, name, mime, contents string) {
	dir := f.t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		f.t.Fatal(err)
	}
	f.files["att-"+id] = path
	attachment := map[string]any{"id": "att-" + id, "name": name, "mime": mime, "size": len(contents)}
	f.messages = append(f.messages, map[string]any{"id": "msg-" + id, "chat_id": "chat-relay", "author": map[string]any{"kind": "bot", "bot_id": "bot-patch"},
		"body":  map[string]any{"kind": "text", "text": "", "attachments": []any{attachment}},
		"state": map[string]any{"kind": "complete"}, "created_at": float64(time.Now().Unix()),
		"output": map[string]any{"id": "out-" + id, "name": name, "mime": mime, "bot_id": "bot-patch", "chat_id": "chat-relay", "version": 1}})
}

func TestOutputViewerDrawsMarkdownAndShowsItsSource(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.addFileOutput("guide", "Guide.md", "text/markdown", "# Welcome to Lorca\n\nA **bold** claim.\n")
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("Release notes") })

	m.presentOutput("chat-relay", "out-guide")
	f.pump(tt, "the drawn page", func() bool { return tt.HasText("Welcome to Lorca") })
	wantText(t, tt, L("Preview"), L("Source"), L("Copy"))
	renderBoth(t, tt, "markdown-viewer")
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "# Welcome") {
		t.Fatalf("the page shows Markdown marks: %q", tt.Texts())
	}

	if err := tt.Click(L("Source")); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "the source", func() bool { return strings.Contains(strings.Join(tt.Texts(), "\n"), "# Welcome to Lorca") })
}

func TestOutputViewerShowsAnHTMLFileAsSourceWithoutAWebView(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.addFileOutput("page", "page.html", "text/html", "<h1>Hello</h1>\n")
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("Release notes") })

	// The test window has no native window to make a web view in, so the page shows as its source
	// with no switch between looks.
	m.presentOutput("chat-relay", "out-page")
	f.pump(tt, "the source", func() bool { return strings.Contains(strings.Join(tt.Texts(), "\n"), "<h1>Hello</h1>") })
	if tt.HasText(L("Preview")) {
		t.Fatalf("a page with no web view offers a preview: %q", tt.Texts())
	}
}

func TestFileInAChatOpensInTheViewer(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.addFileOutput("guide", "Guide.md", "text/markdown", "# Welcome to Lorca\n")
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("Release notes") })

	m.presentFile(model.Attachment{ID: "att-guide", Name: "Guide.md", Mime: "text/markdown", Size: 19}, "chat-relay", "msg-guide")
	f.pump(tt, "the drawn page", func() bool { return tt.HasText("Welcome to Lorca") })
	wantText(t, tt, L("Save As…"), L("Open"), L("Close"), L("Preview"), L("Source"))
	renderBoth(t, tt, "file-viewer")
	tt.Key(0, ui.KeyEscape)
	settle(tt)
	if m.hasSheet() {
		t.Fatal("Escape left the viewer up")
	}
}

func TestImageInAChatOpensInTheViewer(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.install(true)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the transcript's files", func() bool { return tt.HasText(L("View all")) })

	attachment := model.Attachment{ID: "att-header", Name: "Header after.png", Mime: "image/png", Size: 4096, Width: 640, Height: 360}
	m.presentFile(attachment, "chat-relay", "msg-header")
	f.pump(tt, "the picture", func() bool { return tt.HasText("PNG · 640 × 360 · 4 KB") })
	wantText(t, tt, "Header after.png", L("Save As…"), L("Open"), L("Close"))
	if tt.HasText(L("Preview")) || tt.HasText(L("Copy")) {
		t.Fatal("a picture has a document's controls")
	}
	renderBoth(t, tt, "picture-viewer")
	tt.Key(0, ui.KeyEscape)
	settle(tt)
	if m.hasSheet() {
		t.Fatal("Escape left the picture up")
	}
}

func TestReadViewerContentCutsLongTextAndLeavesBinaryAlone(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "long.txt")
	os.WriteFile(long, []byte(strings.Repeat("é", viewerTextLimit)), 0600)
	read, err := readViewerContent(model.PreviewText, "long.txt", long)
	if err != nil || !read.truncated || len(read.text) > viewerTextLimit || len(read.text) < viewerTextLimit-1 {
		t.Fatalf("long text read as %d bytes, truncated %v, err %v", len(read.text), read.truncated, err)
	}
	binary := filepath.Join(dir, "data.txt")
	os.WriteFile(binary, []byte{0xff, 0xfe, 0x00, 0x80}, 0600)
	if read, err = readViewerContent(model.PreviewText, "data.txt", binary); err != nil || read.text != "" {
		t.Fatalf("binary data read as %q, err %v", read.text, err)
	}
}

func TestPreviewNavigationKeepsThePageWhereItIs(t *testing.T) {
	var opened []string
	open := previewOpenLink
	previewOpenLink = func(link string) { opened = append(opened, link) }
	t.Cleanup(func() { previewOpenLink = open })
	closed := 0
	navigate := previewNavigation(previewOrigin, func() { closed++ })
	if event := (&mygo.NavigateEvent{URL: previewOrigin + "close"}); true {
		navigate(event)
		if !event.DefaultPrevented() || closed != 1 {
			t.Fatalf("the page's close request prevented %v and closed %d times", event.DefaultPrevented(), closed)
		}
	}
	for _, c := range []struct {
		url              string
		clicked          bool
		prevented, opens bool
	}{
		{previewOrigin, false, false, false},
		{previewOrigin + "#section", true, false, false},
		{"about:blank", false, false, false},
		{"https://example.com/docs", true, true, true},
		{"https://example.com/ads", false, true, false},
		{previewOrigin + "other.html", true, true, true},
	} {
		before := len(opened)
		event := &mygo.NavigateEvent{URL: c.url, UserInitiated: c.clicked}
		navigate(event)
		if event.DefaultPrevented() != c.prevented || (len(opened) > before) != c.opens {
			t.Errorf("%s (clicked %v): prevented %v, opened %v", c.url, c.clicked, event.DefaultPrevented(), len(opened) > before)
		}
	}
}

func TestPicturePageCarriesThePictureAndAskingToClose(t *testing.T) {
	page := pictureHTML("image/jpeg", []byte("abc"), "#f4f4f4")
	for _, want := range []string{`src="data:image/jpeg;base64,YWJj"`, `"` + previewOrigin + `close"`, "gesturechange", "ctrlKey", "background: #f4f4f4;"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if strings.Contains(page, "__IMAGE__") || strings.Contains(page, "__CLOSE__") || strings.Contains(page, "__GROUND__") {
		t.Fatal("the page keeps a placeholder")
	}
	if !strings.Contains(pictureHTML("application/octet-stream", nil, "#000000"), "data:image/png;base64,") {
		t.Fatal("a picture of no known type is not given an image type")
	}
}

func TestFileURL(t *testing.T) {
	if got := fileURL("/tmp/a b/report.pdf"); got != "file:///tmp/a%20b/report.pdf" {
		t.Fatalf("fileURL = %q", got)
	}
}

func TestOutputsInTheTranscript(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.install(true)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the transcript's files", func() bool { return tt.HasText(L("Couldn't download · Retry")) && tt.HasText(L("View all")) })
	// The image's bytes land after the list.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case fn := <-f.posts:
			fn()
		case <-time.After(20 * time.Millisecond):
		}
		tt.Frame()
	}
	settle(tt)
	renderBoth(t, tt, "outputs-transcript")
	// The tile of a file that could not be fetched retries on a click.
	if err := tt.Click(L("Couldn't download · Retry")); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "the retried tile", func() bool { return !tt.HasText(L("Couldn't download · Retry")) })
	if f.fetches["att-missing"] != 2 {
		t.Fatalf("the tile fetched %d times", f.fetches["att-missing"])
	}
}

func TestCopyFileLeavesTheDestinationOnFailure(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "saved.txt")
	os.WriteFile(target, []byte("existing"), 0600)
	if err := copyFile(filepath.Join(root, "missing"), target); err == nil {
		t.Fatal("a missing source copied")
	}
	if data, _ := os.ReadFile(target); string(data) != "existing" {
		t.Fatal("a failed copy changed the destination")
	}
	source := filepath.Join(root, "source.txt")
	os.WriteFile(source, []byte("new result"), 0600)
	if err := copyFile(source, target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new result" {
		t.Fatal("the copy did not replace the destination")
	}
}

func TestARunOfOutputsJoinsIntoOneList(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	proof := map[string]any{"kind": "test_result", "status": "unverified", "summary": "Claude Code saved this as proof of its work."}
	for i, name := range []string{"gh-pr-diff-now.txt", "go-test-all-windows.txt", "maintainer-fix-commit.diff", "test-after-fix-windows.txt"} {
		id := fmt.Sprintf("proof%d", i)
		f.files["att-"+id] = f.files["att-links2"]
		f.messages = append(f.messages, map[string]any{"id": "msg-" + id, "chat_id": "chat-relay", "author": map[string]any{"kind": "bot", "bot_id": "bot-patch"},
			"body":  map[string]any{"kind": "text", "text": "", "attachments": []any{map[string]any{"id": "att-" + id, "name": name, "mime": "text/plain", "size": 4000 + i*900}}},
			"state": map[string]any{"kind": "complete"}, "created_at": float64(time.Now().Unix()),
			"output": map[string]any{"id": "out-" + id, "name": name, "mime": "text/plain", "bot_id": "bot-patch", "chat_id": "chat-relay", "version": 1, "evidence": proof}})
	}
	f.install(true)
	// A test frame never reads a picture off the main thread, so the screenshots' are read here.
	for _, id := range []string{"att-header", "att-missing"} {
		data, _ := os.ReadFile(f.files[id])
		bitmap, err := ui.DecodeBitmap(data)
		if err != nil {
			t.Fatal(err)
		}
		bitmaps.Lock()
		bitmaps.entries[f.files[id]] = &bitmapEntry{bitmap: bitmap}
		bitmaps.Unlock()
	}
	rows := buildRows(store.Chat("chat-relay"), nil, "")
	// Before.png is a picture, which stands apart from the files around it; the proofs after it are
	// a run of their own, and another bot's link ends the one before.
	var run []outputStack
	for _, row := range rows {
		if row.kind == rowMessage && (row.message.ID == "msg-notes" || row.message.ID == "msg-missing" || strings.HasPrefix(row.message.ID, "msg-proof")) {
			run = append(run, row.stack)
		}
	}
	want := []outputStack{{}, {}, {below: true}, {above: true, below: true}, {above: true, below: true}, {above: true}}
	if !slices.Equal(run, want) {
		t.Fatalf("the run's rows are %v, want %v", run, want)
	}
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the run's files", func() bool { return tt.HasText("test-after-fix-windows.txt") })
	// The image's bytes land after the list.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case fn := <-f.posts:
			fn()
		case <-time.After(20 * time.Millisecond):
		}
		tt.Frame()
	}
	settle(tt)
	if tt.HasText("Claude Code saved this as proof of its work.") || tt.HasText(L("Not verified")) {
		t.Fatal("a row in the run shows the bot's check")
	}
	renderBoth(t, tt, "outputs-run")
}

func TestPeekOfAFileForItsCard(t *testing.T) {
	md := peekOf("```\nskip\n```\n# Welcome to **Lorca**\n\nA [guide](https://x.dev) to `pairing`\nyour phone.\n\n## Next\n", model.PreviewMarkdown)
	if md.title != "Welcome to Lorca" || md.body != "A guide to pairing your phone." {
		t.Fatalf("markdown peek %+v", md)
	}
	page := peekOf("<html><head><title>Calc &amp; Co</title></head><body><h1>A <b>calculator</b></h1>", model.PreviewHTML)
	if page.title != "Calc & Co" || page.body != "A calculator" {
		t.Fatalf("html peek %+v", page)
	}
	if bare := peekOf("<h1>Only</h1>", model.PreviewHTML); bare.title != "Only" || bare.body != "" {
		t.Fatalf("html peek without a title %+v", bare)
	}
	code := peekOf("\n\nfunc main() {\n\tprintln(1)\n}\n", model.PreviewText)
	if len(code.lines) != 4 || code.lines[0] != "func main() {" || code.lines[1] != "    println(1)" {
		t.Fatalf("text peek %q", code.lines)
	}
	if !peekOf("", model.PreviewText).empty() || peekable(model.PreviewPDF) {
		t.Fatal("an empty file or a PDF has a peek")
	}
}

func TestLoneOutputCardsShowTheirFilesStart(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.messages = nil
	bots := []string{"bot-nova", "bot-patch", "bot-nova", "bot-patch"}
	for i, file := range []struct{ id, name, mime, text string }{
		{"guide", "Getting started.md", "text/markdown", "# Getting started\n\nPair your phone with this computer, then chat with your bots from anywhere.\n"},
		{"calc", "calculator-v1.html", "text/html", "<!doctype html><title>Calculator</title><h1>A tidy calculator</h1>"},
		{"sales", "Q3 sales.csv", "text/csv", "Region,Units,Revenue\nNorth,1204,$48.1k\nSouth,987,$39.4k\n"},
		{"conf", "config.json", "application/json", "{\n  \"relay\": \"https://relay.lorca.app\",\n  \"port\": 7421,\n  \"bots\": [\"Researcher\", \"Developer\"]\n}\n"},
	} {
		f.addFileOutput(file.id, file.name, file.mime, file.text)
		last := f.messages[len(f.messages)-1]
		last["author"] = map[string]any{"kind": "bot", "bot_id": bots[i]}
		last["output"].(map[string]any)["bot_id"] = bots[i]
		if file.id == "calc" {
			last["output"].(map[string]any)["evidence"] = map[string]any{"kind": "test_result", "status": "passed", "summary": "Behaviour driven in the real page via Playwright: 12×8=96."}
		}
		// A test frame never reads a file off the main thread, so the starts are read here.
		path := f.files["att-"+file.id]
		peeks.Lock()
		peeks.entries[path] = &peekEntry{peek: readPeek(path, file.name, model.PreviewKindOf(file.name, file.mime))}
		peeks.Unlock()
	}
	f.install(true)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the cards' peeks", func() bool {
		return tt.HasText("A tidy calculator") && tt.HasText(`"port": 7421,`) && tt.HasText("Revenue")
	})
	wantText(t, tt, "Getting started", "Calculator", `"port": 7421,`, "HTML · 66 B")
	if tt.HasText("Behaviour driven in the real page") || tt.HasText(L("Passed")) {
		t.Fatal("the card shows the bot's check")
	}
	renderBoth(t, tt, "outputs-peek")
}

func TestFileIconsCoverCommonFiles(t *testing.T) {
	entries, err := fileIconFiles.ReadDir("assets/fileicons")
	if err != nil || len(entries) < 50 {
		t.Fatalf("%d icons, err %v", len(entries), err)
	}
	for _, entry := range entries {
		data, _ := fileIconFiles.ReadFile("assets/fileicons/" + entry.Name())
		if _, err := ui.ParseSVG(data); err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
		}
	}
	for _, c := range []struct{ name, mime, want string }{
		{"calculator-v1.html", "text/html", "html"}, {"page.htm", "", "html"}, {"Report.PDF", "", "pdf"},
		{"main.go", "", "code"}, {"App.tsx", "", "code"}, {"notes.md", "", "document"}, {"run.log", "", "txt"},
		{"data.tsv", "", "spreadsheets"}, {"Budget.xlsx", "", "xlsx"}, {"Deck.key", "", "pptx"},
		{"memo.m4a", "", "audio"}, {"clip.mov", "", "video"}, {"photo.heic", "", "image"}, {"backup.tar.gz", "", "zip"},
		{"Dockerfile", "", "code"}, {"blob", "image/avif", "image"}, {"blob", "application/octet-stream", "empty"},
	} {
		if got := fileIconName(c.name, c.mime); got != c.want {
			t.Errorf("fileIconName(%q, %q) = %q, want %q", c.name, c.mime, got, c.want)
		}
	}
	if tint := fileIconFor("a.html", "").tint; tint != ui.Hex("#444CE7") {
		t.Fatalf("html tint %v", tint)
	}
}

func TestOutputViewerDrawsATableAndItsSource(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.addFileOutput("steps", "Setup steps.csv", "text/csv", "Step,Platform,Finished\nInstall the app,macOS,1198\n\"Connect a provider, then a bot\",All,1422\n")
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("Release notes") })

	m.presentOutput("chat-relay", "out-steps")
	f.pump(tt, "the table", func() bool { return tt.HasText("Connect a provider, then a bot") })
	wantText(t, tt, "Step", "Platform", "1198", L("Preview"), L("Source"))
	renderBoth(t, tt, "table-viewer")
	if err := tt.Click(L("Source")); err != nil {
		t.Fatal(err)
	}
	f.pump(tt, "the source", func() bool { return strings.Contains(strings.Join(tt.Texts(), "\n"), "Step,Platform,Finished") })
}

func TestOutputViewerDrawsAnSVGAndLeavesAMediaFileToItsApp(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-relay")
	f := newOutputFixture(t)
	f.addFileOutput("logo", "logo.svg", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="#f00"/></svg>`)
	f.addFileOutput("memo", "memo.mp3", "audio/mpeg", "ID3")
	f.install(false)
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	f.pump(tt, "the inspector's outputs", func() bool { return tt.HasText("memo.mp3") })

	// The test window has no web view: the SVG draws as a native picture, with its source a switch
	// away.
	m.presentOutput("chat-relay", "out-logo")
	f.pump(tt, "the picture", func() bool { return tt.HasText(L("Source")) })
	tt.Key(0, ui.KeyEscape)
	settle(tt)

	// A recording plays in the web view; without one it is a card that opens it in its app.
	m.presentOutput("chat-relay", "out-memo")
	f.pump(tt, "the card", func() bool { return tt.HasText(L("No preview for this kind of file.")) })
	wantText(t, tt, "memo.mp3", "MP3 · 3 B")
}
