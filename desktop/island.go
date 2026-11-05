package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The island is the window at the top of every display that says what the bots and coding agents
// are doing while the main window is out of sight: at work, done, or waiting for the user. It is
// its own window over every app, so hiding the main window leaves it up, and it draws native UI on
// a transparent, frameless window, so only the island shows.
//
// Its look, motion, and rules are Cindy's Agent Island (makecindy/cindy, Apache-2.0), ported one
// to one from the SwiftUI helper apps/desktop/native/agent-island/macos-agent-island-helper.swift:
// a black shape hugging the notch, compact as the notch's wings, expanded into a list of sessions
// or one card, springing between them. The sessions and their timers are island_state.go, the
// numbers island_metrics.go, the store's reading island_store.go.

const (
	// The carrier: every window is as large as the expanded island and its inset, centered on a
	// display's top edge, so the island grows inside it instead of resizing the window.
	islandCarrierInset  = 80
	islandWindowWidth   = islandExpandedMaxWidth + islandCarrierInset*2
	islandWindowHeight  = islandExpandedMaxHeight + islandCarrierInset
	islandTopDragSlop   = 2
	islandPointerPoll   = 33 * time.Millisecond
	islandShortcut      = "CmdOrCtrl+Shift+I"
	islandWaveDuration  = 2450 * time.Millisecond
	islandWaveBandWidth = 72
	islandToolbarDelay  = 140 * time.Millisecond
	islandSwipeReveal   = 80
)

// Cindy's colors: the status colors its whole app shares, and the island's fill.
var (
	islandBlack       = ui.RGBA(0, 0, 0, 1)
	islandOrange      = ui.RGBA(255, 102, 0, 1)
	islandBlue        = ui.RGBA(0, 217, 197, 1)
	islandErrorRed    = ui.RGBA(239, 68, 68, 1)
	islandUnreadGreen = ui.RGBA(34, 197, 94, 1)
	islandWhite       = ui.RGBA(255, 255, 255, 1)
)

// The island's springs, as Cindy's SwiftUI ones: spring(response: 0.42, dampingFraction: 0.82) and
// the swipe's settle, spring(response: 0.3, dampingFraction: 0.86). A SwiftUI response is the
// period of the undamped spring, and the damping fraction 1 - bounce, as MyGo's Duration and Bounce.
var (
	islandOpenSpring   = ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.18}}
	islandSettleSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0.14}}
	islandHoverEase    = ui.Animation{Duration: 120 * time.Millisecond, Ease: ui.EaseOut}
)

// islandAskKind is how a question is answered on the island.
type islandAskKind int

const (
	// islandAskChoices is a bot's `ask_user`: one option per row, a click answers.
	islandAskChoices islandAskKind = iota
	// islandAskPermission is an approval: Deny, Always allow, Allow once.
	islandAskPermission
	// islandAskElsewhere is a form the island cannot answer in place: it opens the chat instead.
	islandAskElsewhere
)

// islandAsk is a question waiting for the user, as the island shows it.
type islandAsk struct {
	Kind      islandAskKind
	ChatID    string
	MessageID string
	BotName   string
	Question  string
	// Command is what a permission would run.
	Command string
	// Options are a bot's options by their place.
	Options []model.AskOption
	// Decisions are a permission's buttons, as the card orders them.
	Decisions []model.Answer
}

// islandWindow is one display's window: the native window, its display and notch, and where the
// pointer is over the island.
type islandWindow struct {
	win       *mygo.Window
	display   mygo.Display
	screen    islandScreen
	inMenuBar bool
	inPanel   bool
}

// island holds the island of every display and the machine behind them.
type island struct {
	wins    map[int64]*islandWindow
	m       *islandMachine
	wake    *time.Timer
	wakeAt  time.Time
	hovered int64
	// mouseWasDown follows the button for a click outside the island.
	mouseWasDown bool
	// lastPhases is what the sounds compare the next display with.
	lastPhases  map[string]islandPhase
	soundsUntil map[islandSoundEvent]time.Time
	// swipe is the completed card being swiped, by session, and how far.
	swipe map[string]*islandSwipe
	// shownAt is when the expanded content appeared, for the toolbar's delayed fade.
	shownAt time.Time
	// measured is the expanded content's height, as Cindy's helper reports its own.
	measured float32
	// leaving is the last display that showed a session: drawn while the island goes back in.
	leaving islandDisplay
}

// islandSwipe is a completed card's swipe: its offset, whether it passed the point a release
// commits, and the card it began on.
type islandSwipe struct {
	offset     ui.Point
	armed      bool
	committed  bool
	generation uint64
	at         time.Time
}

// newIsland creates the island's windows and follows the displays: a HUD on one screen of two says
// nothing to a user looking at the other.
func newIsland() *island {
	i := &island{wins: map[int64]*islandWindow{}, m: newIslandMachine(), soundsUntil: map[islandSoundEvent]time.Time{}, swipe: map[string]*islandSwipe{}}
	i.rebuild()
	mygo.Screen.OnDisplaysChanged(func() { post(i.rebuild) })
	// The key works while another app has the keyboard, which is when a question lands; a menu item
	// of the same keys covers the app while it is active.
	if err := mygo.GlobalShortcut.Register(islandShortcut, islandOpen); err != nil {
		log.Printf("island: %s is taken by another app: %v", islandShortcut, err)
	}
	go func() {
		for range time.Tick(islandPointerPoll) {
			post(i.pollPointer)
		}
	}()
	// The island is a visible window of the app, so AppKit reports one to a Dock click even when
	// the main window is hidden. Bring the main window back as a click on the Dock icon does
	// without the island.
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if hasVisibleWindows && app.main != nil && app.main.win != nil && !app.main.win.IsVisible() {
			post(app.reopen)
		}
	})
	return i
}

// islandDisplays are the displays that get a window: every one, or the main one alone.
func islandDisplays() []mygo.Display {
	all := mygo.Screen.Displays()
	if prefs.get().IslandAllDisplays || len(all) < 2 {
		return all
	}
	for _, d := range all {
		if d.Primary {
			return []mygo.Display{d}
		}
	}
	return all[:1]
}

// rebuild puts a carrier window on each display it should be on, and closes the rest.
func (i *island) rebuild() {
	if i == nil || store == nil || !islandSupported {
		return
	}
	i.buildWindows(islandDisplays())
	i.sync()
}

