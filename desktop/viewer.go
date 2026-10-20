package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The file viewer, for an output and for a file in a chat, covers the whole window as Quick Look
// fills a screen: a slim bar across the top with the file's icon, name, and what it is, the
// Preview/Source switch in its middle, and Copy, Save As…, Open, and Close at its end; under it the
// file, edge to edge. A picture is a page in the system's web view that pinches and slides
// (picture.go), Markdown is drawn as a page in a reading column, an HTML page and a PDF fill a web
// view, and any other text shows as it is. An output's viewer has a pane at its side with what the
// bot checked and its versions. The files come from the local CLI; nothing is read until the
// viewer shows it.

const (
	viewerTextLimit     = 256 << 10
	viewerDocumentLimit = 1 << 20
	viewerReadingWidth  = 760
	// viewerTableRows is as many rows of a table as the viewer draws.
	viewerTableRows = 1000
	// viewerBarHeight is the main window's header's, so the traffic lights sit in the bar.
	viewerBarHeight    = 52
	viewerDetailsWidth = 320
	// previewOrigin is where an HTML page believes it is. It is an https origin the app does not
	// trust, so the page cannot call the app's Go methods, and it resolves to nothing.
	previewOrigin = "https://preview.lorca.invalid/"
)

// viewerContent is what a file holds once read: a bitmap, or the start of its text, with a table's
// rows and an SVG's picture. truncated says the file goes on past text.
type viewerContent struct {
	bitmap    *ui.Bitmap
	vector    *ui.SVG
	text      string
	rows      [][]string
	truncated bool
	loading   bool
	// gutter is the text's line numbers, one to a line, made when first shown.
	gutter string
}

// viewerPage is the web view of one file, made once the viewer first shows it.
type viewerPage struct {
	view   *mygo.WebView
	failed bool
}

// fileViewer is the state of one viewer: what each version's file holds and its web view, by
// message id, and which of a document's two looks is chosen (0 the page, 1 the source).
type fileViewer struct {
	contents map[string]*viewerContent
	pages    map[string]*viewerPage
	mode     int
	closed   bool
	// closing is set when a page asks to be closed.
	closing bool
}

func newFileViewer() *fileViewer {
	return &fileViewer{contents: map[string]*viewerContent{}, pages: map[string]*viewerPage{}}
}

// close ends the viewer's web views, which belong to the window and would outlive the viewer.
func (v *fileViewer) close() {
	v.closed = true
	for _, page := range v.pages {
		if page.view != nil {
			page.view.Destroy()
		}
	}
}

// pdfShown is whether this computer's web view draws a PDF itself: WebKitGTK has no PDF viewer.
func pdfShown(w *appWindow) bool { return w.win != nil && runtime.GOOS != "linux" }

// viewerFile is the file on screen this frame. path is its bytes here, empty while they are on
// their way; fileError why they could not be fetched. page is its web view, when it shows in one;
// drawn whether it can be drawn rather than shown as words.
type viewerFile struct {
	attachment                 model.Attachment
	messageID, path, fileError string
	kind                       model.PreviewKind
	content                    *viewerContent
	page                       *viewerPage
	drawn, hasText             bool
	// code is whether its words are code or data, shown with line numbers, rather than prose.
	code bool
}

// switchable is whether the file has a page and a source to switch between.
func (f viewerFile) switchable() bool {
	switch f.kind {
	case model.PreviewMarkdown, model.PreviewHTML, model.PreviewTable, model.PreviewSVG:
		return f.drawn && f.hasText
	}
	return false
}

