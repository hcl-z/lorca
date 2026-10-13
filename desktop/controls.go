package main

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The app's controls, after the macOS app's Controls.swift: borderless symbol buttons that fill
// under the pointer, push buttons, switches, pop-up buttons whose menu is the system's, fields,
// the search field, and a button that says it copied.

// Motion the controls share. A fill or border follows the pointer over 120 ms, a color change and
// never a slide; a control the pointer holds sinks a little on a spring that settles without
// bouncing, in frames that only paint; a focus halo fades in and out. While the desktop asks for
// less motion, MyGo keeps the fades and drops the sinking.
var (
	controlFade = ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond}
	pressSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 180 * time.Millisecond}}
	haloFade    = ui.Animation{Duration: 140 * time.Millisecond}
)

// Motion the views share. A side pane opens and closes on a spring that settles without
// bouncing; a tab's selection glides to the tab picked; what a pane shows anew (another chat,
// another tab) fades in from a few DIPs lower; a message that just arrived rises into place; an
// unread dot pops in and out. Reduce Motion keeps the fades and drops the moving.
var (
	paneSpring      = ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond}}
	selectionSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.12}}
	contentEnter    = ui.ElementTransition{Enter: &ui.Motion{Y: 6}, Duration: 180 * time.Millisecond}
	dotPop          = ui.ElementTransition{
		Animation: &ui.Animation{Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.3}},
		Enter:     &ui.Motion{Scale: 0.2}, Exit: &ui.Motion{Scale: 0.2},
	}
	// messageArrival is how long after it was made a message still rises in as it shows.
	messageArrival = 900 * time.Millisecond
)

// selectionThumb is a segmented control's raised selection, under the selected segment's
// content; `match` names the control, so the thumb glides from the segment picked before.
func selectionThumb(c *ui.Context, match any, fill ui.Color, radius float32) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius).Background(fill).
		Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12)).PassThrough().
		Transition(ui.ElementTransition{Animation: &selectionSpring, Match: match})
}

// segmentContent is a segment's words or symbol, centered on a layer of their own: MyGo paints a
// parent's absolute children after the others, in their order, so this layer, built after the
// thumb, draws over it.
func segmentContent(c *ui.Context) ui.Element {
	return ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Justify(ui.Center).AlignItems(ui.Center).PassThrough()
}

// thumbMatch is the identity a segmented control's thumb glides by.
type thumbMatch struct{ label string }

// sinkWhenPressed scales `e` down to `scale` while the pointer holds it, and back up once let go.
func sinkWhenPressed(e ui.Element, pressed bool, scale float32) {
	pose := ui.Pose{}
	if pressed {
		pose.Scale = scale
	}
	e.AnimatePose(pose, pressSpring)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// hoverButtonOptions describe a borderless symbol button.
type hoverButtonOptions struct {
	Symbol string
	// Size is the symbol's, 16 by default.
	Size float32
	// Title makes it the symbol and the word, as wide as they need; Trailing is a symbol after a
	// word that leads somewhere.
	Title    string
	Trailing string
	Tooltip  string
	// Label names it for assistive technology, else its tooltip or title.
	Label    string
	Disabled bool
	// Active keeps its fill, as while what it opened is up.
	Active bool
}

// hoverButton is a borderless symbol button that fills a rounded rect under the pointer.
func hoverButton(c *ui.Context, o hoverButtonOptions) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Height(controlRegular).MinWidth(controlRegular).Radius(radiusMD).Gap(5).Justify(ui.Center).TextColor(p.Label2)
	if o.Title != "" {
		b.Padding(0, 8).FontSize(13)
	}
	if label := firstNonEmpty(o.Label, o.Tooltip, o.Title); label != "" {
		b.Label(label)
	}
	if o.Tooltip != "" {
		b.Tooltip(o.Tooltip)
	}
	pressed := !o.Disabled && b.Pressed()
	switch {
	case o.Disabled:
		b.Disabled(true)
	case pressed:
		b.Background(p.Pressed).TextColor(p.Label)
	case b.Hovered() || o.Active:
		b.Background(p.Hover).TextColor(p.Label)
	}
	b.Transition(controlFade)
	sinkWhenPressed(b, pressed, 0.92)
	size := o.Size
	if size == 0 {
		size = 16
	}
	b.Children(func() {
		if o.Symbol != "" {
			symbol(c, o.Symbol, size, 1.75)
		}
		if o.Title != "" {
			ui.Text(c, o.Title).SingleLine()
		}
		if o.Trailing != "" {
			symbol(c, o.Trailing, 10, 2.5)
		}
	})
	return b
}