// buildWindows creates the missing windows and closes those of displays left out.
func (i *island) buildWindows(displays []mygo.Display) {
	seen := map[int64]bool{}
	for _, d := range displays {
		seen[d.ID] = true
		screen := islandScreenOf(float32(d.Bounds.Width), islandNotchOf(d.ID))
		if win, ok := i.wins[d.ID]; ok {
			win.display, win.screen = d, screen
			win.win.SetBounds(islandWindowBounds(d))
			continue
		}
		r := islandWindowBounds(d)
		id := d.ID
		win := &islandWindow{display: d, screen: screen}
		win.win = mygo.NewWindow(mygo.WindowOptions{
			Title:             appName() + " Island",
			X:                 r.X,
			Y:                 r.Y,
			Width:             r.Width,
			Height:            r.Height,
			Frameless:         true,
			Transparent:       true,
			AlwaysOnTop:       true,
			Hidden:            true,
			DisableResize:     true,
			DisableMove:       true,
			DisableMinimize:   true,
			DisableMaximize:   true,
			DisableFullScreen: true,
			DisableShadow:     true,
			SkipTaskbar:       true,
			Content:           ui.View(func(c *ui.Context) { i.view(c, id) }),
		})
		polishIslandWindow(win.win)
		win.win.SetBounds(r)
		// Until the pointer is over the island, clicks go to what is under it.
		win.win.SetIgnoreMouseEvents(true)
		i.wins[id] = win
	}
	for id, win := range i.wins {
		if !seen[id] {
			win.win.Close()
			delete(i.wins, id)
		}
	}
}

// missingWindows is whether the windows on screen are not the ones the preferences ask for.
func (i *island) missingWindows(displays []mygo.Display) bool {
	if len(i.wins) != len(displays) {
		return true
	}
	for _, d := range displays {
		if _, ok := i.wins[d.ID]; !ok {
			return true
		}
	}
	return false
}

// islandWindowBounds is the transparent carrier on a display's top edge, centered, as large as the
// expanded island at its widest.
func islandWindowBounds(d mygo.Display) mygo.Rectangle {
	width := min(islandWindowWidth, d.Bounds.Width)
	return mygo.Rectangle{
		X:      d.Bounds.X + (d.Bounds.Width-width)/2,
		Y:      d.Bounds.Y,
		Width:  width,
		Height: islandWindowHeight,
	}
}

// storeChanged reads the store into the machine.
func (i *island) storeChanged() {
	if i == nil || store == nil {
		return
	}
	now := time.Now()
	i.m.observe(islandObservations(store), now)
	if app.notifier != nil {
		i.m.watch(app.notifier.watchedChat(), now)
	}
}

// watchingChanged follows the chat the user looks at in the app.
func (i *island) watchingChanged(chatID string) {
	if i == nil {
		return
	}
	now := time.Now()
	i.m.watch(chatID, now)
	if chatID != "" {
		i.m.read(chatID, false, now)
	}
	i.sync()
}

// sync shows the island in the state the store is in, plays the sound the change calls for, and
// schedules the next of its timers.
func (i *island) sync() {
	if i == nil || store == nil || len(i.wins) == 0 || !islandSupported {
		return
	}
	displays := islandDisplays()
	if i.missingWindows(displays) {
		i.buildWindows(displays)
	}
	now := time.Now()
	d := i.m.display(now)
	i.playSound(d, now)
	present := d.Current != nil || d.Expanded && len(d.Sessions) > 0
	for _, win := range i.wins {
		win.win.Invalidate()
	}
	// With nothing to show the island goes back into the notch and its windows hide after it
	// (retracted); while something shows, they are up.
	switch {
	case !prefs.get().ShowsIsland:
		i.show(false)
	case present:
		i.show(true)
	}
	i.schedule(i.m.nextDeadline(now))
}

// schedule wakes the island when its next timer is due, and not before.
func (i *island) schedule(at time.Time) {
	if at.IsZero() || (i.wake != nil && i.wakeAt.Equal(at)) {
		return
	}
	if i.wake != nil {
		i.wake.Stop()
	}
	i.wakeAt = at
	i.wake = time.AfterFunc(time.Until(at)+time.Millisecond, func() { post(i.sync) })
}

// playSound plays the sound of the change since the island last showed, as Cindy does, unless the
// user turned sounds off or the event played a moment ago.
func (i *island) playSound(d islandDisplay, now time.Time) {
	next := map[string]islandPhase{}
	attention := map[string]bool{}
	for _, s := range d.Sessions {
		next[s.Key] = s.Phase
		attention[s.Key] = s.attention()
	}
	prev := i.lastPhases
	i.lastPhases = next
	if prev == nil || !prefs.get().IslandSounds || !prefs.get().ShowsIsland {
		return
	}
	event := islandSoundFor(prev, next, attention)
	if event == islandSoundNone || i.soundsUntil[event].After(now) {
		return
	}
	i.soundsUntil[event] = now.Add(islandSoundCooldown(event))
	playIslandSound(event)
}

// islandLayout is the island's size and shape on one display, as Cindy's AgentIslandLayout.
type islandLayout struct {
	Width, Height float32
	TopExtension  float32
	BottomRadius  float32
	MenuBarZone   float32
	Expanded      bool
	HasNotch      bool
	NotchWidth    float32
	ShadowRadius  float32
	ShadowY       float32
	ShadowOpacity float32
}

// hardwareNotchHidden is a compact island no wider than the notch: nothing of it shows.
func (l islandLayout) hardwareNotchHidden() bool {
	return l.HasNotch && !l.Expanded && l.Width <= l.NotchWidth+1
}

// islandLayoutFor is the island's layout for what it shows on a screen, as Cindy's
// AgentIslandLayout.compute.
func islandLayoutFor(d islandDisplay, s islandScreen, carrierWidth float32, measured float32, hovered bool) islandLayout {
	expanded := d.Expanded
	hasSession := d.Current != nil || len(d.Sessions) > 0
	// The notch's layout is only for an island centered on a notched screen, which it always is.
	l := islandLayout{Expanded: expanded, HasNotch: s.HasNotch, NotchWidth: s.NotchWidth, MenuBarZone: max(8, s.TopBar)}
	available := max(1, carrierWidth-islandCarrierInset*2)
	if expanded {
		least := float32(islandExpandedMinWidth)
		if s.HasNotch {
			least = max(least, s.NotchWidth+islandExpandedNotchSide*2+islandExpandedNotchPadding)
		}
		l.Width = min(max(islandExpandedWidthFor(s, hasSession, available), min(least, available)), available)
		approval := d.Current != nil && d.Current.Ask != nil && d.Current.Ask.Kind == islandAskPermission
		l.Height = islandExpandedHeightFor(len(d.Sessions), d.Surface == islandSurfaceList, measured, approval)
		l.TopExtension, l.BottomRadius = islandExpandedTopExtension, islandExpandedBottomRadius
		l.ShadowRadius, l.ShadowY, l.ShadowOpacity = 6, 0, 0.7
	} else {
		badge := float32(0)
		if s.HasNotch && hasSession {
			badge = islandBadgeWidth(d.Pill, true)
		}
		l.Width = min(islandCompactWidth(s, hasSession, badge), carrierWidth-40)
		l.Height = max(8, s.TopBar)
		l.TopExtension, l.BottomRadius = islandClosedTopExtension, islandClosedBottomRadius
		if hasSession {
			l.ShadowRadius, l.ShadowY, l.ShadowOpacity = 7, 2, 0.46
		} else {
			l.ShadowRadius, l.ShadowY, l.ShadowOpacity = 6, 0, 0.7
		}
	}
	if !d.Shadow && !hovered {
		l.ShadowOpacity = 0
	}
	return l
}