// file reads what the viewer needs of a file, starting its reads and its web view on first use.
func (v *fileViewer) file(c *ui.Context, w *appWindow, attachment model.Attachment, messageID, path, fileError string) viewerFile {
	f := viewerFile{attachment: attachment, messageID: messageID, path: path, fileError: fileError, kind: attachment.PreviewKind()}
	switch (model.Output{Name: attachment.Name, Mime: attachment.Mime}).Kind() {
	case model.OutputCode, model.OutputData, model.OutputTable:
		f.code = f.kind == model.PreviewText
	}
	if path == "" || fileError != "" {
		return f
	}
	switch f.kind {
	case model.PreviewImage:
		if w.win != nil {
			if page := v.picturePage(w, attachment, messageID, path, hexColor(colors(c).Window)); !page.failed {
				f.page, f.drawn = page, true
				return f
			}
		}
		// Without a page, or for a picture too large for one, it shows as a native picture.
		f.content = v.content(f.kind, attachment.Name, messageID, path)
		f.drawn = !f.content.loading && f.content.bitmap != nil
	case model.PreviewPDF, model.PreviewMedia:
		// The web view draws a PDF, and plays a recording, from the file's named copy.
		if (f.kind == model.PreviewMedia && w.win != nil) || pdfShown(w) {
			if page := v.namedPage(w, attachment, messageID); !page.failed {
				f.page, f.drawn = page, true
			}
		}
	case model.PreviewMarkdown, model.PreviewHTML, model.PreviewText, model.PreviewTable, model.PreviewSVG:
		f.content = v.content(f.kind, attachment.Name, messageID, path)
		f.hasText = !f.content.loading && f.content.text != ""
		switch {
		case f.kind == model.PreviewMarkdown:
			f.drawn = f.hasText
		case f.kind == model.PreviewTable:
			f.drawn = len(f.content.rows) > 0
			f.code = true
		case f.kind == model.PreviewSVG && f.hasText:
			f.code = true
			if w.win != nil {
				if page := v.picturePage(w, attachment, messageID, path, hexColor(colors(c).Window)); !page.failed {
					f.page, f.drawn = page, true
					break
				}
			}
			f.drawn = f.content.vector != nil
		// A page cut short would draw wrongly.
		case f.kind == model.PreviewHTML && f.hasText && !f.content.truncated && w.win != nil:
			if page := v.htmlPage(w, messageID, f.content.text); !page.failed {
				f.page, f.drawn = page, true
			}
		}
	}
	return f
}

// content reads the version's file once, off the main thread: an image, or the start of its text.
func (v *fileViewer) content(kind model.PreviewKind, name, messageID, path string) *viewerContent {
	if content := v.contents[messageID]; content != nil {
		return content
	}
	content := &viewerContent{loading: true}
	v.contents[messageID] = content
	model.Async(store, func() (viewerContent, error) { return readViewerContent(kind, name, path) }, func(read viewerContent, err error) {
		*content = read
		invalidateWindows()
	})
	return content
}

// readViewerContent reads a file for the viewer: a bitmap for an image, else the start of its text,
// up to a limit by kind, with a table's rows, an SVG's picture, and a JSON file that is all on one
// line laid out. A file that is not UTF-8 text reads as nothing and shows as a file.
func readViewerContent(kind model.PreviewKind, name, path string) (viewerContent, error) {
	var result viewerContent
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	if kind == model.PreviewImage {
		data, err := io.ReadAll(io.LimitReader(file, 100<<20))
		if err != nil {
			return result, err
		}
		result.bitmap, err = ui.DecodeBitmap(data)
		return result, err
	}
	limit := viewerTextLimit
	if kind == model.PreviewMarkdown || kind == model.PreviewHTML || kind == model.PreviewSVG {
		limit = viewerDocumentLimit
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return result, err
	}
	if result.truncated = len(data) > limit; result.truncated {
		data = data[:limit]
	}
	// The cut may split the last character.
	for cut := 0; cut < utf8.UTFMax && len(data) > 0 && !utf8.Valid(data); cut++ {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return result, nil
	}
	result.text = string(data)
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case kind == model.PreviewTable:
		result.rows = readTable(result.text, ext == ".tsv", result.truncated)
	case kind == model.PreviewSVG:
		result.vector, _ = ui.ParseSVG(data)
	case ext == ".json" && !result.truncated && !strings.Contains(strings.TrimSpace(result.text), "\n"):
		var indented bytes.Buffer
		if json.Indent(&indented, data, "", "  ") == nil {
			result.text = indented.String()
		}
	}
	return result, nil
}