type buttonKind int

const (
	buttonDefault buttonKind = iota
	buttonPrimary
	buttonDestructive
)

// pushOptions describe a push button.
type pushOptions struct {
	Kind     buttonKind
	Small    bool
	Large    bool
	Disabled bool
	Tooltip  string
	// Symbol leads the title.
	Symbol string
}

// pushButton is a push button: a rounded rect, 28 tall. The default button is filled with ink and
// has no border; a plain one is the button fill inside a hairline.
func pushButton(c *ui.Context, title string, o pushOptions) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Height(controlRegular).Padding(0, space3).Radius(radiusMD).Gap(5).Justify(ui.Center).FontSize(13).FontWeight(500).TextColor(p.Label).Label(title)
	switch {
	case o.Small:
		b.Height(controlSmall).Padding(0, 10).FontSize(12).Radius(radiusSM + 1)
	case o.Large:
		b.Height(controlLarge).Padding(0, 18).Radius(radiusLG)
	}
	primary := o.Kind == buttonPrimary && !o.Disabled
	fill, hover, pressed := p.ButtonBG, p.ButtonHover, p.ButtonPressed
	// A disabled default button is a plain one, as on macOS; MyGo dims what is disabled.
	if primary {
		fill, hover, pressed = p.Ink, p.inkHover(), p.inkPressed()
		b.TextColor(p.InkText)
	} else {
		b.Border(1, p.ButtonBorder)
		if o.Kind == buttonDestructive {
			b.TextColor(p.Red)
		}
	}
	held := !o.Disabled && b.Pressed()
	switch {
	case o.Disabled:
		b.Disabled(true)
	case held:
		fill = pressed
	case b.Hovered():
		fill = hover
	}
	b.Background(fill).Transition(controlFade)
	sinkWhenPressed(b, held, 0.97)
	if o.Tooltip != "" {
		b.Tooltip(o.Tooltip)
	}
	b.Children(func() {
		if o.Symbol != "" {
			symbol(c, o.Symbol, 13, 2)
		}
		ui.Text(c, title).SingleLine()
	})
	return b
}

// actionLook is how a row's action shows: its symbol, and whether the symbol stands alone, its
// words then its tooltip and its name for assistive technology.
type actionLook struct {
	symbol   string
	iconOnly bool
}

