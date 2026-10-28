//go:build darwin

package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// Window levels, styles, and behaviors of AppKit, named here because MyGo 0.4.5 exposes neither.
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

func init() {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.RegisterLibFunc(&objectSetClass, lib, "object_setClass")
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
	objc.Send[struct{}](win, objc.RegisterName("setLevel:"), int64(nsStatusWindowLevel))
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