// readTable is a CSV or TSV file's rows, up to viewerTableRows. A file cut short loses its last
// row, which may be cut too; a file that does not parse has no rows and shows as its source.
func readTable(text string, tabs, truncated bool) [][]string {
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord, reader.LazyQuotes, reader.ReuseRecord = -1, true, false
	if tabs {
		reader.Comma = '\t'
	}
	var rows [][]string
	for len(rows) < viewerTableRows {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if truncated && len(rows) > 0 {
				break
			}
			return nil
		}
		rows = append(rows, row)
	}
	if truncated && len(rows) > 1 && len(rows) < viewerTableRows {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// pageFor is the version's web view, which `start` begins to make on first use. The window makes
// it off the main thread; the viewer shows a spinner until it is here.
func (v *fileViewer) pageFor(messageID string, start func(page *viewerPage)) *viewerPage {
	if page := v.pages[messageID]; page != nil {
		return page
	}
	page := &viewerPage{}
	v.pages[messageID] = page
	start(page)
	return page
}

// htmlPage is an HTML page's web view, loaded from its text under the preview origin.
func (v *fileViewer) htmlPage(w *appWindow, messageID, html string) *viewerPage {
	return v.pageFor(messageID, func(page *viewerPage) {
		v.makeWebView(w.win, page, "", "", previewOrigin, func(view *mygo.WebView) { view.Page().LoadHTML(html, previewOrigin) })
	})
}

// namedPage is a PDF's or a recording's web view, loaded from the CLI's named copy.
func (v *fileViewer) namedPage(w *appWindow, attachment model.Attachment, messageID string) *viewerPage {
	return v.pageFor(messageID, func(page *viewerPage) {
		store.OpenableFile(attachment, func(named string, err error) {
			if err != nil || v.closed {
				page.failed = true
				invalidateWindows()
				return
			}
			target := fileURL(named)
			v.makeWebView(w.win, page, "", target, target, nil)
		})
	})
}

// picturePage is a picture's web view, made once its bytes are read off the main thread, on the
// viewer's ground.
func (v *fileViewer) picturePage(w *appWindow, attachment model.Attachment, messageID, path, ground string) *viewerPage {
	return v.pageFor(messageID, func(page *viewerPage) {
		mime := attachment.Mime
		if attachment.PreviewKind() == model.PreviewSVG {
			mime = "image/svg+xml"
		}
		model.Async(store, func() (string, error) { return readPictureHTML(mime, path, ground) }, func(html string, err error) {
			if err != nil || v.closed {
				page.failed = err != nil
				invalidateWindows()
				return
			}
			v.makeWebView(w.win, page, ground, "", previewOrigin, func(view *mygo.WebView) { view.Page().LoadHTML(html, previewOrigin) })
		})
	})
}

// makeWebView makes a web view that fills with `background` until its page paints.
func (v *fileViewer) makeWebView(win *mygo.Window, page *viewerPage, background, load, allowed string, after func(*mygo.WebView)) {
	model.Async(store, func() (*mygo.WebView, error) {
		view, err := win.NewWebView(mygo.WebViewOptions{URL: load, BackgroundColor: background})
		if err != nil {
			return nil, err
		}
		view.Page().OnWillNavigate(previewNavigation(allowed, func() {
			v.closing = true
			invalidateWindows()
		}))
		if after != nil {
			after(view)
		}
		return view, nil
	}, func(view *mygo.WebView, err error) {
		switch {
		case err != nil:
			page.failed = true
		case v.closed:
			view.Destroy()
		default:
			page.view = view
		}
		invalidateWindows()
	})
}

// previewOpenLink opens a link a page was clicked on; tests replace it.
var previewOpenLink = openLink

// previewNavigation keeps a previewed page where it is. Its own address and the anchors within it
// stay; any other navigation is cancelled, and a click on a web or mail link opens it in the
// browser instead. A page under the preview origin asks to be closed by navigating to its /close,
// which a page cannot otherwise tell the app (the web view takes the keyboard from the viewer).
func previewNavigation(allowed string, onClose func()) func(*mygo.NavigateEvent) {
	return func(e *mygo.NavigateEvent) {
		if e.URL == "" || e.URL == "about:blank" || e.URL == allowed || strings.HasPrefix(e.URL, allowed+"#") {
			return
		}
		if e.URL == previewOrigin+"close" {
			e.PreventDefault()
			if onClose != nil {
				onClose()
			}
			return
		}
		e.PreventDefault()
		if e.UserInitiated {
			previewOpenLink(e.URL)
		}
	}
}

// fileURL is the file: address of a local path.
func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

// hexColor is a color as CSS writes it, for a page's ground.
func hexColor(color ui.Color) string { return fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B) }

