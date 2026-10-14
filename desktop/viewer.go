package main

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The file viewer, in the sheet of an output and of a file in a chat: an image fitted to the sheet
// or at its own size, Markdown drawn as a page, an HTML page and a PDF in the system's web view,
// and any other text as it is. Markdown and HTML switch between the page and their source. The
// files come from the local CLI; nothing is read until the sheet shows it.

const (
	// The box is between viewerMinHeight and viewerMaxHeight; viewerChrome is what a sheet takes
	// of the window besides the box: the room around the panel, its title and buttons, and the
	// viewer's bar.
	viewerMinHeight     = 200
	viewerMaxHeight     = 560
	viewerChrome        = 296
	viewerTextLimit     = 256 << 10
	viewerDocumentLimit = 1 << 20
	viewerReadingWidth  = 720
	// viewerSheetWidth is the width of a sheet that shows a file.
	viewerSheetWidth = 900
	// previewOrigin is where an HTML page believes it is. It is an https origin the app does not
	// trust, so the page cannot call the app's Go methods, and it resolves to nothing.
	previewOrigin = "https://preview.lorca.invalid/"
)

// viewerContent is what a file holds once read: a bitmap, or the start of its text. truncated says
// the file goes on past text.
type viewerContent struct {
	bitmap    *ui.Bitmap
	text      string
	truncated bool
	loading   bool
}

// viewerPage is the web view of one file, made once the sheet first shows it.
type viewerPage struct {
	view   *mygo.WebView
	failed bool
}

// fileViewer is the state of one sheet's viewer: what each version's file holds and its web view,
// by message id, and which of a file's two looks is chosen (0 the page or fitted, 1 the source or
// actual size).
type fileViewer struct {
	contents map[string]*viewerContent
	pages    map[string]*viewerPage
	mode     int
	closed   bool
	// background fills a web view until its page paints; onClose hears a page ask to be closed.
	background string
	onClose    func()
	// reserve is the height of what the sheet shows under the file, which the box leaves room for.
	reserve float32
}

func newFileViewer() *fileViewer {
	return &fileViewer{contents: map[string]*viewerContent{}, pages: map[string]*viewerPage{}}
}

// close ends the viewer's web views, which belong to the window and would outlive the sheet.
func (v *fileViewer) close() {
	v.closed = true
	for _, page := range v.pages {
		if page.view != nil {
			page.view.Destroy()
		}
	}
}

// boxHeight is the file's box: as tall as the window leaves once the sheet's title, buttons, bar,
// and what the sheet shows under the file (`reserve`) have their room, never short enough to be
// useless or taller than a page.
func (v *fileViewer) boxHeight(c *ui.Context) float32 {
	_, window := c.Size()
	return min(max(window-viewerChrome-v.reserve, viewerMinHeight), viewerMaxHeight)
}

// viewerModes are the looks a kind of file has, as the words of the control that picks one.
func viewerModes(kind model.PreviewKind) []string {
	switch kind {
	case model.PreviewImage:
		return []string{L("Fit"), L("Actual Size")}
	case model.PreviewMarkdown, model.PreviewHTML:
		return []string{L("Preview"), L("Source")}
	}
	return nil
}

// pdfShown is whether this computer's web view draws a PDF itself: WebKitGTK has no PDF viewer.
func pdfShown(w *appWindow) bool { return w.win != nil && runtime.GOOS != "linux" }