// actionLookFor is the look of an action by its words, which come through L, so the table holds in
// every language. An action the table leaves out is its words alone.
func actionLookFor(title string) actionLook {
	switch title {
	// What says itself in a symbol stands alone.
	case L("Edit…"):
		return actionLook{"pencil", true}
	case L("Copy"):
		return actionLook{"doc.on.doc", true}
	case L("Copied"):
		// The green check says it, at the size the Copy it stands in for had.
		return actionLook{"checkmark", true}
	case L("Show"):
		return actionLook{"folder", true}
	case L("Open"):
		return actionLook{"arrow.up.right", true}
	case L("Reload"), L("Retry"), L("Try Again"):
		return actionLook{"arrow.triangle.2.circlepath", true}
	case L("Reset"):
		return actionLook{"arrow.uturn.backward", true}
	// The rest keep their words beside the symbol.
	case L("Connect"), L("Connect…"), L("Sign in"), L("Sign in again"):
		return actionLook{"link", false}
	case L("Change"):
		return actionLook{"square.and.pencil", false}
	case L("Compact"):
		return actionLook{"arrow.down.to.line", false}
	case L("Add…"), L("Add from Plugins…"), L("Add Server…"), L("Add Provider…"), L("New Bot…"):
		return actionLook{"plus", false}
	case L("Check for Updates…"), L("Update"), L("Regenerate…"):
		return actionLook{"arrow.triangle.2.circlepath", false}
	case L("Install App…"):
		return actionLook{"arrow.down.to.line", false}
	case L("Set Up…"):
		return actionLook{"gearshape", false}
	case L("Allow"):
		return actionLook{"checkmark", false}
	case L("Ignore"), L("Skip"):
		return actionLook{"xmark", false}
	case L("Sign Out"), L("Disconnect"), L("Unpair…"):
		return actionLook{"minus.circle", false}
	case L("Delete Account…"):
		return actionLook{"trash", false}
	case L("Show Onboarding Again"):
		return actionLook{"sparkles", false}
	}
	if strings.HasPrefix(title, L("Copy")) {
		return actionLook{"doc.on.doc", false}
	}
	return actionLook{}
}

// linkButton is a row's action, as Edit… and Change are: a symbol, or a symbol and its words, on
// a small chip.
func linkButton(c *ui.Context, title string, disabled bool) ui.Element {
	b := actionButton(c, title, false)
	if disabled {
		b.Disabled(true)
	}
	return b
}

// actionButton is a row's action on a small chip in the chip fill, so it reads apart from the
// values beside it, a shade deeper under the pointer. Once `copied`, a green check stands for its
// symbol.
func actionButton(c *ui.Context, title string, copied bool) ui.Element {
	p := colors(c)
	look := actionLookFor(title)
	b := ui.ButtonBase(c).Height(24).Radius(radiusSM).Gap(5).Shrink(0).Justify(ui.Center).FontSize(12).FontWeight(500).
		TextColor(p.Label).Background(p.Chip).Label(title).Cursor(ui.CursorPointer)
	if look.iconOnly {
		b.Width(24).Tooltip(title)
	} else if look.symbol != "" {
		b.Padding(0, space2+1, 0, space2)
	} else {
		b.Padding(0, space2+1)
	}
	held := b.Pressed()
	if held {
		b.Background(p.Pressed.Over(p.Chip))
	} else if b.Hovered() {
		b.Background(p.Hover.Over(p.Chip))
	}
	b.Transition(controlFade)
	sinkWhenPressed(b, held, 0.94)
	if copied {
		b.TextColor(p.Green)
	}
	b.Children(func() {
		switch {
		case copied:
			symbol(c, "checkmark", 12, 2.4).ContentTransition(ui.ContentTransition{})
		case look.symbol != "":
			symbol(c, look.symbol, 13, 2).ContentTransition(ui.ContentTransition{})
		}
		if !look.iconOnly {
			ui.Text(c, title).SingleLine()
		}
	})
	return b
}

// toggleSwitch is a switch: an ink track while on, and a knob in the page's color that slides.
func toggleSwitch(c *ui.Context, on *bool, small bool) ui.Element {
	p := colors(c)
	w, h, knob := float32(34), float32(20), float32(16)
	if small {
		w, h, knob = 26, 15, 11
	}
	s := ui.SwitchBase(c, on).Size(w, h).Radius(h / 2)
	held := s.Pressed()
	track, knobFill := p.SwitchOff, p.White
	if *on {
		track, knobFill = p.Ink, p.InkText
		if s.Hovered() && !held {
			track = p.inkHover()
		}
	}
	s.Background(track).Transition(ui.ElementTransition{Colors: true, Duration: 150 * time.Millisecond})
	// The knob stretches toward the middle while held, as the system's switches do, and slides
	// on a spring with a trace of bounce.
	stretch := float32(0)
	if held {
		stretch = knob / 4
	}
	stretch = s.AnimateTo("stretch", stretch, pressSpring)
	left := float32(2)
	if *on {
		left = w - knob - 2 - stretch
	}
	x := s.AnimateTo("knob", left, ui.Animation{Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.12}})
	s.Children(func() {
		ui.Box(c).Absolute().Top((h-knob)/2).Left(x).Size(knob+stretch, knob).Radius(knob/2).Background(knobFill).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.25))
	})
	return s
}