// islandNotchPath is the island's silhouette, as Cindy's NotchShape (after Code Island's MIT
// NotchPanelShape): a top edge reaching ext past each side into the menu bar, concave shoulders
// down into the sides, and bottom corners of continuous curvature.
func islandNotchPath(r ui.Rect, ext, bottomRadius, minHeight float32) *ui.Path {
	minX, maxX, minY := r.X, r.X+r.W, r.Y
	maxY := max(r.Y+r.H, minY+minHeight)
	br := min(bottomRadius, r.W/4, (maxY-minY)/2)
	const k = 0.62
	var p ui.Path
	p.MoveTo(minX-ext, minY)
	p.LineTo(maxX+ext, minY)
	p.CubeTo(maxX+ext*0.35, minY, maxX, minY+ext*0.35, maxX, minY+ext)
	p.LineTo(maxX, maxY-br)
	p.CubeTo(maxX, maxY-br*(1-k), maxX-br*(1-k), maxY, maxX-br, maxY)
	p.LineTo(minX+br, maxY)
	p.CubeTo(minX+br*(1-k), maxY, minX, maxY-br*(1-k), minX, maxY-br)
	p.LineTo(minX, minY+ext)
	p.CubeTo(minX, minY+ext*0.35, minX-ext*0.35, minY, minX-ext, minY)
	p.Close()
	return &p
}

// pollPointer reads the pointer against each display's island, as Cindy's tracking layer does: over
// the menu bar strip or the panel, the window takes clicks and the machine hears of it; away, clicks
// pass through. A press away from an expanded island is a click outside it.
func (i *island) pollPointer() {
	if i == nil || store == nil || !prefs.get().ShowsIsland {
		return
	}
	now := time.Now()
	d := i.m.display(now)
	if d.Current == nil && !(d.Expanded && len(d.Sessions) > 0) {
		// Nothing shows: the island takes no pointer, and what was over it is no longer.
		for _, win := range i.wins {
			if win.inMenuBar || win.inPanel {
				win.inMenuBar, win.inPanel = false, false
				win.win.SetIgnoreMouseEvents(true)
			}
		}
		if i.m.inMenuBar || i.m.inPanel {
			i.m.hover(false, false, now)
		}
		return
	}
	p := mygo.Screen.CursorScreenPoint()
	down := islandMouseDown()
	pressed := down && !i.mouseWasDown
	i.mouseWasDown = down
	changed := false
	anyMenuBar, anyPanel := false, false
	for id, win := range i.wins {
		if win.win.IsDestroyed() || !win.win.IsVisible() {
			continue
		}
		bounds := win.win.Bounds()
		l := islandLayoutFor(d, win.screen, float32(bounds.Width), i.measured, win.inMenuBar || win.inPanel)
		if l.hardwareNotchHidden() && !d.Expanded {
			// Hidden behind the notch, the island still opens under the pointer, as Cindy's does.
			l.Width = l.NotchWidth
		}
		left := float32(win.display.Bounds.X) + (float32(win.display.Bounds.Width)-l.Width)/2 - l.TopExtension
		right := left + l.Width + 2*l.TopExtension
		top := float32(win.display.Bounds.Y) - islandTopDragSlop
		bottom := float32(win.display.Bounds.Y) + l.Height
		x, y := float32(p.X), float32(p.Y)
		inside := x >= left && x < right && y >= top && y < bottom
		menuBar := inside && (!l.Expanded || y < float32(win.display.Bounds.Y)+l.MenuBarZone)
		panel := inside && !menuBar
		if menuBar != win.inMenuBar || panel != win.inPanel {
			win.inMenuBar, win.inPanel = menuBar, panel
			win.win.SetIgnoreMouseEvents(!inside)
			changed = true
		}
		if inside {
			i.hovered = id
		}
		anyMenuBar = anyMenuBar || menuBar
		anyPanel = anyPanel || panel
		if pressed && !inside && l.Expanded {
			i.m.outsideClick(now)
			changed = true
		}
	}
	if anyMenuBar != i.m.inMenuBar || anyPanel != i.m.inPanel {
		i.m.hover(anyMenuBar, anyPanel, now)
		changed = true
	}
	if changed {
		i.sync()
	}
}

// show raises or hides every display's island. ShowInactive brings the window to the front without
// activating the app or taking the keyboard from the field the user is typing in; Show would do
// both.
func (i *island) show(visible bool) {
	for _, win := range i.wins {
		if win.win.IsDestroyed() {
			continue
		}
		if !visible {
			if win.win.IsVisible() {
				win.win.Hide()
			}
			continue
		}
		if !win.win.IsVisible() {
			win.win.ShowInactive()
			revealIslandWindow(win.win)
		}
	}
}

// invalidate redraws every display's island. It is the island's own redraw: its windows are not in
// windows(), so invalidateWindows leaves them out.
func (i *island) invalidate() {
	if i == nil {
		return
	}
	for _, win := range i.wins {
		if !win.win.IsDestroyed() {
			win.win.Invalidate()
		}
	}
}

// view draws one display's island: the island alone, since the window and the root are transparent.
// Only the display under the pointer expands into the list, as Cindy's expandedDisplayId.
func (i *island) view(c *ui.Context, displayID int64) {
	applyTheme(c)
	c.Root().Background(ui.Transparent)
	if store == nil {
		return
	}
	win := i.wins[displayID]
	if win == nil {
		return
	}
	d := i.m.last
	if d.Surface == islandSurfaceList && i.hovered != displayID && len(i.wins) > 1 {
		d = islandDisplay{Current: d.Current, Sessions: d.Sessions, Pill: d.Pill}
		if d.Current == nil && len(d.Sessions) > 0 {
			d.Current = d.Sessions[0]
		}
	}
	i.draw(c, d, win.screen, win.inMenuBar || win.inPanel)
}