// viewerFrameOptions describe the viewer's bar: the file's icon, its name, and what it is; Center builds the middle of the bar and Actions the buttons before Close. Details builds
// the side pane, which DetailsShown opens and closes.
type viewerFrameOptions struct {
	Title, Subtitle string
	// Name and Mime pick the file's icon.
	Name, Mime   string
	Center       func()
	Actions      func()
	Details      func()
	DetailsShown *bool
}

// viewerFrame builds the viewer over the whole window, `body` under its bar, and answers whether it
// was closed this frame: by Close or Escape.
func viewerFrame(c *ui.Context, o viewerFrameOptions, body func()) bool {
	p := colors(c)
	closed := false
	// It settles in from a little smaller and lower, as a window zooms open.
	root := ui.Column(c).Fill().Background(p.Content).AlignItems(ui.Stretch).Focusable().FocusRing(false).AutoFocus().
		Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: 0.97, Y: 10}, Animation: &viewerSpring})
	root.Children(func() {
		// The bar is the window's title bar: it drags the window and keeps clear of its controls.
		bar := c.TitleBar()
		header := ui.Row(c).FillWidth().Height(max(bar.Height, viewerBarHeight)).Padding(0, max(12, bar.Right+8), 0, max(16, bar.Left+12)).
			Gap(12).AlignItems(ui.Center).DragWindow()
		header.Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(10).AlignItems(ui.Center).Children(func() {
				icon := ui.Box(c).Size(30, 30).Shrink(0).Center()
				icon.Children(func() { fileIconView(c, o.Name, o.Mime, 30) })
				ui.Column(c).Shrink(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, o.Title).FontSize(13).FontWeight(600).TextColor(p.Label).SingleLine().Tooltip(o.Title)
					if o.Subtitle != "" {
						ui.Text(c, o.Subtitle).FontSize(11).TextColor(p.Label2).FontFeatures("tnum").SingleLine()
					}
				})
			})
			if o.Center != nil {
				ui.Row(c).Shrink(0).Children(o.Center)
			}
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(2).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				if o.Actions != nil {
					o.Actions()
				}
				if o.Details != nil && o.DetailsShown != nil {
					words := L("Show Info")
					if *o.DetailsShown {
						words = L("Hide Info")
					}
					if hoverButton(c.Key("viewer-details"), hoverButtonOptions{Symbol: "info.circle", Tooltip: words, Active: *o.DetailsShown}).Clicked() {
						*o.DetailsShown = !*o.DetailsShown
					}
				}
				ui.Box(c).Size(1, 18).Margin(0, 6).Background(p.Separator)
				if hoverButton(c.Key("viewer-close"), hoverButtonOptions{Symbol: "xmark", Tooltip: L("Close")}).Clicked() {
					closed = true
				}
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(p.Separator)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Children(body)
			if o.Details != nil && o.DetailsShown != nil && *o.DetailsShown {
				ui.Row(c.Key("viewer-info")).Shrink(0).AlignItems(ui.Stretch).
					Transition(ui.ElementTransition{Enter: &ui.Motion{X: 24}, Exit: &ui.Motion{X: 24}, Animation: &paneSpring}).Children(func() {
					ui.Box(c).Width(1).Background(p.Separator)
					ui.Scroll(c).Width(viewerDetailsWidth).Background(p.Inspector).Children(func() {
						ui.Column(c).Padding(20, 18, 28, 18).Gap(22).Children(o.Details)
					})
				})
			}
		})
	})
	if root.OverlayShortcut(0, ui.KeyEscape) {
		closed = true
	}
	return closed
}