// view builds the bar over the file (its looks, how much of it shows, Copy) and the file in a box.
// path is the file's bytes here, empty while they are on their way; fileError why they could not be
// fetched.
func (v *fileViewer) view(c *ui.Context, w *appWindow, attachment model.Attachment, messageID, path, fileError string) {
	p := colors(c)
	kind := attachment.PreviewKind()
	var content *viewerContent
	if path != "" && fileError == "" {
		content = v.content(kind, messageID, path)
	}
	ready := content != nil && !content.loading
	page := v.pages[messageID]
	pageFailed := page != nil && page.failed
	hasText := ready && content.text != ""
	// What the file can be drawn as, rather than shown as words.
	var drawn bool
	switch kind {
	case model.PreviewImage:
		drawn = ready && content.bitmap != nil
	case model.PreviewMarkdown:
		drawn = hasText
	case model.PreviewHTML:
		// A page cut short would draw wrongly.
		drawn = hasText && !content.truncated && w.win != nil && !pageFailed
	case model.PreviewPDF:
		drawn = path != "" && fileError == "" && pdfShown(w) && !pageFailed
	}
	modes := viewerModes(kind)
	if hasText || (drawn && len(modes) > 0) {
		ui.Row(c).Gap(8).Children(func() {
			if drawn && len(modes) > 0 {
				segmented(c, &v.mode, L("View"), modes...)
			}
			ui.Spacer(c)
			if hasText && content.truncated {
				ui.Text(c, L("Showing the first %@", model.SizeText(int64(len(content.text))))).FontSize(11).TextColor(p.Label3).SingleLine()
			}
			if hasText {
				copyButton(c, content.text, copyOptions{Title: L("Copy"), Symbol: "doc.on.doc"})
			}
		})
	}
	looks := drawn && v.mode == 0
	box := ui.Column(c.Key("preview")).Height(v.boxHeight(c)).Radius(10).Clip().Background(p.BotBubble).Border(1, p.BotBubbleBorder).AlignItems(ui.Stretch)
	if kind == model.PreviewImage && drawn {
		box.Padding(12)
	}
	box.Children(func() {
		centered := func(inner func()) {
			ui.Column(c).Grow(1).Gap(6).Padding(0, 24).AlignItems(ui.Center).Justify(ui.Center).Children(inner)
		}
		placeholder := func(text, detail string) {
			centered(func() {
				ui.Text(c, text).FontSize(13).TextColor(p.Label2).TextAlign(ui.Center)
				if detail != "" {
					ui.Text(c, detail).FontSize(11).TextColor(p.Label3).TextAlign(ui.Center)
				}
			})
		}
		switch {
		case fileError != "":
			placeholder(L("This file couldn't be downloaded."), strings.ToUpper(fileError[:1])+fileError[1:])
		case path == "":
			centered(func() {
				spinner(c, 18)
				ui.Text(c, L("Downloading…")).FontSize(13).TextColor(p.Label2)
			})
		case content != nil && content.loading:
			centered(func() { spinner(c, 18) })
		case looks && kind == model.PreviewImage:
			ui.Image(c, content.bitmap).Grow(1).MinHeight(0).Fit(ui.ScaleDown)
		case looks && kind == model.PreviewMarkdown:
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
					ui.Column(c).Width(viewerReadingWidth).MaxWidthPercent(100).Padding(20, 28).Children(func() {
						markdownView(c, content.text, markdownOptions{Size: 15, Document: true})
					})
				})
			})
		case looks && (kind == model.PreviewHTML || kind == model.PreviewPDF):
			if page = v.page(w, attachment, kind, messageID, content); page.view != nil {
				ui.WebView(c, page.view).Grow(1).MinHeight(0)
			} else {
				centered(func() { spinner(c, 18) })
			}
		case drawn && kind == model.PreviewImage:
			ui.ScrollBoth(c).Grow(1).MinHeight(0).Children(func() { ui.Image(c, content.bitmap) })
		case hasText:
			ui.Scroll(c).Grow(1).MinHeight(0).Background(p.Code).Padding(10, 14).Children(func() {
				ui.Text(c, content.text).Font(monoFont).FontSize(12).LineHeight(1.5).Selectable()
			})
		default:
			centered(func() {
				symbol(c, "doc", 40, 1.2).TextColor(p.Label2)
				ui.Text(c, attachment.Name).FontSize(13).FontWeight(500).TextColor(p.Label)
				ui.Text(c, model.SizeText(attachment.Size)).FontSize(11).TextColor(p.Label2)
			})
		}
	})
}