// draw paints the island for what it shows on a screen. The tests draw a display without a store's
// windows behind it.
//
// With nothing to show the island is gone. It comes out of the notch as its first session arrives:
// on a notched display it starts as the notch itself, black on black, and its wings spring out;
// without a notch it starts as a sliver at the top edge and springs down and out. Its content fades
// in a moment after the shape starts. When its last session goes, the content fades first and the
// shape goes back in, drawing the session it last showed until it is gone.
func (i *island) draw(c *ui.Context, d islandDisplay, s islandScreen, hovered bool) {
	c.Root().Background(ui.Transparent)
	carrier := float32(islandWindowWidth)
	present := d.Current != nil || d.Expanded && len(d.Sessions) > 0
	if present {
		i.leaving = d
	} else if i.leaving.Current != nil {
		// Gone: keep drawing what was last shown while the shape retracts around it.
		d = i.leaving
	}
	l := islandLayoutFor(d, s, carrier, i.measured, hovered)
	target := l
	if !present {
		target = islandHiddenLayout(l, s)
	}
	ui.Column(c).Fill().AlignItems(ui.Center).Children(func() {
		body := ui.Box(c.Key("island"))
		// A value's first frame is its target as it is, so an island built empty starts from the
		// hidden shape, and the first session grows it out of the notch.
		w := body.AnimateTo("w", target.Width, islandOpenSpring)
		h := body.AnimateTo("h", target.Height, islandOpenSpring)
		ext := body.AnimateTo("ext", target.TopExtension, islandOpenSpring)
		br := body.AnimateTo("br", target.BottomRadius, islandOpenSpring)
		shadow := body.AnimateTo("shadow", target.ShadowOpacity, islandOpenSpring)
		sr := body.AnimateTo("sr", target.ShadowRadius, islandOpenSpring)
		sy := body.AnimateTo("sy", target.ShadowY, islandOpenSpring)
		// The content follows the shape in and leads it out.
		fade := ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut, Delay: islandContentDelay}
		if !present {
			fade = ui.Animation{Duration: 120 * time.Millisecond, Ease: ui.EaseIn}
		}
		contentOpacity := body.AnimateTo("content", boolOpacity(present), fade)
		if !present && i.leaving.Current != nil && w <= islandHiddenLayout(l, s).Width+0.5 && contentOpacity < 0.01 {
			// Back in the notch: nothing is left to draw, and the window can go.
			i.leaving = islandDisplay{}
			post(i.retracted)
		}
		if w < 1 || h < 1 {
			return
		}
		menuBar := min(l.MenuBarZone, h)
		// The shape reaches past the body by ext each side; the body's box is the shape's sides.
		body.Size(w, h).Draw(func(p *ui.Painter, r ui.Rect) {
			path := islandNotchPath(r, ext, br, menuBar)
			if shadow > 0.001 {
				p.Shadow(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: max(r.H, menuBar)}, br, 0, sy, sr*2, 0, ui.RGBA(0, 0, 0, shadow))
			}
			p.FillPath(path, islandBlack)
		})
		body.Clip().Radius(0, 0, br, br)
		live := l
		live.Width, live.Height = w, h
		body.Children(func() {
			ui.Box(c).Opacity(contentOpacity).Children(func() { i.content(c, d, live) })
		})
		if present && !d.Expanded && body.Clicked() {
			i.m.expand(time.Now())
			i.sync()
		}
	})
}

// islandContentDelay is how long the content waits after the shape starts growing, so the words
// come in on a shape already wide enough for them.
const islandContentDelay = 90 * time.Millisecond

// islandHiddenLayout is the island gone: the notch's own size on a notched display, black on its
// black, else a sliver along the top edge as narrow as the simulated notch.
func islandHiddenLayout(l islandLayout, s islandScreen) islandLayout {
	h := l
	h.Expanded = false
	h.ShadowOpacity = 0
	h.TopExtension, h.BottomRadius = islandClosedTopExtension, islandClosedBottomRadius
	if s.HasNotch {
		h.Width, h.Height = s.NotchWidth, max(8, s.TopBar)
		return h
	}
	h.Width, h.Height = s.NotchWidth*0.6, 0
	h.TopExtension, h.BottomRadius = 0, 0
	return h
}

