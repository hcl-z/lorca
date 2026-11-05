//go:build darwin

package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// Window levels, styles, and behaviors of AppKit, named here because MyGo exposes none of them.
const (
	// nsStatusWindowLevel is above the menu bar, so a window can sit on the notch. MyGo's
	// AlwaysOnTop stops at the floating level (3), which AppKit keeps out of the menu bar.
	nsStatusWindowLevel = 25
	// canJoinAllSpaces | stationary | ignoresCycle | fullScreenAuxiliary: the island stays
	// through a Space switch, a full-screen app, and Mission Control.
	nsCollectionAllSpaces = 1 | 16 | 64 | 256
	// nsNonactivatingPanel is a panel's style: a click does not activate the app.
	nsNonactivatingPanel = 1 << 7
)

// islandSupported is whether this platform has the island: macOS alone has the window levels,
// the notch, and the nonactivating panel it needs.
const islandSupported = true

// objectSetClass is C's object_setClass, which swapped the window's class below. purego's objc
// package sends messages but exposes no class functions.
var objectSetClass func(id, class uintptr)

// The calls that return a struct, which purego's objc.Send does not register for. On arm64 every
// struct comes back through objc_msgSend, and NSRect is four doubles.
type nsRect struct{ X, Y, W, H float64 }

type nsEdgeInsets struct{ Top, Left, Bottom, Right float64 }

var (
	msgSendRect   func(id uintptr, sel objc.SEL) nsRect
	msgSendInsets func(id uintptr, sel objc.SEL) nsEdgeInsets
)

func init() {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.RegisterLibFunc(&objectSetClass, lib, "object_setClass")
	if send, err := purego.Dlsym(lib, "objc_msgSend"); err == nil {
		purego.RegisterFunc(&msgSendRect, send)
		purego.RegisterFunc(&msgSendInsets, send)
	}
}

// polishIslandWindow makes MyGo's window one the island can live in on macOS: on the screen's top
// edge above the menu bar, on every Space, borderless (a titled window is pushed out of the menu
// bar), and a panel that takes a click without activating the app.
//
// The panel part applies mygo's flyout class to a window made as a plain NSWindow: it is the only
// way to get a non-activating window without changing MyGo, which owns the window. NSPanel adds no
// instance variables to NSWindow, so the swap is safe, but it does drop MyGoWindow's own
// canBecomeKeyWindow: an island takes no typing, so nothing is lost. If a macOS release breaks
// this, drop the swap and keep the rest: the island still shows, and a click activates the app.
func polishIslandWindow(w *mygo.Window) {
	h := w.NativeHandle()
	if h == 0 {
		return
	}
	win := objc.ID(h)
	// The title bar goes first, while the class is still MyGoWindow: swapping the class under a
	// titled window leaves AppKit's title bar view observing a window it no longer matches, which
	// throws when it moves away.
	objc.Send[struct{}](win, objc.RegisterName("setStyleMask:"), uint64(0))
	if panel := uintptr(objc.GetClass("NSPanel")); panel != 0 && objectSetClass != nil {
		objectSetClass(h, panel)
		objc.Send[struct{}](win, objc.RegisterName("setStyleMask:"), uint64(nsNonactivatingPanel))
		objc.Send[struct{}](win, objc.RegisterName("setBecomesKeyOnlyIfNeeded:"), true)
		objc.Send[struct{}](win, objc.RegisterName("setFloatingPanel:"), true)
	}
	// Cindy's island sits two levels above the status bar's, over the menu bar's own extras.
	objc.Send[struct{}](win, objc.RegisterName("setLevel:"), int64(nsStatusWindowLevel+2))
	objc.Send[struct{}](win, objc.RegisterName("setHidesOnDeactivate:"), false)
	objc.Send[struct{}](win, objc.RegisterName("setCollectionBehavior:"), uint64(nsCollectionAllSpaces))
}

// revealIslandWindow orders the panel in front of everything after MyGo shows it: a panel made by
// swapping the class skips NSPanel's own init, so ordering is done here as a panel's would be.
func revealIslandWindow(w *mygo.Window) {
	if h := w.NativeHandle(); h != 0 {
		objc.Send[struct{}](objc.ID(h), objc.RegisterName("orderFrontRegardless"))
	}
}