type popUpStyle int

const (
	// popUpBordered is a form control's: a push button's flat fill with the chevrons at its end.
	popUpBordered popUpStyle = iota
	// popUpSettings is System Settings' look: the text, then chevrons on a small platter.
	popUpSettings
	// popUpPlain is text with chevrons, for a toolbar.
	popUpPlain
)

type popUpOption struct {
	Value string
	Label string
	// Symbol shows before the picked option's label, as a Device's.
	Symbol string
	// Separated puts a separator before the option.
	Separated bool
}

// popUpExtra is an item after the choices, such as Pair a Device….
type popUpExtra struct {
	ID    string
	Label string
}

type popUp struct {
	Options  []popUpOption
	Value    string
	Style    popUpStyle
	Disabled bool
	Tooltip  string
	Label    string
	Extras   []popUpExtra
	// Width fixes the button's width; 0 fits its choice.
	Width float32
	// MaxWidth caps a button that fits its choice; a longer choice is cut short.
	MaxWidth float32
}

// popUpButton shows the choice as text; its menu is the system's, with a check on the choice. It
// answers the option picked this frame when it differs from the choice, and the extra picked.
func popUpButton(c *ui.Context, o popUp) (picked string, changed bool, extra string) {
	p := colors(c)
	current := popUpOption{}
	for _, option := range o.Options {
		if option.Value == o.Value {
			current = option
		}
	}
	b := ui.ButtonBase(c).Gap(8).TextColor(p.Label)
	if o.Label != "" {
		b.Label(o.Label)
	}
	if o.Tooltip != "" {
		b.Tooltip(o.Tooltip)
	}
	if o.Width > 0 {
		b.Width(o.Width)
	}
	if o.MaxWidth > 0 {
		b.MaxWidth(o.MaxWidth).MinWidth(0).Shrink(1)
	}
	hovered, pressed := b.Hovered() && !o.Disabled, b.Pressed() && !o.Disabled
	chevrons := float32(12)
	switch o.Style {
	case popUpSettings:
		b.Height(controlSmall).Padding(0, 4, 0, 9).Radius(radiusMD).FontSize(13)
		if hovered {
			b.Background(p.Hover)
		}
		chevrons = 11
	case popUpBordered:
		b.Height(controlRegular).Padding(0, 6, 0, 10).Radius(radiusMD).FontSize(12.5).Justify(ui.SpaceBetween).Border(1, p.ButtonBorder)
		fill := p.ButtonBG
		if pressed {
			fill = p.ButtonPressed
		} else if hovered {
			fill = p.ButtonHover
		}
		b.Background(fill)
	case popUpPlain:
		b.Height(controlRegular).Padding(0, 8, 0, 10).Radius(radiusCapsule).FontSize(13)
		if hovered {
			b.Background(p.Hover)
		}
	}
	if o.Disabled {
		b.Disabled(true)
	}
	b.Transition(controlFade)
	b.Children(func() {
		if current.Symbol != "" {
			symbol(c, current.Symbol, 13, 1.9).TextColor(p.Label2).Margin(0, -3, 0, 0)
		}
		title := ui.Text(c, current.Label).SingleLine()
		if o.Width > 0 {
			title.Grow(1).Shrink(1)
		} else if o.MaxWidth > 0 {
			title.Shrink(1).MinWidth(0)
		}
		well := ui.Row(c).Center()
		switch o.Style {
		case popUpSettings:
			well.Size(19, 19).Radius(radiusSM)
			if pressed {
				well.Background(p.Pressed)
			} else if !hovered {
				well.Background(p.Hover)
			}
			well.Transition(controlFade)
		case popUpBordered:
			well.TextColor(p.Label2)
		}
		well.Children(func() { symbol(c, "chevron.up.chevron.down", chevrons, 2.4) })
	})
	b.Menu(func(m *ui.Menu) {
		for i, option := range o.Options {
			if option.Separated && i > 0 {
				m.Separator()
			}
			if m.Item(option.Label).Checked(option.Value == o.Value).Chosen() && option.Value != o.Value {
				picked, changed = option.Value, true
			}
		}
		if len(o.Extras) > 0 {
			m.Separator()
			for _, item := range o.Extras {
				if m.Item(item.Label).Chosen() {
					extra = item.ID
				}
			}
		}
	})
	return picked, changed, extra
}