func boolOpacity(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

// retracted hides the windows once the island has gone back in, unless something arrived meanwhile.
func (i *island) retracted() {
	if i == nil || len(i.wins) == 0 {
		return
	}
	if d := i.m.display(time.Now()); d.Current == nil && !d.Expanded {
		i.show(false)
	}
}

// content is what the island holds: the expanded list or card, or the compact session.
func (i *island) content(c *ui.Context, d islandDisplay, l islandLayout) {
	// The toolbar's fade counts from the island opening; folded, it starts over.
	if !d.Expanded {
		i.shownAt = time.Time{}
	}
	switch {
	case d.Current != nil && d.Expanded:
		i.expandedSessions(c, d, l)
	case d.Current != nil:
		i.compactSession(c, d, l)
	}
}

// MARK: - Compact

// compactSession is the compact island of a session: on a notched screen its two wings, else one
// line, as Cindy's CompactSessionView.
func (i *island) compactSession(c *ui.Context, d islandDisplay, l islandLayout) {
	s := d.Current
	switch {
	case l.hardwareNotchHidden():
	case l.HasNotch:
		side := max(32, (l.Width-l.NotchWidth)/2)
		reveal := islandNotchTextOpacity(side)
		ui.Row(c).Width(l.Width).Height(l.Height).AlignItems(ui.Center).Children(func() {
			// The leading wing: the status dot and the title.
			ui.Row(c).Width(side).Height(l.Height).Clip().AlignItems(ui.Center).Gap(6).
				Padding(0, 0, 0, islandNotchSideInset(side, 18)).Children(func() {
				islandStatusDot(c, s, islandStatusIcon, true)
				islandLineText(c, s, i.compactTitle(d), 10, 600, islandIdentityColor(s), titleOpacity(s)).Opacity(reveal)
			})
			ui.Spacer(c).MinWidth(l.NotchWidth)
			// The trailing wing: the detail and the badge.
			ui.Row(c).Width(side).Height(l.Height).Clip().AlignItems(ui.Center).Justify(ui.End).Gap(5).
				Padding(0, islandNotchSideInset(side, 22), 0, 0).Children(func() {
				if sub := s.subtitle(); sub != "" {
					islandLineText(c, s, sub, 9, 400, islandWhite, 0.56).Shrink(1).MinWidth(0).Opacity(reveal)
				}
				islandPillBadge(c, d.Pill, true)
			})
		})
	default:
		padding := float32(11)
		if l.Width <= 96 {
			padding = 7
		}
		showsDetail := l.Width >= islandDetailCollapseWidth && s.subtitle() != ""
		showsBadge := l.Width >= islandBadgeCollapseWidth
		ui.Row(c).Width(l.Width).Height(l.Height).Padding(0, padding).Gap(7).AlignItems(ui.Center).Children(func() {
			// The line takes the width the badge leaves, which pushes the badge to the edge: Cindy's
			// line has layout priority over its spacer, which MyGo's rows have no word for.
			i.compactTitleLine(c, d, l.Width-padding*2, showsDetail, showsBadge)
			if showsBadge {
				islandPillBadge(c, d.Pill, true)
			}
		})
	}
}

// compactTitleLine is the status dot, the title, and its detail when there is room.
func (i *island) compactTitleLine(c *ui.Context, d islandDisplay, width float32, showsDetail, showsBadge bool) {
	s := d.Current
	title := i.compactTitle(d)
	const spacing = 7
	ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(spacing).AlignItems(ui.Center).Children(func() {
		islandStatusDot(c, s, islandStatusIcon, true)
		if showsDetail {
			container := width
			if showsBadge {
				container -= islandBadgeWidth(d.Pill, true) + 7
			}
			limit := islandTitleMaxWidth(container, spacing, title)
			measured, _ := c.MeasureText(0, ui.Span{Text: title, Size: 11, Weight: 600})
			islandLineText(c, s, title, 11, 600, islandIdentityColor(s), titleOpacity(s)).Width(min(limit, measured+1)).Shrink(0)
			islandLineText(c, s, s.subtitle(), 10, 400, islandWhite, 0.58).Grow(1).Shrink(1).MinWidth(0)
			return
		}
		islandLineText(c, s, title, 11, 600, islandIdentityColor(s), titleOpacity(s)).MinWidth(islandTitleMinWidth(title)).Shrink(1)
	})
}

func (i *island) compactTitle(d islandDisplay) string { return d.Current.displayTitle() }

// titleOpacity dims a running session's title, which the shimmer brightens as it passes.
func titleOpacity(s *islandSession) float32 {
	if s.Phase == islandRunning {
		return 0.66
	}
	return 1
}

// islandLineText is a compact line: shimmering while the session runs, as Cindy's
// AgentIslandWaveText.
func islandLineText(c *ui.Context, s *islandSession, text string, size float32, weight int, color ui.Color, opacity float32) ui.Element {
	if s.Phase == islandRunning {
		return islandWaveText(c, text, size, weight, color, opacity)
	}
	return ui.Text(c, text).FontSize(size).FontWeight(weight).TextColor(color.Alpha(opacity)).SingleLine()
}

// islandWaveText is the text in its color at opacity, with a 72-point band of light sweeping across
// it every 2.45 seconds, as Cindy's wave text: the band's gradient, from clear to the color at 22%,
// white at 88% in its middle, the color at 30%, and clear, painted over the glyphs. MyGo masks no
// fill by text, so the band paints the text again under a moving clip, each slice in the band's
// color at its place.
func islandWaveText(c *ui.Context, text string, size float32, weight int, color ui.Color, opacity float32) ui.Element {
	t := ui.Text(c, text).FontSize(size).FontWeight(weight).TextColor(color.Alpha(opacity)).SingleLine()
	progress := t.Loop("wave", islandWaveDuration, ui.EaseInOut)
	t.DrawOver(func(p *ui.Painter, r ui.Rect) {
		x0 := r.X - islandWaveBandWidth + progress*(r.W+2*islandWaveBandWidth)
		const slices = 12
		slice := float32(islandWaveBandWidth) / slices
		w, h := p.MeasureText(0, ui.Span{Text: text, Size: size, Weight: weight})
		ty := r.Y + (r.H-h)/2
		for n := 0; n < slices; n++ {
			at := (float32(n) + 0.5) / slices
			band := islandWaveColor(color, at)
			if band.A == 0 {
				continue
			}
			sx := x0 + float32(n)*slice
			clip := ui.Rect{X: max(sx, r.X), Y: r.Y, W: min(sx+slice, r.X+min(r.W, w)) - max(sx, r.X), H: r.H}
			if clip.W <= 0 {
				continue
			}
			p.Clip(clip, 0, func() {
				p.RichText(r.X, ty, 0, ui.Span{Text: text, Size: size, Weight: weight, Color: band})
			})
		}
	})
	return t
}

// islandWaveColor is the wave's band at a place along it, from 0 to 1.
func islandWaveColor(color ui.Color, at float32) ui.Color {
	stops := []struct {
		at float32
		c  ui.Color
	}{
		{0, color.Alpha(0)},
		{0.34, color.Alpha(0.22)},
		{0.50, islandWhite.Alpha(0.88)},
		{0.66, color.Alpha(0.30)},
		{1, color.Alpha(0)},
	}
	for n := 1; n < len(stops); n++ {
		if at <= stops[n].at {
			a, b := stops[n-1], stops[n]
			return a.c.Mix(b.c, (at-a.at)/(b.at-a.at))
		}
	}
	return color.Alpha(0)
}

// islandIdentityColor is a session's title color: orange at work, blue asking, red failed, else
// white.
func islandIdentityColor(s *islandSession) ui.Color {
	switch s.Phase {
	case islandRunning:
		return islandOrange
	case islandNeedsYou:
		return islandBlue
	case islandFailed:
		return islandErrorRed
	}
	return islandWhite.Alpha(0.92)
}

// islandAccent is a session's accent: the same, with a dimmer white.
func islandAccent(s *islandSession) ui.Color {
	switch s.Phase {
	case islandRunning:
		return islandOrange
	case islandNeedsYou:
		return islandBlue
	case islandFailed:
		return islandErrorRed
	}
	return islandWhite.Alpha(0.68)
}

// islandPillBadge is the compact badge: what waits on the user, a dot, and what runs, in their
// colors, or the number of sessions, as Cindy's PillBadge.
func islandPillBadge(c *ui.Context, p islandPill, compact bool) {
	size, height := float32(10), float32(18)
	if !compact {
		size, height = 11, 20
	}
	attention := islandUnreadGreen
	if p.Pending > 0 {
		attention = islandBlue
	}
	ui.Row(c).MinWidth(islandBadgeWidth(p, compact)).MinHeight(height).Padding(0, 2).Gap(1).
		Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
		for _, run := range islandBadgeRuns(p) {
			color := islandWhite.Alpha(0.92)
			switch run.Tone {
			case islandBadgeAttention:
				color = attention
			case islandBadgeDot:
				color = islandWhite.Alpha(0.42)
			case islandBadgeRunning:
				color = islandOrange
			}
			ui.Text(c, run.Text).Font(monoFont).FontSize(size).FontWeight(600).TextColor(color).SingleLine()
		}
	})
}

// islandStatusDot is a session's mark, as Cindy's StatusDot: at work, the agent's mark or the bot's
// avatar breathing; asking, done, or failed, a glyph on a tint of the accent.
func islandStatusDot(c *ui.Context, s *islandSession, side float32, compact bool) ui.Element {
	scale := side / 20
	accent := islandAccent(s)
	box := ui.Box(c).Size(side, side).Shrink(0).Justify(ui.Center).AlignItems(ui.Center)
	box.Children(func() {
		if s.Phase == islandRunning {
			islandRunningMark(c, s, side, compact)
			return
		}
		if s.Phase == islandFailed {
			islandFailedMark(c, s, side)
			return
		}
		fill, ink := float32(0.13), float32(0.96)
		if s.Phase == islandCompleted {
			fill, ink = 0.08, 0.78
		}
		ui.Box(c).Size(side, side).Radius(side / 2).Background(accent.Alpha(fill)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			name, glyph := "questionmark.bubble", float32(11)
			switch s.Phase {
			case islandCompleted:
				name = "checkmark"
			case islandFailed:
				name, glyph = "exclamationmark.triangle", 10
			}
			symbol(c, name, glyph*scale, 2.4).TextColor(accent.Alpha(ink))
		})
	})
	return box
}