// viewerSpring is how the viewer opens.
var viewerSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 320 * time.Millisecond, Bounce: 0.08}}

// switcher is the bar's Preview/Source control, for a document that has both.
func (v *fileViewer) switcher(c *ui.Context, f viewerFile) func() {
	if !f.switchable() {
		return nil
	}
	return func() { segmented(c.Key("viewer-mode"), &v.mode, L("View"), L("Preview"), L("Source")) }
}

// viewerActions are the bar's buttons that were clicked this frame.
type viewerActions struct{ save, open bool }

// actions are the bar's Copy, for text, Save As…, and Open.
func (v *fileViewer) actions(c *ui.Context, f viewerFile) viewerActions {
	var clicked viewerActions
	if f.hasText {
		copyButton(c.Key("viewer-copy"), f.content.text, copyOptions{Symbol: "doc.on.doc", Tooltip: L("Copy")}).Height(controlRegular).MinWidth(controlRegular).Radius(radiusMD)
	}
	clicked.save = hoverButton(c.Key("viewer-save"), hoverButtonOptions{Symbol: "arrow.down.to.line", Tooltip: L("Save As…"), Disabled: f.path == ""}).Clicked()
	clicked.open = hoverButton(c.Key("viewer-open"), hoverButtonOptions{Symbol: "arrow.up.right", Tooltip: L("Open"), Disabled: f.path == ""}).Clicked()
	return clicked
}

