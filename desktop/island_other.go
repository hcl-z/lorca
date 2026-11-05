//go:build !darwin

package main

import "github.com/egoist/mygo"

// islandSupported is false here: the island is a macOS feature.
const islandSupported = false

// polishIslandWindow leaves the window as MyGo made it: the island is a macOS feature, and other
// platforms build the same code without the AppKit window levels behind it.
func polishIslandWindow(w *mygo.Window) {}

func revealIslandWindow(w *mygo.Window) {}

// islandNotch is a display's notch and top strip, which no other platform has.
type islandNotch struct {
	Width  float32
	TopBar float32
}

func islandNotchOf(displayID int64) islandNotch { return islandNotch{} }

func islandMouseDown() bool { return false }

func playIslandSound(e islandSoundEvent) {}