// islandRunningMark is a session at work: its mark in orange, breathing from 0.3 to 1 over 0.75
// seconds and back, as Cindy's vendor icon. A coding agent wears its own mark; a bot wears its
// symbol the same way, so every session at work reads alike.
func islandRunningMark(c *ui.Context, s *islandSession, side float32, compact bool) {
	holder := ui.Box(c).Size(side, side).Justify(ui.Center).AlignItems(ui.Center)
	breath := holder.Loop("breathe", 1500*time.Millisecond, ui.Linear)
	// easeInOut, there and back: 0.75 seconds each way.
	phase := breath * 2
	if phase > 1 {
		phase = 2 - phase
	}
	opacity := 0.3 + 0.7*ui.EaseInOut(phase)
	holder.Children(func() {
		if s.AgentKind != "" {
			mark := float32(12)
			if s.AgentKind == "claude" || s.AgentKind == "claude-acp" {
				mark = 13
			}
			mark *= side / 20
			ui.Box(c).Size(mark, mark).Opacity(opacity).Children(func() { ui.Image(c, agentMark(s.AgentKind)).Fill() })
			return
		}
		name := "sparkles"
		if bot := store.Bot(s.BotID); bot != nil && bot.SymbolName != "" {
			name = bot.SymbolName
		}
		symbol(c, name, 13*side/20, 2.2).TextColor(islandOrange).Opacity(opacity)
	})
}

// toolbar is the expanded island's three buttons, as Cindy's ExpandedIslandToolbarControls: sound,
// settings, and a new chat, fading in 0.14 seconds after the island opens. Only their opacity
// waits: they stand where the opening island's layout puts them from its first frame, as SwiftUI's
// opacity does, rather than following it there late as a transition of their place would.
func (i *island) toolbar(c *ui.Context) {
	bar := ui.Row(c.Key("island-toolbar")).Gap(5).AlignItems(ui.Center)
	if i.shownAt.IsZero() {
		i.shownAt = c.Now()
	}
	shown := c.Now().Sub(i.shownAt) - islandToolbarDelay
	opacity := float32(0)
	if shown > 0 {
		opacity = ui.EaseOut(min(1, float32(shown)/float32(120*time.Millisecond)))
	}
	if opacity < 1 {
		c.AnimationFrame()
	}
	bar.Opacity(opacity)
	if opacity == 0 {
		// Before they show, the buttons take no clicks, as Cindy's allowsHitTesting(false).
		bar.PassThrough()
	}
	bar.Children(func() {
		sounds := prefs.get().IslandSounds
		name, base, help := "speaker.wave.2.fill", float32(0.64), L("Mute island sounds")
		if !sounds {
			name, base, help = "speaker.slash.fill", 0.48, L("Play island sounds")
		}
		if islandToolbarButton(c, "sound", name, help, base) {
			on := !sounds
			setPrefs(PreferencesPatch{IslandSounds: &on})
		}
		if islandToolbarButton(c, "settings", "gearshape.fill", L("Settings"), 0.60) {
			i.m.collapse(time.Now())
			app.showSettings()
			i.sync()
		}
		if islandToolbarButton(c, "new", "square.and.pencil", L("New Session"), 0.60) {
			i.m.collapse(time.Now())
			runCommand("newBot")
			i.sync()
		}
	})
}

// islandToolbarButton is one 23-point round button of the toolbar.
func islandToolbarButton(c *ui.Context, key, name, help string, base float32) bool {
	b := ui.ButtonBase(c.Key("island-tool-"+key)).Size(23, 23).Radius(11.5).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Label(help)
	hovered := b.Hovered()
	fill, ink := float32(0.045), base
	if hovered {
		fill, ink = 0.12, min(0.82, base+0.18)
	}
	b.Background(b.AnimateColor("bg", islandWhite.Alpha(fill), islandHoverEase))
	b.Border(1, b.AnimateColor("border", islandWhite.Alpha(fill), islandHoverEase))
	b.Children(func() { symbol(c, name, 10.5, 2.4).TextColor(islandWhite.Alpha(ink)) })
	return b.Clicked()
}

// MARK: - Expanded

// expandedSessions is the expanded island, as Cindy's ExpandedSessionsView: the top bar, then the
// list of sessions or the one card.
func (i *island) expandedSessions(c *ui.Context, d islandDisplay, l islandLayout) {
	col := ui.Column(c.Key(fmt.Sprint("island-expanded-", d.Surface))).Width(l.Width).Gap(islandBodySpacing)
	col.Transition(ui.ElementTransition{Duration: 200 * time.Millisecond, Colors: true, Enter: &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0}})
	col.Children(func() {
		i.topBar(c, d.Current, l)
		list := d.Surface == islandSurfaceList && len(d.Sessions) > 1
		body := ui.Column(c).FillWidth().Padding(0, islandCardHorizontalPadding, islandBottomPadding, islandCardHorizontalPadding)
		body.Children(func() {
			if !list {
				i.sessionRow(c, d.Current)
				return
			}
			rows := func() {
				ui.Column(c).FillWidth().Gap(islandRowGap).Children(func() {
					for _, s := range d.Sessions {
						i.sessionRow(c, s)
					}
				})
			}
			if len(d.Sessions) >= islandMaxVisibleRows {
				ui.Scroll(c).FillWidth().Height(islandMaxVisibleRows * islandRowSlot).Children(rows)
				return
			}
			rows()
		})
	})
	col.Draw(func(p *ui.Painter, r ui.Rect) {
		// The content's height, as Cindy's helper measures and reports it, sizes the island.
		if h := float32(int(r.H + 0.999)); h > 0 && (h-i.measured > 1 || i.measured-h > 1) {
			i.measured = h
			post(i.invalidate)
		}
	})
}

// topBar is the expanded island's top strip: the session's mark at the left, the toolbar at the
// right, around the notch on a notched screen.
func (i *island) topBar(c *ui.Context, current *islandSession, l islandLayout) {
	bar := ui.Row(c).FillWidth().Height(islandTopBarHeight).Padding(0, islandCardHorizontalPadding).AlignItems(ui.Center)
	bar.Children(func() {
		if l.HasNotch {
			inner := l.Width - islandCardHorizontalPadding*2
			notch := min(l.NotchWidth, max(0, inner))
			side := max(0, (inner-notch)/2)
			ui.Row(c).Width(side).Clip().AlignItems(ui.Center).Gap(8).Children(func() { islandTopBarMark(c, current) })
			ui.Box(c).Width(notch)
			ui.Row(c).Width(side).Clip().Justify(ui.End).AlignItems(ui.Center).Children(func() { i.toolbar(c) })
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() { islandTopBarMark(c, current) })
		ui.Spacer(c).MinWidth(8)
		i.toolbar(c)
	})
	bar.Padding(islandTopPadding, islandCardHorizontalPadding, 0, islandCardHorizontalPadding).Height(islandTopBarHeight + islandTopPadding)
}

// islandTopBarMark is the leading mark of the top bar: the current session's, while it runs or
// waits, as Cindy's running mascot.
func islandTopBarMark(c *ui.Context, s *islandSession) {
	if s == nil {
		return
	}
	islandStatusDot(c, s, 18, true)
}