// body is the file under the bar: drawn, as its words, or a card of it when it is neither. retry
// fetches a file that could not be, open hands it to its app.
func (v *fileViewer) body(c *ui.Context, f viewerFile, retry, open func()) {
	p := colors(c)
	centered := func(inner func()) {
		ui.Column(c).Grow(1).MinHeight(0).Gap(8).Padding(0, 32).AlignItems(ui.Center).Justify(ui.Center).Children(inner)
	}
	waiting := func() { centered(func() { spinner(c, 20) }) }
	note := func() {
		if f.content != nil && f.content.truncated {
			ui.Text(c, L("Showing the first %@", model.SizeText(int64(len(f.content.text))))).FontSize(11).TextColor(p.Label3)
		}
	}
	looks := f.drawn && (v.mode == 0 || !f.switchable())
	switch {
	case f.fileError != "":
		centered(func() {
			ui.Box(c).Margin(0, 0, 4, 0).TextColor(p.Label3).Children(func() { symbol(c, "exclamationmark.triangle", 28, 1.6) })
			ui.Text(c, L("This file couldn't be downloaded.")).FontSize(14).FontWeight(500).TextColor(p.Label).TextAlign(ui.Center)
			ui.Text(c, strings.ToUpper(f.fileError[:1])+f.fileError[1:]).FontSize(12).TextColor(p.Label2).TextAlign(ui.Center)
			ui.Box(c).Margin(8, 0, 0, 0).Children(func() {
				if pushButton(c.Key("viewer-retry"), L("Try Again"), pushOptions{}).Clicked() {
					retry()
				}
			})
		})
	case f.path == "":
		centered(func() {
			spinner(c, 20)
			ui.Text(c, L("Downloading…")).FontSize(12).TextColor(p.Label2)
		})
	case looks && f.page != nil:
		if f.page.view == nil {
			waiting()
			return
		}
		ui.WebView(c, f.page.view).Grow(1).MinHeight(0)
	case looks && f.kind == model.PreviewTable:
		tableView(c, p, f.content.rows, len(f.content.rows) == viewerTableRows || f.content.truncated)
	case looks && f.kind == model.PreviewSVG:
		ui.Column(c).Grow(1).MinHeight(0).Padding(24).Background(p.Window).AlignItems(ui.Stretch).Children(func() {
			ui.Image(c, f.content.vector).Grow(1).MinHeight(0).Fit(ui.Contain)
		})
	case looks && f.kind == model.PreviewImage:
		ui.Column(c).Grow(1).MinHeight(0).Padding(24).Background(p.Window).AlignItems(ui.Stretch).Children(func() {
			ui.Image(c, f.content.bitmap).Grow(1).MinHeight(0).Fit(ui.ScaleDown)
		})
	case looks && f.kind == model.PreviewMarkdown:
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
				ui.Column(c).Width(viewerReadingWidth).MaxWidthPercent(100).Padding(40, 48, 72, 48).Gap(24).Children(func() {
					markdownView(c, f.content.text, markdownOptions{Size: 15, Document: true})
					note()
				})
			})
		})
	case f.content != nil && f.content.loading:
		waiting()
	case f.hasText && (f.code || f.switchable()):
		// Code and a document's source keep their lines whole beside their numbers; the lines slide
		// sideways under the numbers, which stay.
		if f.content.gutter == "" {
			f.content.gutter = lineNumbers(f.content.text)
		}
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Stretch).Children(func() {
				ui.Text(c, f.content.gutter).Font(monoFont).FontSize(12).LineHeight(1.65).TextColor(p.Label3).TextAlign(ui.End).
					Padding(20, 12, 48, 16).Shrink(0).Background(p.Window.Alpha(0.6))
				ui.Box(c).Width(1).Background(p.Separator)
				ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Children(func() {
					ui.Column(c).Padding(20, 32, 48, 16).Gap(16).Children(func() {
						ui.Text(c, f.content.text).Font(monoFont).FontSize(12).LineHeight(1.65).TextColor(p.Label).NoWrap().Selectable()
						note()
					})
				})
			})
		})
	case f.hasText:
		// Prose and logs wrap in a column a reader's eye can follow.
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
				ui.Column(c).Width(viewerReadingWidth+120).MaxWidthPercent(100).Padding(32, 40, 56, 40).Gap(16).Children(func() {
					ui.Text(c, f.content.text).Font(monoFont).FontSize(12.5).LineHeight(1.65).TextColor(p.Label).Selectable()
					note()
				})
			})
		})
	default:
		centered(func() {
			ui.Box(c).Margin(0, 0, 8, 0).Children(func() { fileIconView(c, f.attachment.Name, f.attachment.Mime, 72) })
			ui.Text(c, f.attachment.Name).FontSize(14).FontWeight(600).TextColor(p.Label).TextAlign(ui.Center)
			ui.Text(c, fileDetail(f.attachment)).FontSize(12).TextColor(p.Label2).TextAlign(ui.Center)
			ui.Text(c, L("No preview for this kind of file.")).FontSize(12).TextColor(p.Label3).TextAlign(ui.Center)
			ui.Box(c).Margin(8, 0, 0, 0).Children(func() {
				if pushButton(c.Key("viewer-open-card"), L("Open"), pushOptions{Kind: buttonPrimary}).Clicked() {
					open()
				}
			})
		})
	}
}

// lineNumbers are the numbers of the text's lines, one to a line.
func lineNumbers(text string) string {
	lines := strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		if i > 1 {
			b.WriteByte('\n')
		}
		b.WriteString(strconv.Itoa(i))
	}
	return b.String()
}

