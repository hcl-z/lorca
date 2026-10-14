package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The lightbox, for an image in a chat: the picture alone on a dark ground, fitted to the window.
// Two fingers on the trackpad pinch it larger and slide it about, a drag or the wheel moves it, and
// a double click zooms in and back; Escape, a press outside, and the close button put it away. The
// picture is a page in the system's web view, because the native views hear no pinch (only the
// wheel); where there is no web view it is a native picture, fitted to the window. Save As… and
// Open are on its bar.

const (
	// lightboxLimit is the largest picture the page takes, as the bytes of a data address.
	lightboxLimit = 32 << 20
	// lightboxBackground fills the web view until its page paints.
	lightboxBackground = "#121214"
)

var (
	lightboxGround = ui.RGB(18, 18, 20)
	lightboxInk    = ui.RGB(245, 245, 247)
)

type lightbox struct {
	attachment        model.Attachment
	chatID, messageID string
	viewer            *fileViewer
	page              *viewerPage
	started, closing  bool
}

// presentImage puts the image up in the lightbox.
func (w *appWindow) presentImage(attachment model.Attachment, chatID, messageID string) {
	l := &lightbox{attachment: attachment, chatID: chatID, messageID: messageID, viewer: newFileViewer(), page: &viewerPage{}}
	l.viewer.background = lightboxBackground
	l.viewer.onClose = func() {
		l.closing = true
		invalidateWindows()
	}
	w.present(l.view, l.viewer.close)
}

var errLightboxTooLarge = errors.New("the picture is too large for the page")

// start reads the picture off the main thread and makes the page of it, once its bytes are here.
func (l *lightbox) start(win *mygo.Window, path string) {
	l.started = true
	model.Async(store, func() (string, error) {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, lightboxLimit+1))
		if err != nil {
			return "", err
		}
		if len(data) > lightboxLimit {
			return "", errLightboxTooLarge
		}
		return lightboxHTML(l.attachment.Mime, data), nil
	}, func(html string, err error) {
		if err != nil || l.viewer.closed {
			l.page.failed = err != nil
			invalidateWindows()
			return
		}
		l.viewer.makeWebView(win, l.page, "", previewOrigin, func(view *mygo.WebView) { view.Page().LoadHTML(html, previewOrigin) })
	})
}

func (l *lightbox) view(c *ui.Context, s *sheet) {
	p := colors(c)
	path := store.LocalFile(l.attachment, l.chatID, l.messageID)
	fileError := store.AttachmentError(l.attachment.ID)
	if path != "" && !l.started && s.window.win != nil {
		l.start(s.window.win, path)
	}
	// Without a page, the picture shows fitted to the window.
	var bitmap *ui.Bitmap
	if path != "" && (s.window.win == nil || l.page.failed) {
		bitmap = loadBitmap(path)
	}

	panel := ui.Column(c).Fill().Radius(radiusSheet).Clip().Background(lightboxGround).Shadow(0, 10, 40, 0, p.Shadow).Border(0.5, p.ShadowEdge).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -10, Scale: 0.97}, Duration: 180 * time.Millisecond})
	if pressedBackdrop(panel) {
		backdropPressed = true
	}
	panel.Focusable().FocusRing(false).AutoFocus()
	closed := false
	panel.Children(func() {
		ui.Row(c).FillWidth().Padding(10, 12, 10, 18).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, l.attachment.Name).FontSize(13).FontWeight(600).TextColor(lightboxInk).SingleLine()
				detail := model.SizeText(l.attachment.Size)
				if width, height := l.attachment.Width, l.attachment.Height; width > 0 && height > 0 {
					detail = fmt.Sprintf("%d × %d · %s", width, height, detail)
				}
				ui.Text(c, detail).FontSize(11).TextColor(lightboxInk.Alpha(0.6)).SingleLine()
			})
			if lightboxButton(c, "lightbox-save", "arrow.down.to.line", L("Save As…"), path == "").Clicked() {
				s.window.saveAttachment(l.attachment)
			}
			if lightboxButton(c, "lightbox-open", "arrow.up.right", L("Open"), path == "").Clicked() {
				s.window.openAttachment(l.attachment)
			}
			if lightboxButton(c, "lightbox-close", "xmark", L("Close"), false).Clicked() {
				closed = true
			}
		})
		switch {
		case l.page.view != nil:
			ui.WebView(c, l.page.view).Grow(1).MinHeight(0)
		case bitmap != nil:
			ui.Image(c, bitmap).Grow(1).MinHeight(0).Margin(0, 16, 16, 16).Fit(ui.ScaleDown)
		case fileError != "":
			ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, L("This file couldn't be downloaded.")).FontSize(13).TextColor(lightboxInk.Alpha(0.7)).TextAlign(ui.Center)
			})
		default:
			ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Children(func() { spinner(c, 22) })
		}
	})
	if closed || l.closing || backdropPressed || panel.OverlayShortcut(0, ui.KeyEscape) {
		s.dismiss()
	}
}