// sessionRow is one session in the list, or the one card, as Cindy's ExpandedSessionRow: a 32-point
// mark, its title and status, its summary, and the approval buttons when it asks for one. A
// completed card swipes right to be marked read.
func (i *island) sessionRow(c *ui.Context, s *islandSession) {
	if s.Phase == islandCompleted && s.Generation != 0 {
		i.swipeRow(c, s)
		return
	}
	i.card(c, s, 0)
}

// card is the row's rounded card.
func (i *island) card(c *ui.Context, s *islandSession, offset float32) ui.Element {
	card := ui.Column(c.Key("island-card-"+s.Key)).FillWidth().MinHeight(islandRowHeight).Padding(12, 14).Gap(4).
		Radius(12).Cursor(ui.CursorPointer).Label(s.displayTitle())
	hovered := card.Hovered()
	base, stroke := float32(0.035), float32(0)
	fill := islandWhite
	switch s.Phase {
	case islandNeedsYou:
		base, stroke = 0.065, 0.18
	case islandFailed:
		fill, base, stroke = islandFailCard()
	}
	if hovered {
		base += 0.035
		stroke = max(stroke, 0.10)
	}
	card.Background(card.AnimateColor("bg", fill.Alpha(base), islandHoverEase))
	card.Border(1, card.AnimateColor("border", islandAccent(s).Alpha(stroke), islandHoverEase))
	if offset != 0 {
		card.Translate(offset, 0)
	}
	approval := s.Ask != nil && s.Ask.Kind == islandAskPermission
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(islandRowLeading, islandRowLeading).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
				islandStatusDot(c, s, 26, false)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
				i.headerLine(c, s)
				i.summaryLine(c, s)
			})
		})
		switch {
		case approval:
			ui.Box(c).Height(4)
			i.approvalActions(c, s)
		case s.Ask != nil && s.Ask.Kind == islandAskChoices && len(s.Ask.Options) > 0:
			// Lorca answers a bot's question in place: its options as rows, as before the port.
			for n, option := range s.Ask.Options {
				place := n
				i.optionRow(c, s, n, option, func() {
					store.AnswerAsk(s.Ask.ChatID, s.Ask.MessageID, []int{place}, "", func(error) { i.sync() })
				})
			}
		}
	})
	if !approval && card.Clicked() {
		i.open(s)
	}
	return card
}

// headerLine is the row's title and, at its right, the Review button of a question it cannot
// answer in place or the word of its state.
func (i *island) headerLine(c *ui.Context, s *islandSession) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, s.Title).FontSize(13).FontWeight(500).TextColor(islandWhite.Alpha(0.92)).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c).MinWidth(8)
		if s.Phase == islandNeedsYou && (s.Ask == nil || s.Ask.Kind == islandAskElsewhere) {
			b := ui.ButtonBase(c.Key("island-review-"+s.Key)).Height(22).Padding(0, 8).Gap(4).Radius(11).AlignItems(ui.Center).
				Background(islandBlue.Alpha(0.16)).Cursor(ui.CursorPointer).Label(L("Review")).Shrink(0)
			b.Children(func() {
				symbol(c, "arrowshape.turn.up.right.fill", 9, 2.6).TextColor(islandBlue.Alpha(0.98))
				ui.Text(c, L("Review")).FontSize(11).FontWeight(500).TextColor(islandBlue.Alpha(0.98)).SingleLine()
			})
			if b.Clicked() {
				i.open(s)
			}
			return
		}
		if s.Phase == islandFailed {
			islandFailLabel(c, s.statusLabel())
			return
		}
		ink := float32(0.85)
		if s.attention() {
			ink = 0.95
		}
		ui.Text(c, s.statusLabel()).FontSize(11).TextColor(islandAccent(s).Alpha(ink)).SingleLine().Height(20).Shrink(0)
	})
}

// summaryLine is what the session last said or did, with a person before the user's own words.
func (i *island) summaryLine(c *ui.Context, s *islandSession) {
	summary := s.summary()
	ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
		ink := float32(0.62)
		if summary.IsUser {
			symbol(c, "person.fill", 9, 2.4).TextColor(islandBlue)
			ink = 0.78
		}
		if s.Phase == islandFailed {
			// The error is the card's message: read as plainly as a reply.
			ink = 0.82
		}
		ui.Text(c, islandCompactText(summary.Text)).FontSize(12).TextColor(islandWhite.Alpha(ink)).SingleLine().Grow(1).Shrink(1).MinWidth(0)
	})
}

// approvalActions are an approval's buttons, as Cindy's PermissionApprovalActions: Deny at the left,
// then Always allow and Allow once at the right, Allow once in blue.
func (i *island) approvalActions(c *ui.Context, s *islandSession) {
	ask := s.Ask
	var deny, always, once *model.Answer
	for n := range ask.Decisions {
		switch ask.Decisions[n].Decision {
		case "deny":
			deny = &ask.Decisions[n]
		case "always":
			always = &ask.Decisions[n]
		case "allow":
			once = &ask.Decisions[n]
		}
	}
	answer := func(decision string) {
		store.AnswerPermission(ask.ChatID, ask.MessageID, decision)
		i.sync()
	}
	ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
		if deny != nil && islandPermissionButton(c, s.Key+"-deny", deny.Title, "xmark", false) {
			answer("deny")
		}
		ui.Spacer(c).MinWidth(4)
		if always != nil && islandPermissionButton(c, s.Key+"-always", always.Title, "checkmark.seal", false) {
			answer("always")
		}
		if once != nil && islandPermissionButton(c, s.Key+"-allow", once.Title, "checkmark", true) {
			answer("allow")
		}
	})
}

// islandPermissionButton is one capsule of the approval row: blue on a blue tint for the primary,
// white on a faint fill for the others, each stronger under the pointer.
func islandPermissionButton(c *ui.Context, key, label, icon string, primary bool) bool {
	b := ui.ButtonBase(c.Key("island-perm-"+key)).Height(24).Padding(0, 9).Gap(5).Radius(12).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Label(label).Shrink(0)
	hovered := b.Hovered()
	ink, fill := islandWhite.Alpha(0.76), islandWhite.Alpha(0.08)
	if hovered {
		fill = islandWhite.Alpha(0.16)
	}
	if primary {
		ink, fill = islandBlue.Alpha(0.98), islandBlue.Alpha(0.16)
		if hovered {
			fill = islandBlue.Alpha(0.24)
		}
	}
	b.Background(fill)
	b.Children(func() {
		symbol(c, icon, 9, 2.8).TextColor(ink)
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(ink).SingleLine()
	})
	return b.Clicked()
}

