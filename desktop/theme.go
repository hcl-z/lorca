package main

import (
	"math"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// The app's design tokens: colors, type, radii, control heights, and spacing, light and dark. The
// look is monochrome: neutral surfaces, ink (black in the light appearance, white in the dark) for
// the primary button, the user's bubbles, and a switch that is on, and a neutral fill for the
// selected row. The accent colors focus, links, and mentions; bots keep their system colors.

type palette struct {
	Label, Label2, Label3, Label4 ui.Color
	Separator, Placeholder, Link  ui.Color

	Accent, AccentText ui.Color
	// Ink fills the primary button and a switch that is on; InkText is drawn on it. Blue fills the user's bubble, with AccentText on it.
	Ink, InkText ui.Color
	// Unread is the dot and count of a chat with unread messages.
	Unread ui.Color
	// WarningBG and WarningText are a badge that waits for the user, such as Approval needed.
	WarningBG, WarningText ui.Color
	// ButtonBorder outlines a plain push button and a pop-up button.
	ButtonBorder ui.Color
	// ButtonHover and ButtonPressed are a plain push button's fill under the pointer and pressed.
	ButtonHover, ButtonPressed ui.Color
	// SwitchOff is a switch's track while off.
	SwitchOff ui.Color
	// Scrollbar is a scroll container's thumb.
	Scrollbar ui.Color

	Indigo, Blue, Teal, Green, Orange, Pink, Purple, Red ui.Color

	Window, Sidebar, Inspector, Content, ToolbarLine ui.Color

	BotBubble, BotBubbleBorder, Code, CodeHover, Chip, Card, CardBorder ui.Color
	// OutputCard fills a published output's card in the transcript, apart from the bubbles.
	OutputCard ui.Color

	ComposerField, ComposerBorder, ComposerControl, ComposerPrimary, ComposerPrimaryContent ui.Color
	// GlassButton fills a round glass button over its frosted backdrop, as AppKit's glass bezel.
	GlassButton ui.Color

	Hover, Pressed, ButtonBG                      ui.Color
	Selection, SelectionText, SelectionInactive   ui.Color
	Field, FieldBorder, Popover, Scrim, Sheet     ui.Color
	SearchBG, RowHover, Shadow, ShadowEdge, White ui.Color
}

var lightPalette = palette{
	Label:       ui.Hex("#0a0a0a"),
	Label2:      ui.RGBA(10, 10, 10, 0.56),
	Label3:      ui.RGBA(10, 10, 10, 0.34),
	Label4:      ui.RGBA(10, 10, 10, 0.12),
	Separator:   ui.RGBA(0, 0, 0, 0.08),
	Placeholder: ui.RGBA(10, 10, 10, 0.36),
	Link:        ui.Hex("#0068da"),

	Accent:     ui.Hex("#007aff"),
	AccentText: ui.Hex("#ffffff"),

	Ink:           ui.Hex("#0a0a0a"),
	InkText:       ui.Hex("#ffffff"),
	Unread:        ui.Hex("#0a84ff"),
	WarningBG:     ui.RGBA(255, 149, 0, 0.14),
	WarningText:   ui.Hex("#c76a00"),
	ButtonBorder:  ui.RGBA(0, 0, 0, 0.1),
	ButtonHover:   ui.Hex("#ededed"),
	ButtonPressed: ui.Hex("#e2e2e2"),
	SwitchOff:     ui.RGBA(0, 0, 0, 0.14),
	Scrollbar:     ui.RGBA(0, 0, 0, 0.16),

	Indigo: ui.Hex("#5856d6"),
	Blue:   ui.Hex("#007aff"),
	Teal:   ui.Hex("#30b0c7"),
	Green:  ui.Hex("#34c759"),
	Orange: ui.Hex("#ff9500"),
	Pink:   ui.Hex("#ff2d55"),
	Purple: ui.Hex("#af52de"),
	Red:    ui.Hex("#ff3b30"),

	Window:      ui.Hex("#f4f4f4"),
	Sidebar:     ui.Hex("#f7f7f7"),
	Inspector:   ui.Hex("#fbfbfb"),
	Content:     ui.Hex("#ffffff"),
	ToolbarLine: ui.RGBA(0, 0, 0, 0.07),

	BotBubble:       ui.Hex("#f2f2f2"),
	BotBubbleBorder: ui.RGBA(0, 0, 0, 0),
	Code:            ui.RGBA(0, 0, 0, 0.05),
	CodeHover:       ui.RGBA(0, 0, 0, 0.09),
	Chip:            ui.RGBA(0, 0, 0, 0.045),
	Card:            ui.Hex("#ffffff"),
	CardBorder:      ui.RGBA(0, 0, 0, 0.09),
	OutputCard:      ui.Hex("#ffffff"),

	ComposerField:          ui.RGBA(255, 255, 255, 0.92),
	ComposerBorder:         ui.RGBA(0, 0, 0, 0.1),
	ComposerControl:        ui.RGBA(0, 0, 0, 0.06),
	ComposerPrimary:        ui.Hex("#0a0a0a"),
	ComposerPrimaryContent: ui.Hex("#ffffff"),
	GlassButton:            ui.RGBA(255, 255, 255, 0.92),

	Hover:             ui.RGBA(0, 0, 0, 0.05),
	Pressed:           ui.RGBA(0, 0, 0, 0.1),
	ButtonBG:          ui.Hex("#ffffff"),
	Selection:         ui.Hex("#e9e9e9"),
	SelectionText:     ui.Hex("#0a0a0a"),
	SelectionInactive: ui.Hex("#eeeeee"),
	Field:             ui.Hex("#ffffff"),
	FieldBorder:       ui.RGBA(0, 0, 0, 0.12),
	Popover:           ui.Hex("#ffffff"),
	Scrim:             ui.RGBA(0, 0, 0, 0.2),
	Sheet:             ui.Hex("#ffffff"),
	SearchBG:          ui.Hex("#ececec"),
	RowHover:          ui.RGBA(0, 0, 0, 0.035),
	Shadow:            ui.RGBA(0, 0, 0, 0.14),
	ShadowEdge:        ui.RGBA(0, 0, 0, 0.1),
	White:             ui.Hex("#ffffff"),
}

var darkPalette = palette{
	Label:       ui.Hex("#f2f2f2"),
	Label2:      ui.RGBA(242, 242, 242, 0.58),
	Label3:      ui.RGBA(242, 242, 242, 0.34),
	Label4:      ui.RGBA(242, 242, 242, 0.12),
	Separator:   ui.RGBA(255, 255, 255, 0.09),
	Placeholder: ui.RGBA(242, 242, 242, 0.36),
	Link:        ui.Hex("#4ea1ff"),

	Accent:     ui.Hex("#0a84ff"),
	AccentText: ui.Hex("#ffffff"),

	Ink:           ui.Hex("#f5f5f5"),
	InkText:       ui.Hex("#0a0a0a"),
	Unread:        ui.Hex("#0a84ff"),
	WarningBG:     ui.RGBA(255, 159, 10, 0.16),
	WarningText:   ui.Hex("#ffb340"),
	ButtonBorder:  ui.RGBA(255, 255, 255, 0.1),
	ButtonHover:   ui.Hex("#2a2a2c"),
	ButtonPressed: ui.Hex("#333336"),
	SwitchOff:     ui.RGBA(255, 255, 255, 0.16),
	Scrollbar:     ui.RGBA(255, 255, 255, 0.2),

	Indigo: ui.Hex("#5e5ce6"),
	Blue:   ui.Hex("#0a84ff"),
	Teal:   ui.Hex("#40cbe0"),
	Green:  ui.Hex("#30d158"),
	Orange: ui.Hex("#ff9f0a"),
	Pink:   ui.Hex("#ff375f"),
	Purple: ui.Hex("#bf5af2"),
	Red:    ui.Hex("#ff453a"),

	Window:      ui.Hex("#0e0e0f"),
	Sidebar:     ui.Hex("#141415"),
	Inspector:   ui.Hex("#0c0c0d"),
	Content:     ui.Hex("#0a0a0a"),
	ToolbarLine: ui.RGBA(255, 255, 255, 0.07),

	BotBubble:       ui.Hex("#1c1c1e"),
	BotBubbleBorder: ui.RGBA(255, 255, 255, 0),
	Code:            ui.RGBA(255, 255, 255, 0.06),
	CodeHover:       ui.RGBA(255, 255, 255, 0.1),
	Chip:            ui.RGBA(255, 255, 255, 0.07),
	Card:            ui.Hex("#161618"),
	CardBorder:      ui.RGBA(255, 255, 255, 0.1),
	OutputCard:      ui.Hex("#161618"),

	ComposerField:          ui.RGBA(24, 24, 26, 0.92),
	ComposerBorder:         ui.RGBA(255, 255, 255, 0.12),
	ComposerControl:        ui.RGBA(255, 255, 255, 0.1),
	ComposerPrimary:        ui.Hex("#f5f5f5"),
	ComposerPrimaryContent: ui.Hex("#0a0a0a"),
	GlassButton:            ui.RGBA(36, 36, 38, 0.92),

	Hover:             ui.RGBA(255, 255, 255, 0.06),
	Pressed:           ui.RGBA(255, 255, 255, 0.11),
	ButtonBG:          ui.Hex("#1f1f21"),
	Selection:         ui.Hex("#2a2a2c"),
	SelectionText:     ui.Hex("#f2f2f2"),
	SelectionInactive: ui.Hex("#202022"),
	Field:             ui.Hex("#121213"),
	FieldBorder:       ui.RGBA(255, 255, 255, 0.13),
	Popover:           ui.Hex("#1c1c1e"),
	Scrim:             ui.RGBA(0, 0, 0, 0.5),
	Sheet:             ui.Hex("#141415"),
	SearchBG:          ui.Hex("#1c1c1e"),
	RowHover:          ui.RGBA(255, 255, 255, 0.04),
	Shadow:            ui.RGBA(0, 0, 0, 0.6),
	ShadowEdge:        ui.RGBA(255, 255, 255, 0.1),
	White:             ui.Hex("#ffffff"),
}

// Text sizes, in DIPs.
const (
	textMessage = 15
	textBody    = 13
	textSmall   = 12
	textCaption = 10.5
	textNotice  = 11.5
)

// monoFont is the system's monospaced family.
const monoFont = "monospace"

// Corner radii, in DIPs, from a small platter to a sheet.
const (
	radiusXS      = 4
	radiusSM      = 6
	radiusMD      = 8
	radiusLG      = 10
	radiusCard    = 12
	radiusXL      = 14
	radiusSheet   = 20
	radiusCapsule = 999
)

// Control heights, in DIPs.
const (
	controlSmall   = 24
	controlRegular = 28
	controlLarge   = 34
	fieldHeight    = 30
)

// Spacing steps, in DIPs.
const (
	space1 = 4
	space2 = 8
	space3 = 12
	space4 = 16
	space5 = 20
)

// colors is the palette of the appearance the view draws in, in the desktop's accent color when
// it has one, as the Mac app follows the system's.
func colors(c *ui.Context) *palette {
	base := &lightPalette
	if c.Theme().Dark {
		base = &darkPalette
	}
	accent := c.Preferences().Accent
	if accent.A == 0 {
		return base
	}
	key := accentKey{dark: base == &darkPalette, r: accent.R, g: accent.G, b: accent.B}
	if p, ok := accentPalettes[key]; ok {
		return p
	}
	p := *base
	// The accent colors focus, links, and mentions; the selected row and the ink stay neutral.
	p.Accent = ui.RGB(accent.R, accent.G, accent.B)
	if luminance(p.Accent) > 0.6 {
		p.AccentText = ui.Hex("#000000")
	}
	accentPalettes[key] = &p
	return &p
}

type accentKey struct {
	dark    bool
	r, g, b uint8
}

// accentPalettes are the palettes made for the desktop's accents, on the main thread.
var accentPalettes = map[accentKey]*palette{}

// luminance is a color's relative luminance, 0 for black and 1 for white.
func luminance(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// accentColor is a bot's accent.
func (p *palette) accentColor(a model.Accent) ui.Color {
	switch a {
	case "blue":
		return p.Blue
	case "teal":
		return p.Teal
	case "green":
		return p.Green
	case "orange":
		return p.Orange
	case "pink":
		return p.Pink
	case "purple":
		return p.Purple
	case "red":
		return p.Red
	}
	return p.Indigo
}

// tone is the color of a state's words.
func (p *palette) tone(t model.Tone) ui.Color {
	switch t {
	case model.ToneTertiary:
		return p.Label3
	case model.ToneGreen:
		return p.Green
	case model.ToneAccent:
		return p.Accent
	case model.ToneRed:
		return p.Red
	case model.ToneOrange:
		return p.Orange
	}
	return p.Label2
}

// applyTheme gives the window's widgets the app's colors and type, over the theme that follows the
// desktop: its appearance, accent, contrast, and text size.
func applyTheme(c *ui.Context) {
	base := c.Theme()
	shownDark = base.Dark
	p := colors(c)
	t := *base
	t.Background = p.Content
	t.Text = p.Label
	t.TextMuted = p.Label2
	t.Border = p.FieldBorder
	t.Surface = p.ButtonBG
	t.SurfaceHover = p.ButtonHover
	t.SurfacePressed = p.ButtonPressed
	t.Accent = p.Ink
	t.AccentHover = p.inkHover()
	t.AccentPressed = p.inkPressed()
	t.AccentText = p.InkText
	t.Danger = p.Red
	t.Warning = p.Orange
	t.Success = p.Green
	t.Selection = p.Accent.Alpha(0.32)
	t.Focus = p.Accent.Alpha(0.7)
	// A thin, faint thumb that shows under the pointer; High Contrast keeps the stronger one.
	t.Scrollbar, t.ScrollbarWidth = p.Scrollbar, 4
	if c.Preferences().HighContrast {
		t.Scrollbar, t.ScrollbarWidth = p.Label3, 6
	}
	t.Radius = radiusMD
	t.FontSize = textBody * scaleOf(c)
	c.SetTheme(&t)
}

// shownDark is whether the windows last drew in the dark appearance, on the main thread; the
// appearance command reads it for its title and where it goes.
var shownDark bool

// inkHover and inkPressed are the ink under the pointer and pressed: toward the page's color.
func (p *palette) inkHover() ui.Color   { return p.Ink.Mix(p.InkText, 0.12) }
func (p *palette) inkPressed() ui.Color { return p.Ink.Mix(p.InkText, 0.22) }

// scaleOf is the desktop's text size, which the default theme's font size carries.
func scaleOf(c *ui.Context) float32 {
	if s := c.Preferences().TextScale; s > 0 {
		return s
	}
	return 1
}
