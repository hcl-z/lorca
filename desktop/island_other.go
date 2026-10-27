//go:build !darwin

package main

import "github.com/egoist/mygo"

// islandSupported is false here: the island is a macOS feature.
const islandSupported = false

// polishIslandWindow leaves the window as MyGo made it: the island is a macOS feature, and other
// platforms build the same code without the AppKit window levels behind it.
func polishIslandWindow(w *mygo.Window) {}

func revealIslandWindow(w *mygo.Window) {}