// islandNotch is what a display says of its top edge: the notch's width (0 without one) and the
// height of the strip the compact island fills, the notch's safe area or else the menu bar.
type islandNotch struct {
	Width  float32
	TopBar float32
}

// islandNotchOf reads a display's notch from its NSScreen, found by the display id MyGo gives it,
// as Cindy's AgentIslandScreenMetrics: the notch is the screen's width less the areas left and right
// of it (auxiliaryTopLeftArea, auxiliaryTopRightArea, macOS 12), and the strip is its safe area's
// top, else the menu bar's height.
func islandNotchOf(displayID int64) islandNotch {
	if msgSendRect == nil {
		return islandNotch{}
	}
	screen := islandScreenFor(displayID)
	if screen == 0 {
		return islandNotch{}
	}
	var n islandNotch
	frame := msgSendRect(uintptr(screen), objc.RegisterName("frame"))
	if objc.Send[bool](screen, objc.RegisterName("respondsToSelector:"), objc.RegisterName("auxiliaryTopLeftArea")) {
		left := msgSendRect(uintptr(screen), objc.RegisterName("auxiliaryTopLeftArea"))
		right := msgSendRect(uintptr(screen), objc.RegisterName("auxiliaryTopRightArea"))
		if left.W > 0 || right.W > 0 {
			n.Width = float32(max(1, frame.W-left.W-right.W))
		}
	}
	if objc.Send[bool](screen, objc.RegisterName("respondsToSelector:"), objc.RegisterName("safeAreaInsets")) {
		if top := msgSendInsets(uintptr(screen), objc.RegisterName("safeAreaInsets")).Top; top > 0 {
			n.TopBar = float32(top)
		}
	}
	if n.TopBar == 0 {
		visible := msgSendRect(uintptr(screen), objc.RegisterName("visibleFrame"))
		if bar := frame.Y + frame.H - (visible.Y + visible.H); bar > 5 {
			n.TopBar = float32(bar)
		}
	}
	return n
}

// islandScreenFor is the NSScreen whose NSScreenNumber is the display's id, as MyGo numbers them.
func islandScreenFor(displayID int64) objc.ID {
	screens := objc.ID(objc.GetClass("NSScreen")).Send(objc.RegisterName("screens"))
	if screens == 0 {
		return 0
	}
	count := objc.Send[uint64](screens, objc.RegisterName("count"))
	key := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "NSScreenNumber\x00")
	for i := uint64(0); i < count; i++ {
		screen := screens.Send(objc.RegisterName("objectAtIndex:"), i)
		number := screen.Send(objc.RegisterName("deviceDescription")).Send(objc.RegisterName("objectForKey:"), key)
		if number != 0 && int64(objc.Send[uint32](number, objc.RegisterName("unsignedIntValue"))) == displayID {
			return screen
		}
	}
	return 0
}

// islandMouseDown is whether the primary button is down anywhere on screen, for a click outside
// the island, which an ignored window never hears of.
func islandMouseDown() bool {
	return objc.Send[uint64](objc.ID(objc.GetClass("NSEvent")), objc.RegisterName("pressedMouseButtons"))&1 != 0
}

// islandSoundNames are the system sounds the island plays for Cindy's events, in place of Cindy's
// own chimes: a start, a question, a result, and a failure.
var islandSoundNames = map[islandSoundEvent]string{
	islandSoundStart:     "Pop",
	islandSoundAttention: "Glass",
	islandSoundComplete:  "Hero",
	islandSoundError:     "Basso",
}

// playIslandSound plays the event's system sound at Cindy's volume, 0.58.
func playIslandSound(e islandSoundEvent) {
	name, ok := islandSoundNames[e]
	if !ok {
		return
	}
	str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), name+"\x00")
	sound := objc.ID(objc.GetClass("NSSound")).Send(objc.RegisterName("soundNamed:"), str)
	if sound == 0 {
		return
	}
	objc.Send[struct{}](sound, objc.RegisterName("stop"))
	objc.Send[struct{}](sound, objc.RegisterName("setVolume:"), float32(0.58))
	objc.Send[bool](sound, objc.RegisterName("play"))
}