// fieldOptions describe a text field.
type fieldOptions struct {
	Placeholder string
	Secure      bool
	Mono        bool
	Disabled    bool
	ReadOnly    bool
	AutoFocus   bool
	// Plain has no border or fill, as a field inside a row.
	Plain bool
	Label string
}

// fieldChrome draws a field's border and fill, and its focus as the border in the label color with
// a soft halo of it outside, both fading in as the field takes the keyboard and out as it lets go.
func fieldChrome(c *ui.Context, e ui.Element, focused, plain bool) {
	p := colors(c)
	e.FocusRing(false)
	if plain {
		return
	}
	e.Radius(radiusLG).Background(p.Field)
	border, halo := p.FieldBorder, ui.Color{}
	if focused {
		border, halo = p.Label3, p.Label4
	}
	e.Border(1, border).Shadow(0, 0, 0, 3, e.AnimateColor("halo", halo, haloFade)).Transition(controlFade)
}

// textField is a one-line field; its Submitted is Return.
func textField(c *ui.Context, value *string, o fieldOptions) ui.Element {
	p := colors(c)
	e := ui.TextInputBase(c, value).MinHeight(fieldHeight).Padding(5, 10).FontSize(13).TextColor(p.Label)
	if o.Mono {
		e.Font(monoFont).FontSize(12)
	}
	if o.Secure {
		e.Password()
	}
	if o.Placeholder != "" {
		e.Placeholder(o.Placeholder)
	}
	if o.Label != "" {
		e.Label(o.Label)
	}
	if o.AutoFocus {
		e.AutoFocus()
	}
	if o.ReadOnly {
		e.ReadOnly(true)
	}
	if o.Disabled {
		e.Disabled(true)
	}
	fieldChrome(c, e, e.Focused() && !o.Disabled, o.Plain)
	if e.Submitted() {
		fieldSubmitted = true
	}
	return e
}

// textArea is a field of several lines, `lines` high, which scrolls past them.
func textArea(c *ui.Context, value *string, lines int, o fieldOptions) ui.Element {
	p := colors(c)
	e := ui.TextAreaBase(c, value).Padding(8, 10).FontSize(13).LineHeight(1.4).TextColor(p.Label)
	size := float32(13)
	if o.Mono {
		e.Font(monoFont).FontSize(12)
		size = 12
	}
	if lines > 0 {
		e.Height(float32(lines)*size*1.4 + 16)
	}
	if o.Placeholder != "" {
		e.Placeholder(o.Placeholder)
	}
	if o.Label != "" {
		e.Label(o.Label)
	}
	if o.AutoFocus {
		e.AutoFocus()
	}
	if o.ReadOnly {
		e.ReadOnly(true)
	}
	if o.Disabled {
		e.Disabled(true)
	}
	fieldChrome(c, e, e.Focused() && !o.Disabled, o.Plain)
	return e
}

