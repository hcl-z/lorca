package main

import (
	"fmt"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The island gallery: a window of Debug that shows every look of the island, picked from a list,
// on a display with a notch or one without, and live: the same views as the island's windows, so
// the springs between scenes, the shimmer, the breathing marks, hover, and the swipe all move as
// they do on the menu bar. The scenes are made up, not the store's; clicks that would answer a
// question or open a chat go nowhere.

// islandScene is one look of the island: its name, and the display it draws.
type islandScene struct {
	Group string
	Name  string
	Make  func() islandDisplay
}

// islandGalleryWindow is the gallery window: the scene picked, the screen it is drawn on, and the island
// it draws with.
type islandGalleryWindow struct {
	appWindow
	scene     int
	screen    int
	hovered   bool
	backdrop  int
	island    *island
	sessions  []*islandSession
	startedAt time.Time
	// hidden is the scene taken away, for its island to go back into the notch; Play brings it
	// back. A Transition scene alternates by itself every 2.4 seconds.
	hidden   bool
	cycledAt time.Time
}

// islandGalleryScreens are the displays a scene can be drawn on.
var islandGalleryScreens = []struct {
	Label  string
	Screen islandScreen
}{
	{"MacBook notch (1512 wide, 185 notch)", islandScreenOf(1512, islandNotch{Width: 185, TopBar: 32})},
	{"MacBook Pro 16 notch (1728, 200 notch)", islandScreenOf(1728, islandNotch{Width: 200, TopBar: 38})},
	{"External display, no notch (1728)", islandScreenOf(1728, islandNotch{TopBar: 24})},
	{"Small display, no notch (1280)", islandScreenOf(1280, islandNotch{TopBar: 24})},
	{"4K display, no notch (3840)", islandScreenOf(3840, islandNotch{TopBar: 25})},
}

// galleryBot is the demo's bot at n, for a scene's avatar.
func galleryBot(n int) (string, string) {
	if store == nil || len(store.Bots) == 0 {
		return "", "Atlas"
	}
	bot := store.Bots[n%len(store.Bots)]
	return bot.ID, bot.Name
}

// gallerySession is a made-up session for a scene.
func gallerySession(key string, bot int, phase islandPhase) *islandSession {
	botID, name := galleryBot(bot)
	chatID := ""
	if store != nil && len(store.Chats) > 0 {
		chatID = store.Chats[bot%len(store.Chats)].ID
	}
	return &islandSession{Key: key, ChatID: chatID, BotID: botID, Title: name, Phase: phase, Running: phase == islandRunning,
		Generation: uint64(len(key) + bot + 1), StartedAt: time.Now().Add(-time.Duration(30+bot*50) * time.Second)}
}

func galleryAgent(key, kind, name string) *islandSession {
	s := gallerySession(key, 0, islandRunning)
	s.AgentKind, s.Title = kind, name
	return s
}

func withLine(s *islandSession, kind islandLineKind, text string) *islandSession {
	s.appendLine(kind, text)
	return s
}

func galleryPill(list ...*islandSession) islandPill {
	return newIslandMachine().pill(list, time.Now())
}

func galleryList(current *islandSession, list ...*islandSession) islandDisplay {
	return islandDisplay{Expanded: true, Surface: islandSurfaceList, Current: current, Sessions: list, Pill: galleryPill(list...), Shadow: true}
}

func galleryCard(surface islandSurface, s *islandSession, others ...*islandSession) islandDisplay {
	all := append([]*islandSession{s}, others...)
	return islandDisplay{Expanded: true, Surface: surface, Current: s, Sessions: all, Pill: galleryPill(all...), Shadow: true}
}

func galleryCompact(s *islandSession, others ...*islandSession) islandDisplay {
	all := append([]*islandSession{s}, others...)
	return islandDisplay{Current: s, Sessions: all, Pill: galleryPill(all...)}
}

func galleryApproval(key string, bot int, command string, always bool) *islandSession {
	s := gallerySession(key, bot, islandNeedsYou)
	decisions := []model.Answer{{Title: L("Allow once"), Decision: "allow"}, {Title: L("Deny"), Decision: "deny"}}
	if always {
		decisions = []model.Answer{{Title: L("Allow once"), Decision: "allow"}, {Title: L("Always allow"), Decision: "always"}, {Title: L("Deny"), Decision: "deny"}}
	}
	s.Ask = &islandAsk{Kind: islandAskPermission, ChatID: s.ChatID, MessageID: "m-" + key, BotName: s.Title, Command: command, Question: "$ " + command, Decisions: decisions}
	s.Detail = command
	return s
}

func galleryQuestion(key string, bot int, question string, options ...model.AskOption) *islandSession {
	s := gallerySession(key, bot, islandNeedsYou)
	s.Ask = &islandAsk{Kind: islandAskChoices, ChatID: s.ChatID, MessageID: "q-" + key, BotName: s.Title, Question: question, Options: options}
	s.Detail = question
	return s
}

func galleryForm(key string, bot int, detail string) *islandSession {
	s := gallerySession(key, bot, islandNeedsYou)
	s.Ask = &islandAsk{Kind: islandAskElsewhere, ChatID: s.ChatID, MessageID: "f-" + key, BotName: s.Title, Question: detail}
	s.Detail = detail
	return s
}

func galleryDone(key string, bot int, reply string) *islandSession {
	s := gallerySession(key, bot, islandCompleted)
	s.Unread = true
	return withLine(s, islandLineAssistant, reply)
}

func galleryFailed(key string, bot int, why string) *islandSession {
	s := gallerySession(key, bot, islandFailed)
	s.Unread, s.Detail = true, why
	return s
}

func galleryRunning(key string, bot int, detail string) *islandSession {
	s := gallerySession(key, bot, islandRunning)
	s.Detail, s.DetailFromTool = detail, true
	return s
}

// islandScenes are every look of the island, in Cindy's order: its coming and going, compact, the
// cards, and the list. A scene in Transition alternates between nothing and its session, so the
// island grows out of the notch and goes back in.
func islandScenes() []islandScene {
	return []islandScene{
		{"Transition", "Appear — a bot starts", func() islandDisplay {
			return galleryCompact(galleryRunning("r1", 0, "Install dependencies · $ bun install"))
		}},
		{"Transition", "Appear — a question opens", func() islandDisplay {
			return galleryCard(islandSurfaceInteraction, galleryApproval("p1", 0, "bun test --watch", true))
		}},
		{"Transition", "Appear — a result in", func() islandDisplay {
			return galleryCard(islandSurfaceCompletion, galleryDone("d1", 0, "4 files changed, tests pass. Opened PR #77."))
		}},
		{"Transition", "Appear — a failure", func() islandDisplay {
			return galleryCard(islandSurfaceCompletion, galleryFailed("e1", 1, "Rate limit reached, try again in a minute"))
		}},

		{"Compact", "Bot at work — a tool", func() islandDisplay {
			return galleryCompact(galleryRunning("r1", 0, "Install dependencies · $ bun install"))
		}},
		{"Compact", "Bot at work — streaming reply", func() islandDisplay {
			s := gallerySession("r2", 1, islandRunning)
			withLine(s, islandLineUser, "Summarize yesterday's standup")
			line := s.appendLine(islandLineAssistant, "Yesterday the team shipped the relay quota fix and started on push sync")
			s.Preview = line
			return galleryCompact(s)
		}},
		{"Compact", "Claude Code at work", func() islandDisplay {
			return galleryCompact(withLine(galleryAgent("a1", "claude", "Claude Code"), islandLineAssistant, "Updating the island's shape to hug the notch"))
		}},
		{"Compact", "Codex at work", func() islandDisplay {
			s := galleryAgent("a2", "codex", "Codex")
			s.Detail, s.DetailFromTool = "$ cargo test -p lorca", true
			return galleryCompact(s)
		}},
		{"Compact", "Two at work (badge R)", func() islandDisplay {
			return galleryCompact(galleryRunning("r1", 0, "$ bun test"), galleryAgent("a1", "claude", "Claude Code"))
		}},
		{"Compact", "Waiting on you (badge A, blue)", func() islandDisplay {
			return galleryCompact(galleryQuestion("q1", 1, "Ship 1.0.37 now?", model.AskOption{Label: "Ship"}))
		}},
		{"Compact", "Waiting and at work (badge A·R)", func() islandDisplay {
			return galleryCompact(galleryApproval("p1", 0, "rm -rf build", false), galleryRunning("r1", 1, "$ bun test"), galleryAgent("a1", "claude", "Claude Code"))
		}},
		{"Compact", "Done, unread (badge green)", func() islandDisplay {
			return galleryCompact(galleryDone("d1", 0, "4 files changed, tests pass"))
		}},
		{"Compact", "Failed", func() islandDisplay {
			return galleryCompact(galleryFailed("e1", 1, "Rate limit reached"))
		}},
		{"Compact", "Long title, CJK", func() islandDisplay {
			s := galleryRunning("r3", 0, "正在整理本周的产品周报并发给团队")
			s.Title = "产品周报助手（长名字测试）"
			return galleryCompact(s)
		}},
		{"Compact", "Many sessions (badge 12·3)", func() islandDisplay {
			var list []*islandSession
			for n := 0; n < 12; n++ {
				list = append(list, galleryDone(fmt.Sprint("d", n), n, "Done"))
			}
			for n := 0; n < 3; n++ {
				list = append(list, galleryRunning(fmt.Sprint("r", n), n, "$ bun test"))
			}
			return galleryCompact(list[0], list[1:]...)
		}},

		{"Card", "Approval — command, Always allow", func() islandDisplay {
			return galleryCard(islandSurfaceInteraction, galleryApproval("p1", 0, "bun test --watch", true))
		}},
		{"Card", "Approval — command, no rule", func() islandDisplay {
			return galleryCard(islandSurfaceInteraction, galleryApproval("p2", 1, "git push origin feat/island-cindy", false))
		}},
		{"Card", "Approval — coding agent", func() islandDisplay {
			s := galleryApproval("p3", 0, "rm -rf node_modules && bun install", true)
			s.AgentKind, s.Title = "claude", "Claude Code"
			return galleryCard(islandSurfaceInteraction, s)
		}},
		{"Card", "Question — options answered in place", func() islandDisplay {
			return galleryCard(islandSurfaceInteraction, galleryQuestion("q1", 0, "Ship 1.0.37 now, or wait for the phone fix?",
				model.AskOption{Label: "Ship now", Description: "Tag, build, and upload the draft release"},
				model.AskOption{Label: "Wait for the fix", Description: "Hold the release until the phone fix lands"},
				model.AskOption{Label: "Draft release notes only"}))
		}},
		{"Card", "Question — form, Review opens the chat", func() islandDisplay {
			return galleryCard(islandSurfaceInteraction, galleryForm("f1", 1, "Which database should the migration use?"))
		}},
		{"Card", "Question — awaiting your reply (no detail)", func() islandDisplay {
			s := galleryQuestion("q2", 0, "", model.AskOption{Label: "Yes"}, model.AskOption{Label: "No"})
			s.Detail = ""
			return galleryCard(islandSurfaceInteraction, s)
		}},
		{"Card", "Completed — swipe right to mark read", func() islandDisplay {
			return galleryCard(islandSurfaceCompletion, galleryDone("d1", 0, "4 files changed, tests pass. Opened PR #77."))
		}},
		{"Card", "Completed — the user's words as preview", func() islandDisplay {
			s := galleryDone("d2", 1, "")
			line := s.appendLine(islandLineUser, "Book the team dinner for Friday")
			s.Lines = []islandLine{*line}
			s.Preview = line
			return galleryCard(islandSurfaceCompletion, s)
		}},
		{"Card", "Failed", func() islandDisplay {
			return galleryCard(islandSurfaceCompletion, galleryFailed("e1", 1, "Rate limit reached, try again in a minute"))
		}},
		{"Card", "Failed — a long error", func() islandDisplay {
			return galleryCard(islandSurfaceCompletion, galleryFailed("e2", 0, "The provider refused the request: the API key for OpenAI was revoked on another Device"))
		}},
		{"Card", "Failed — coding agent", func() islandDisplay {
			s := galleryFailed("e3", 0, "Claude Code exited with code 1 after the tests failed")
			s.AgentKind, s.Title = "claude", "Claude Code"
			return galleryCard(islandSurfaceCompletion, s)
		}},
		{"Card", "Running card (single session expanded)", func() islandDisplay {
			return galleryCard(islandSurfaceList, galleryRunning("r1", 0, "Install dependencies · $ bun install"))
		}},

		{"List", "Two sessions", func() islandDisplay {
			a, b := galleryApproval("p1", 0, "bun test", true), galleryRunning("r1", 1, "Reading the relay logs")
			return galleryList(a, a, b)
		}},
		{"List", "Failure among others", func() islandDisplay {
			f, d, r := galleryFailed("e1", 1, "Rate limit reached, try again in a minute"), galleryDone("d1", 0, "4 files changed, tests pass"),
				galleryRunning("r1", 0, "Install dependencies · $ bun install")
			return galleryList(f, f, d, r)
		}},
		{"List", "Four sessions, every state", func() islandDisplay {
			a, d, g, r := galleryApproval("p1", 1, "bun test --watch", true), galleryDone("d1", 0, "4 files changed, tests pass"),
				withLine(galleryAgent("a1", "claude", "Claude Code"), islandLineAssistant, "Writing the gallery window"), galleryRunning("r1", 0, "Install dependencies · $ bun install")
			return galleryList(a, a, d, g, r)
		}},
		{"List", "Seven sessions (scrolls past five)", func() islandDisplay {
			list := []*islandSession{
				galleryApproval("p1", 0, "bun test", true), galleryForm("f1", 1, "Pick a database"), galleryFailed("e1", 2, "Rate limit reached"),
				galleryDone("d1", 0, "Shipped"), galleryDone("d2", 1, "Notes drafted"),
				galleryAgent("a1", "codex", "Codex"), galleryRunning("r1", 2, "$ cargo build"),
			}
			return galleryList(list[0], list...)
		}},
	}
}

// showIslandGallery opens the gallery window, or brings it forward.
func (a *appDelegate) showIslandGallery() {
	if a.gallery == nil {
		g := &islandGalleryWindow{island: &island{m: newIslandMachine(), swipe: map[string]*islandSwipe{}}, startedAt: time.Now()}
		win := a.newWindow(&g.appWindow, mygo.WindowOptions{
			Title:           "Island Gallery - " + appName(),
			Width:           1240,
			Height:          760,
			MinWidth:        900,
			MinHeight:       520,
			BackgroundColor: "light-dark(#ffffff, #1c1c1c)",
		}, g.view)
		win.OnClosed(func() {
			if a.gallery != nil && a.gallery.win.ID() == win.ID() {
				a.gallery = nil
			}
		})
		win.SetMenu(windowMenuBar(windowOther))
		a.gallery = g
	}
	a.gallery.win.Show()
	a.gallery.win.Focus()
}

// The backdrops a scene is drawn over: the menu bar of a light and a dark desktop.
var islandGalleryBackdrops = []string{"Dark desktop", "Light desktop", "Wallpaper"}

func (g *islandGalleryWindow) view(c *ui.Context) {
	p := colors(c)
	scenes := islandScenes()
	g.scene = min(max(g.scene, 0), len(scenes)-1)
	scene := scenes[g.scene]
	screen := islandGalleryScreens[g.screen].Screen
	// Arrow keys step through the scenes.
	if c.Shortcut(0, ui.KeyDown) || c.Shortcut(0, ui.KeyRight) {
		g.scene = (g.scene + 1) % len(scenes)
	}
	if c.Shortcut(0, ui.KeyUp) || c.Shortcut(0, ui.KeyLeft) {
		g.scene = (g.scene - 1 + len(scenes)) % len(scenes)
	}
	ui.Row(c).Fill().Background(p.Window).Children(func() {
		// The list of scenes.
		ui.Column(c).Width(300).FillHeight().Background(p.Sidebar).Border(0, p.Separator).Children(func() {
			ui.Column(c).FillWidth().Padding(16, 16, 10, 16).Gap(4).Children(func() {
				ui.Text(c, "Island Gallery").FontSize(15).FontWeight(600).TextColor(p.Label)
				ui.Text(c, "Every look of the island, live. ↑ ↓ to step.").FontSize(12).TextColor(p.Label2)
			})
			ui.Scroll(c).FillWidth().Grow(1).Children(func() {
				ui.Column(c).FillWidth().Padding(0, 8, 12, 8).Gap(1).Children(func() {
					group := ""
					for n, s := range scenes {
						if s.Group != group {
							group = s.Group
							ui.Text(c, group).FontSize(11).FontWeight(600).TextColor(p.Label3).Padding(10, 8, 4, 8)
						}
						row := ui.ButtonBase(c.Key(fmt.Sprint("scene-", n))).FillWidth().Height(28).Padding(0, 8).Radius(radiusMD).Justify(ui.Start).AlignItems(ui.Center).Label(s.Name)
						switch {
						case n == g.scene:
							row.Background(p.Selection)
						case row.Hovered():
							row.Background(p.Hover)
						}
						ink := p.Label
						if n == g.scene {
							ink = p.SelectionText
						}
						row.Children(func() {
							ui.Text(c, s.Name).FontSize(12.5).TextColor(ink).SingleLine().Shrink(1).MinWidth(0)
						})
						if row.Clicked() {
							g.scene, g.hidden, g.cycledAt = n, false, time.Time{}
						}
					}
				})
			})
		})
		// The stage: the controls, then the island on its menu bar.
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			ui.Row(c).FillWidth().Padding(14, 18).Gap(14).AlignItems(ui.Center).Border(0, p.Separator).Children(func() {
				options := make([]popUpOption, len(islandGalleryScreens))
				for n, s := range islandGalleryScreens {
					options[n] = popUpOption{Value: fmt.Sprint(n), Label: s.Label}
				}
				if picked, changed, _ := popUpButton(c, popUp{Options: options, Value: fmt.Sprint(g.screen), Style: popUpSettings, Label: "Display"}); changed {
					fmt.Sscan(picked, &g.screen)
				}
				segmented(c, &g.backdrop, "Backdrop", islandGalleryBackdrops...)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					toggleSwitch(c, &g.hovered, true).Label("Pointer over it")
					ui.Text(c, "Pointer over it").FontSize(12).TextColor(p.Label2)
				})
				ui.Spacer(c)
				label := "Hide Island"
				if g.hidden {
					label = "Show Island"
				}
				if pushButton(c, label, pushOptions{Small: true, Tooltip: "Take the scene away, or bring it back, to watch the island go into the notch and come out"}).Clicked() {
					g.hidden, g.cycledAt = !g.hidden, time.Time{}
				}
				if pushButton(c, "Live Island", pushOptions{Small: true, Tooltip: "Show the menu bar's island in this scene's place"}).Clicked() {
					islandOpen()
				}
			})
			stage := ui.Box(c).FillWidth().Grow(1).Clip()
			switch g.backdrop {
			case 1:
				stage.Background(ui.Hex("#ececec"))
			case 2:
				stage.Gradient(ui.Hex("#5b7fd6"), ui.Hex("#d27aa8"), 160)
			default:
				stage.Background(ui.Hex("#1f2430"))
			}
			stage.Children(func() {
				// The menu bar strip the island sits on.
				bar := ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(screen.TopBar)
				if g.backdrop == 1 {
					bar.Background(ui.RGBA(255, 255, 255, 0.72))
				} else {
					bar.Background(ui.RGBA(20, 20, 24, 0.55))
				}
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
					d := scene.Make()
					// A Transition scene comes and goes by itself; any scene can be taken away and
					// brought back with Hide and Show.
					if scene.Group == "Transition" {
						if g.cycledAt.IsZero() {
							g.cycledAt = c.Now()
						}
						if c.Now().Sub(g.cycledAt) >= 2400*time.Millisecond {
							g.hidden, g.cycledAt = !g.hidden, c.Now()
						}
						c.After(2400*time.Millisecond - c.Now().Sub(g.cycledAt))
					}
					if g.hidden {
						d = islandDisplay{}
					}
					g.island.draw(c, d, screen, g.hovered)
				})
				ui.Column(c).Absolute().Left(18).Bottom(14).Gap(2).Children(func() {
					d := scene.Make()
					l := islandLayoutFor(d, screen, islandWindowWidth, g.island.measured, g.hovered)
					ui.Text(c, scene.Group+" · "+scene.Name).FontSize(12).FontWeight(600).TextColor(ui.RGBA(255, 255, 255, 0.85))
					ui.Text(c, fmt.Sprintf("%.0f × %.0f  ·  top %.0f  ·  radius %.0f  ·  notch %.0f", l.Width, l.Height, l.TopExtension, l.BottomRadius, screen.NotchWidth)).
						Font(monoFont).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.6))
				})
			})
		})
	})
}