// lightboxButton is an icon button on the lightbox's dark bar.
func lightboxButton(c *ui.Context, key, symbolName, words string, disabled bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(radiusMD).Justify(ui.Center).TextColor(lightboxInk).Label(words).Tooltip(words)
	fill := lightboxInk.Alpha(0.1)
	switch {
	case disabled:
		b.Disabled(true)
	case b.Pressed():
		fill = lightboxInk.Alpha(0.28)
	case b.Hovered():
		fill = lightboxInk.Alpha(0.18)
	}
	b.Background(fill).Transition(controlFade)
	b.Children(func() { symbol(c, symbolName, 14, 2) })
	return b
}

// lightboxHTML is the page that shows a picture: it fits the window and moves with the fingers. The
// picture is in the page as a data address, since a page under the preview origin reads no file.
func lightboxHTML(mime string, data []byte) string {
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/png"
	}
	return strings.NewReplacer("__IMAGE__", "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(data), "__CLOSE__", previewOrigin+"close").Replace(lightboxPage)
}

// lightboxPage is the page's markup and script. The picture is placed by a translation and a scale
// from its top left corner, kept between fitting the window and eight times its size (or the
// fit's eight times, for a small one), and never slid so far that the window shows empty ground
// beside a picture larger than it. A pinch arrives as gesture events in WebKit, as a wheel with
// Ctrl in Chromium, and as two pointers on a touch screen; two fingers sliding arrive as the wheel.
const lightboxPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no">
<style>
  html, body { margin: 0; height: 100%; overflow: hidden; background: #121214; }
  #stage { position: fixed; inset: 0; overflow: hidden; touch-action: none; cursor: grab; -webkit-user-select: none; user-select: none; }
  #stage.dragging { cursor: grabbing; }
  #pic { position: absolute; left: 0; top: 0; transform-origin: 0 0; max-width: none; visibility: hidden; -webkit-user-drag: none; user-select: none; will-change: transform; }
</style>
</head>
<body>
<div id="stage"><img id="pic" alt="" draggable="false" src="__IMAGE__"></div>
<script>
(function () {
  var CLOSE = "__CLOSE__";
  var stage = document.getElementById("stage");
  var pic = document.getElementById("pic");
  var W = 0, H = 0, nw = 0, nh = 0;
  var fit = 1, scale = 1, x = 0, y = 0, maxScale = 8;

  function measure() {
    W = stage.clientWidth; H = stage.clientHeight;
    nw = pic.naturalWidth; nh = pic.naturalHeight;
    if (!W || !H || !nw || !nh) return false;
    fit = Math.min(W / nw, H / nh, 1);
    maxScale = Math.max(8, fit * 8);
    return true;
  }
  function clamp() {
    var w = nw * scale, h = nh * scale;
    x = w <= W ? (W - w) / 2 : Math.min(0, Math.max(W - w, x));
    y = h <= H ? (H - h) / 2 : Math.min(0, Math.max(H - h, y));
  }
  function apply() {
    pic.style.transform = "translate(" + x + "px," + y + "px) scale(" + scale + ")";
  }
  function reset() {
    if (!measure()) return;
    scale = fit; x = 0; y = 0;
    clamp(); apply();
    pic.style.visibility = "visible";
  }
  // zoomBy scales about (cx, cy), a point of the stage, so that the point under it stays under it.
  function zoomBy(factor, cx, cy) {
    var next = Math.min(maxScale, Math.max(fit, scale * factor));
    var k = next / scale;
    x = cx - (cx - x) * k;
    y = cy - (cy - y) * k;
    scale = next;
    clamp(); apply();
  }
  function local(e) {
    var r = stage.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  stage.addEventListener("wheel", function (e) {
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) {
      var at = local(e);
      zoomBy(Math.exp(-e.deltaY * 0.01), at.x, at.y);
    } else {
      x -= e.deltaX; y -= e.deltaY;
      clamp(); apply();
    }
  }, { passive: false });

  var gestureFrom = 1;
  stage.addEventListener("gesturestart", function (e) { e.preventDefault(); gestureFrom = scale; });
  stage.addEventListener("gesturechange", function (e) {
    e.preventDefault();
    var at = typeof e.clientX === "number" ? local(e) : { x: W / 2, y: H / 2 };
    zoomBy(gestureFrom * e.scale / scale, at.x, at.y);
  });
  stage.addEventListener("gestureend", function (e) { e.preventDefault(); });

  var pointers = new Map();
  var pinch = null;
  stage.addEventListener("pointerdown", function (e) {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    stage.setPointerCapture(e.pointerId);
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    stage.classList.add("dragging");
    pinch = null;
  });
  stage.addEventListener("pointermove", function (e) {
    var p = pointers.get(e.pointerId);
    if (!p) return;
    if (pointers.size === 1) {
      x += e.clientX - p.x; y += e.clientY - p.y;
      p.x = e.clientX; p.y = e.clientY;
      clamp(); apply();
    } else if (pointers.size === 2) {
      p.x = e.clientX; p.y = e.clientY;
      var pts = Array.from(pointers.values());
      var r = stage.getBoundingClientRect();
      var dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      var cx = (pts[0].x + pts[1].x) / 2 - r.left;
      var cy = (pts[0].y + pts[1].y) / 2 - r.top;
      if (pinch && pinch.dist > 0) {
        x += cx - pinch.cx; y += cy - pinch.cy;
        zoomBy(dist / pinch.dist, cx, cy);
      }
      pinch = { dist: dist, cx: cx, cy: cy };
    }
  });
  function release(e) {
    pointers.delete(e.pointerId);
    pinch = null;
    if (pointers.size === 0) stage.classList.remove("dragging");
  }
  stage.addEventListener("pointerup", release);
  stage.addEventListener("pointercancel", release);

  stage.addEventListener("dblclick", function (e) {
    var at = local(e);
    if (scale > fit * 1.01) zoomBy(fit / scale, at.x, at.y);
    else zoomBy(Math.max(1, fit * 2) / scale, at.x, at.y);
  });

  window.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { location.href = CLOSE; }
    else if (e.key === "+" || e.key === "=") { zoomBy(1.25, W / 2, H / 2); }
    else if (e.key === "-") { zoomBy(0.8, W / 2, H / 2); }
    else if (e.key === "0") { reset(); }
  });

  window.addEventListener("resize", function () {
    var atFit = Math.abs(scale - fit) < 1e-6;
    if (!measure()) return;
    scale = atFit ? fit : Math.min(maxScale, Math.max(fit, scale));
    clamp(); apply();
  });

  if (pic.complete && pic.naturalWidth) reset();
  else pic.addEventListener("load", reset);
})();
</script>
</body>
</html>
`