// searchField is a rounded search field with a magnifier and a clear button; Escape clears it
// while it has text. Its Changed and Submitted are the input's.
func searchField(c *ui.Context, value *string, placeholder string) ui.Element {
	p := colors(c)
	if placeholder == "" {
		placeholder = L("Search")
	}
	var input ui.Element
	box := searchChrome(c, ui.Row(c))
	box.Children(func() {
		symbol(c, "magnifyingglass", 13, 2)
		input = ui.TextInputBase(c, value).Grow(1).Shrink(1).MinWidth(0).FontSize(13).TextColor(p.Label).Placeholder(placeholder).Label(placeholder).FocusRing(false)
		if *value != "" {
			if input.Shortcut(0, ui.KeyEscape) {
				*value = ""
			}
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Background(p.Label3).TextColor(p.Content).Label(L("Clear")).FocusRing(false)
			if clear.Hovered() {
				clear.Background(p.Label2)
			}
			clear.Transition(controlFade)
			clear.Children(func() { symbol(c, "xmark", 9, 3) })
			if clear.Clicked() {
				*value = ""
			}
		}
	})
	border, halo := p.ButtonBorder, ui.Color{}
	if input.Focused() {
		border, halo = p.Label3, p.Label4
	}
	box.Border(1, border).Shadow(0, 0, 0, 3, box.AnimateColor("halo", halo, haloFade)).Transition(controlFade)
	if box.Clicked() {
		input.Focus()
	}
	return input
}

// searchChrome is the search field's box: the sidebar's way into the palette draws it too.
func searchChrome(c *ui.Context, e ui.Element) ui.Element {
	p := colors(c)
	return e.Height(fieldHeight).Gap(6).Padding(0, 8, 0, 10).Radius(radiusLG).Background(p.SearchBG).Border(1, p.ButtonBorder).TextColor(p.Label2).Cursor(ui.CursorText)
}

// spinner is the system's spinner, `size` across.
func spinner(c *ui.Context, size float32) ui.Element {
	return ui.Spinner(c).Size(size, size)
}

type copyOptions struct {
	Title    string
	Symbol   string
	Tooltip  string
	Bordered bool
}

// copyButton copies `text` and says so for a moment: its symbol turns into a green check, its
// title into Copied. It reports the click.
func copyButton(c *ui.Context, text string, o copyOptions) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Height(controlSmall).MinWidth(controlSmall).Gap(5).Justify(ui.Center).Radius(radiusSM)
	copiedAt := ui.Local(b, "copied", func() time.Time { return time.Time{} })
	if b.Clicked() {
		mygo.Clipboard.WriteText(text)
		*copiedAt = c.Now()
	}
	copied := !copiedAt.IsZero() && c.Now().Sub(*copiedAt) < 1500*time.Millisecond
	if copied {
		c.After(1500*time.Millisecond - c.Now().Sub(*copiedAt))
	}
	held := b.Pressed()
	if o.Bordered {
		b.Height(controlRegular).Padding(0, space3).Radius(radiusMD).FontSize(13).TextColor(p.Label).Border(1, p.ButtonBorder)
		fill := p.ButtonBG
		if held {
			fill = p.ButtonPressed
		} else if b.Hovered() {
			fill = p.ButtonHover
		}
		b.Background(fill)
		sinkWhenPressed(b, held, 0.97)
	} else {
		b.Padding(0, 4).TextColor(p.Label2)
		if held {
			b.Background(p.Pressed)
		} else if b.Hovered() {
			b.Background(p.Hover)
		}
		sinkWhenPressed(b, held, 0.92)
	}
	b.Transition(controlFade)
	if copied {
		b.TextColor(p.Green)
	}
	b.Label(firstNonEmpty(o.Tooltip, o.Title, L("Copy")))
	if o.Tooltip != "" && !copied {
		b.Tooltip(o.Tooltip)
	}
	b.Children(func() {
		if copied {
			symbol(c, "checkmark", 13, 2).ContentTransition(ui.ContentTransition{})
		} else if o.Symbol != "" {
			symbol(c, o.Symbol, 13, 2).ContentTransition(ui.ContentTransition{})
		}
		if o.Title != "" {
			title := o.Title
			if copied {
				title = L("Copied")
			}
			ui.Text(c, title).SingleLine()
		}
	})
	return b
}