// content reads the version's file once, off the main thread: an image, or the start of its text.
func (v *fileViewer) content(kind model.PreviewKind, messageID, path string) *viewerContent {
	if kind == model.PreviewPDF || kind == model.PreviewNone {
		return nil
	}
	if content := v.contents[messageID]; content != nil {
		return content
	}
	content := &viewerContent{loading: true}
	v.contents[messageID] = content
	model.Async(store, func() (viewerContent, error) { return readViewerContent(kind, path) }, func(read viewerContent, err error) {
		*content = read
		invalidateWindows()
	})
	return content
}

// readViewerContent reads a file for the viewer: a bitmap for an image, else the start of its text,
// up to a limit by kind. A file that is not UTF-8 text reads as nothing and shows as a file.
func readViewerContent(kind model.PreviewKind, path string) (viewerContent, error) {
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
	if kind == model.PreviewMarkdown || kind == model.PreviewHTML {
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
	if utf8.Valid(data) {
		result.text = string(data)
	}
	return result, nil
}

// page is the file's web view, made on first use: an HTML page's from its text under the preview
// origin, a PDF's from its named copy. The window makes it off the main thread; the sheet shows a
// spinner until it is here.
func (v *fileViewer) page(w *appWindow, attachment model.Attachment, kind model.PreviewKind, messageID string, content *viewerContent) *viewerPage {
	if page := v.pages[messageID]; page != nil {
		return page
	}
	page := &viewerPage{}
	v.pages[messageID] = page
	if kind == model.PreviewHTML {
		html := content.text
		v.makeWebView(w.win, page, "", previewOrigin, func(view *mygo.WebView) { view.Page().LoadHTML(html, previewOrigin) })
		return page
	}
	store.OpenableFile(attachment, func(named string, err error) {
		if err != nil || v.closed {
			page.failed = true
			invalidateWindows()
			return
		}
		target := fileURL(named)
		v.makeWebView(w.win, page, target, target, nil)
	})
	return page
}

func (v *fileViewer) makeWebView(win *mygo.Window, page *viewerPage, load, allowed string, after func(*mygo.WebView)) {
	model.Async(store, func() (*mygo.WebView, error) {
		view, err := win.NewWebView(mygo.WebViewOptions{URL: load, BackgroundColor: v.background})
		if err != nil {
			return nil, err
		}
		view.Page().OnWillNavigate(previewNavigation(allowed, v.onClose))
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
// which a page cannot otherwise tell the app (the web view takes the keyboard from the sheet).
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

// presentFile puts up a sheet for a file in a chat, which is not an output: the viewer, Open, and
// Save As….
func (w *appWindow) presentFile(attachment model.Attachment, chatID, messageID string) {
	viewer := newFileViewer()
	w.present(func(c *ui.Context, s *sheet) {
		path := store.LocalFile(attachment, chatID, messageID)
		fileError := store.AttachmentError(attachment.ID)
		confirm := L("Open")
		if fileError != "" {
			confirm = L("Try Again")
		}
		result := sheetFrame(c, sheetOptions{
			Title: attachment.Name, Subtitle: model.SizeText(attachment.Size), Width: viewerSheetWidth, Confirm: confirm, Cancel: L("Done"),
			Leading: func() {
				if pushButton(c.Key("file-save"), L("Save As…"), pushOptions{Disabled: path == ""}).Clicked() {
					s.window.saveAttachment(attachment)
				}
			},
		}, func() { viewer.view(c, s.window, attachment, messageID, path, fileError) })
		switch {
		case result.Cancelled:
			s.dismiss()
		case result.Confirmed && fileError != "":
			store.RetryAttachment(attachment, chatID, messageID)
		case result.Confirmed:
			s.window.openAttachment(attachment)
			s.dismiss()
		}
	}, viewer.close)
}