// fileDetail is what a file is: its type, a picture's size, and its own size.
func fileDetail(attachment model.Attachment) string {
	var parts []string
	if ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(attachment.Name), ".")); ext != "" {
		parts = append(parts, ext)
	}
	if attachment.Width > 0 && attachment.Height > 0 {
		parts = append(parts, fmt.Sprintf("%d × %d", attachment.Width, attachment.Height))
	}
	if attachment.Size > 0 {
		parts = append(parts, model.SizeText(attachment.Size))
	}
	return strings.Join(parts, " · ")
}

// presentFile opens a file in a chat, which is not an output, in the viewer.
func (w *appWindow) presentFile(attachment model.Attachment, chatID, messageID string) {
	viewer := newFileViewer()
	w.presentFullWindow(func(c *ui.Context, s *sheet) {
		path := store.LocalFile(attachment, chatID, messageID)
		f := viewer.file(c, s.window, attachment, messageID, path, store.AttachmentError(attachment.ID))
		var clicked viewerActions
		closed := viewerFrame(c, viewerFrameOptions{
			Title: attachment.Name, Subtitle: fileDetail(attachment), Name: attachment.Name, Mime: attachment.Mime,
			Center: viewer.switcher(c, f), Actions: func() { clicked = viewer.actions(c, f) },
		}, func() {
			viewer.body(c, f, func() { store.RetryAttachment(attachment, chatID, messageID) }, func() { clicked.open = true })
		})
		switch {
		case clicked.save:
			s.window.saveAttachment(attachment)
		case clicked.open:
			s.window.openAttachment(attachment)
			s.dismiss()
		case closed || viewer.closing:
			s.dismiss()
		}
	}, viewer.close)
}

// tableView draws a table's rows as a grid: the first row as its header, which stays while the rows
// scroll under it, a number before each row, and columns as wide as their words, up to a limit.
// more says the file has rows past those drawn.
func tableView(c *ui.Context, p *palette, rows [][]string, more bool) {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]float32, columns)
	for i, row := range rows {
		if i == 200 {
			break
		}
		for j, cell := range row {
			w, _ := c.MeasureText(0, ui.Span{Text: firstLineOf(cell), Size: 12.5, Weight: 600})
			widths[j] = min(max(widths[j], w+28), 360)
		}
	}
	gutter, _ := c.MeasureText(0, ui.Span{Text: strconv.Itoa(len(rows)), Size: 11})
	numberWidth := gutter + 28
	line := func(i int, row []string, header bool) {
		fill := p.Content
		if header {
			fill = p.Window
		} else if i%2 == 0 {
			fill = p.Window.Alpha(0.45).Over(p.Content)
		}
		ui.Row(c.Key(i)).Height(32).Background(fill).AlignItems(ui.Center).Children(func() {
			number := ""
			if !header {
				number = strconv.Itoa(i)
			}
			ui.Text(c, number).Width(numberWidth).Shrink(0).Padding(0, 12).FontSize(11).TextColor(p.Label3).TextAlign(ui.End).FontFeatures("tnum").SingleLine()
			for j := range columns {
				cell := ""
				if j < len(row) {
					cell = row[j]
				}
				text := ui.Text(c, firstLineOf(cell)).Width(widths[j]).Shrink(0).Padding(0, 14).FontSize(12.5).SingleLine().Tooltip(cell)
				if header {
					text.FontWeight(600).TextColor(p.Label)
				} else {
					text.TextColor(p.Label).FontFeatures("tnum")
				}
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(p.Separator.Alpha(0.6))
	}
	ui.ScrollHorizontal(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).MinHeight(0).FillHeight().Children(func() {
			line(0, rows[0], true)
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Padding(0, 0, 32, 0).Children(func() {
					for i, row := range rows[1:] {
						line(i+1, row, false)
					}
					if more {
						ui.Text(c, L("Showing the first %d rows", len(rows))).Padding(14, 0, 0, numberWidth+14).FontSize(11).TextColor(p.Label3)
					}
				})
			})
		})
	})
}