// segmented is a segmented control: one choice of a few, each a segment as wide as its label.
func segmented(c *ui.Context, selected *int, label string, labels ...string) ui.Element {
	p := colors(c)
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Padding(2).Radius(radiusMD).Background(p.Chip).Label(label)
	parts.Track.Children(func() {
		for i, text := range labels {
			// Each segment is as wide as its words in their selected weight, so picking one moves
			// none; the words sit on a layer over the thumb, so the segment takes its width here.
			w, _ := c.MeasureText(0, ui.Span{Text: text, Size: 12, Weight: 500})
			seg := parts.Segment(i).Width(float32(math.Ceil(float64(w))) + 21).Height(22).Radius(radiusSM).FontSize(12).TextColor(p.Label2).Shrink(0)
			on := i == *selected
			switch {
			case on:
				seg.TextColor(p.Label).FontWeight(500)
			case seg.Hovered():
				seg.TextColor(p.Label).Background(p.Hover)
			}
			seg.Transition(controlFade)
			seg.Children(func() {
				if on {
					selectionThumb(c, thumbMatch{label}, p.ButtonBG, radiusSM)
				}
				segmentContent(c).Children(func() { ui.Text(c, text).SingleLine() })
			})
		}
	})
	return parts.Track
}

// roundButton is a symbol on a raised disc inside a hairline, as the sidebar's Search and Create.
func roundButton(c *ui.Context, name, tooltip, label string) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Size(30, 30).Radius(15).Justify(ui.Center).TextColor(p.Label).Background(p.ButtonBG).
		Border(1, p.ButtonBorder).Shadow(0, 1, 2, 0, p.Shadow.Alpha(0.06)).Label(label).Tooltip(tooltip)
	pressed := b.Pressed()
	switch {
	case pressed:
		b.Background(p.ButtonPressed)
	case b.Hovered():
		b.Background(p.ButtonHover)
	}
	b.Transition(controlFade)
	sinkWhenPressed(b, pressed, 0.92)
	b.Children(func() { symbol(c, name, 15, 1.9) })
	return b
}

// iconTab is a tab of iconTabs: its symbol, and its title as tooltip and label.
type iconTab struct {
	Symbol string
	Title  string
}

// iconTabs is a row of tabs that are symbols alone, sharing the width: a segmented control whose
// selection is a raised capsule on a neutral track.
func iconTabs(c *ui.Context, selected *int, label string, tabs []iconTab) ui.Element {
	p := colors(c)
	parts := ui.SegmentedBase(c, selected, len(tabs))
	parts.Track.Height(34).Padding(3).Gap(2).Radius(radiusCapsule).Background(p.Chip).Label(label)
	parts.Track.Children(func() {
		for i, tab := range tabs {
			seg := parts.Segment(i).Grow(1).Basis(0).Height(28).Radius(radiusCapsule).TextColor(p.Label2).Justify(ui.Center).Label(tab.Title).Tooltip(tab.Title)
			on := i == *selected
			switch {
			case on:
				seg.TextColor(p.Label)
			case seg.Hovered():
				seg.TextColor(p.Label).Background(p.Hover)
			}
			seg.Transition(controlFade)
			seg.Children(func() {
				if on {
					selectionThumb(c, thumbMatch{label}, p.ButtonBG, radiusCapsule)
				}
				segmentContent(c).Children(func() { symbol(c, tab.Symbol, 15, 1.9) })
			})
		}
	})
	return parts.Track
}

// caption is a small line of secondary text.
func caption(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(textCaption).TextColor(colors(c).Label2)
}