// optionRow is one of a bot's options under its question: its number, its words, and its
// description, a click answering with it.
func (i *island) optionRow(c *ui.Context, s *islandSession, n int, option model.AskOption, answer func()) {
	row := ui.ButtonBase(c.Key(fmt.Sprint("island-option-", s.Key, "-", n))).FillWidth().MinHeight(36).Padding(6, 10).Gap(8).
		Radius(10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(option.Label)
	fill := float32(0.06)
	if row.Hovered() {
		fill = 0.12
	}
	row.Background(row.AnimateColor("bg", islandWhite.Alpha(fill), islandHoverEase))
	press := ui.Pose{}
	if row.Pressed() {
		press.Scale = 0.98
	}
	row.AnimatePose(press, ui.Snappy)
	anim := ui.Animation{Spring: ui.SpringOptions{Duration: 320 * time.Millisecond, Bounce: 0.12}, Delay: time.Duration(n) * 30 * time.Millisecond}
	// Only their fade animates: their width follows the island's spring at once, not on a delay.
	row.Transition(ui.ElementTransition{Animation: &anim, Colors: true, Enter: &ui.Motion{Opacity: 0}})
	row.Children(func() {
		ui.Box(c).Size(20, 20).Radius(6).Background(islandBlue.Alpha(0.16)).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprint(n+1)).FontSize(11).FontWeight(600).TextColor(islandBlue).FontFeatures("tnum")
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, option.Label).FontSize(12).FontWeight(500).TextColor(islandWhite.Alpha(0.92)).SingleLine().Shrink(1).MinWidth(0)
			if option.Description != "" {
				ui.Text(c, option.Description).FontSize(11).TextColor(islandWhite.Alpha(0.58)).SingleLine().Shrink(1).MinWidth(0)
			}
		})
	})
	if row.Clicked() {
		answer()
	}
}

// swipeRow is a completed card that swipes right to be marked read, as Cindy's
// CompletedSessionSwipeRow: past 24 points it rests open on its button, past the larger of 140 and
// 72% of its width a release commits, and a commit slides it away and folds its row.
func (i *island) swipeRow(c *ui.Context, s *islandSession) {
	sw := i.swipe[s.Key]
	if sw == nil || sw.generation != s.Generation {
		sw = &islandSwipe{generation: s.Generation}
		i.swipe[s.Key] = sw
	}
	holder := ui.Box(c.Key("island-swipe-" + s.Key)).FillWidth().Radius(12).Clip()
	holder.Transition(ui.ElementTransition{Animation: &ui.Animation{Duration: 100 * time.Millisecond, Ease: ui.EaseOut}, Exit: &ui.Motion{Collapse: true, Opacity: 0}})
	holder.Children(func() {
		width := float32(islandExpandedDefaultWidth - islandCardHorizontalPadding*2)
		armAt := max(140, width*0.72)
		// The button the swipe uncovers, behind the card.
		under := ui.ButtonBase(c.Key("island-markread-" + s.Key)).Absolute().Top(0).Bottom(0).Left(0).Width(max(sw.offset.X, 0)).
			Clip().Justify(ui.Start).AlignItems(ui.Center).Label(L("Mark as Read"))
		alpha := float32(0.8)
		if sw.armed {
			alpha = 1
		}
		under.Background(islandBlue.Alpha(alpha)).Opacity(min(1, sw.offset.X))
		under.Children(func() {
			ui.Column(c).Width(islandSwipeReveal).Gap(5).AlignItems(ui.Center).Shrink(0).Children(func() {
				symbol(c, "checkmark", 18, 2).TextColor(islandWhite.Alpha(0.95))
				ui.Text(c, L("Mark as Read")).FontSize(11).FontWeight(500).TextColor(islandWhite.Alpha(0.95)).SingleLine()
			})
		})
		if under.Clicked() && sw.offset.X > 0 {
			// A click on the uncovered button slides the card the rest of the way, as the release
			// past the arm point does.
			sw.offset = ui.Point{X: width}
			i.commitSwipe(s, sw, width)
		}
		card := i.card(c, s, 0)
		// Cindy decides at the release, from where the card is: past the larger of 140 and 72% of
		// the width it commits, past 24 it rests on its button, else it goes back. MyGo's Snap is
		// given where a throw would carry the card, which a quick flick puts far past the arm
		// point; the decision reads the card's own offset at release instead.
		released := sw.offset.X
		snap := func(p ui.Point) ui.Point {
			switch {
			case sw.committed:
				return ui.Point{X: width}
			case released >= armAt:
				i.commitSwipe(s, sw, width)
				return ui.Point{X: width}
			case released >= 24:
				return ui.Point{X: islandSwipeReveal}
			}
			return ui.Point{}
		}
		card.Draggable(&sw.offset, ui.DraggableOptions{Axis: ui.Horizontal, Bounds: &ui.Rect{W: width}, Rigid: true, Snap: snap, Animation: islandSettleSpring})
		if armed := sw.offset.X >= armAt; armed != sw.armed {
			sw.armed = armed
		}
	})
}

// commitSwipe marks the card read once its slide is done, 0.22 seconds on, as Cindy sends it. The
// slide is the release's own spring to the far edge; a card that could not be marked read, because
// a newer one replaced it, goes back.
func (i *island) commitSwipe(s *islandSession, sw *islandSwipe, width float32) {
	if sw.committed {
		return
	}
	sw.committed, sw.armed = true, true
	key, generation, chatID := s.Key, s.Generation, s.ChatID
	time.AfterFunc(220*time.Millisecond, func() {
		post(func() {
			if i.m.markRead(key, generation, time.Now()) {
				store.MarkRead(chatID)
				delete(i.swipe, key)
			} else {
				// Not marked read: the card comes back, as Cindy's row resets on a failed action.
				sw.committed, sw.armed, sw.offset = false, false, ui.Point{}
			}
			i.sync()
			i.invalidate()
		})
	})
}

// open opens a session's chat from the island: its card closes, a result counts as read.
func (i *island) open(s *islandSession) {
	now := time.Now()
	chatID := s.ChatID
	if s.Ask != nil {
		chatID = s.Ask.ChatID
	}
	i.m.opened(s.Key, now)
	app.openChat(chatID)
	i.sync()
}

// islandOpen opens what waits on the user: the chat of the first question, or Lorca when nothing
// does. The global shortcut calls it.
func islandOpen() {
	if store == nil {
		return
	}
	if app.island != nil {
		for _, s := range app.island.m.ordered(time.Now()) {
			if s.Ask != nil {
				app.island.open(s)
				return
			}
		}
	}
	app.reopen()
}

// islandAuthorName names the bot that wrote a message, for a question's card.
func islandAuthorName(message *model.Message) string {
	if bot := store.Bot(message.Author.BotID); bot != nil {
		return bot.Name
	}
	return L("The bot")
}

// islandQuestionText is a coding agent's question in one line: the command it would run, or what it
// asks.
func islandQuestionText(q *model.AgentQuestion) string {
	if q.Command != "" {
		return "$ " + model.FirstLine(q.Command)
	}
	return q.Text
}

// islandBotName is the bot's name, or "The bot" when the store no longer holds it.
func islandBotName(botID string) string {
	if bot := store.Bot(botID); bot != nil {
		return bot.Name
	}
	return L("The bot")
}
