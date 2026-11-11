package main

import (
	"slices"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// botAccessSheet is a bot's Access in one place: which plugins on its Runner it may use, each
// plugin on or off with each of its tools on or asking first, the skills it knows, and its own
// files and shell commands. Only the user changes it; a chat's access request opens this sheet
// too. The choices live here, across frames, until Save.
type botAccessSheet struct {
	botID, botName string
	runner         *model.Device
	saved          *model.BotPermissions
	// plugins are the Runner's own list until it says what tools each one has.
	plugins []model.AccessPlugin
	levels  map[string]model.AccessLevel
	// tools holds each plugin's own switches, by tool name; a tool not here takes the plugin's.
	tools    map[string]map[string]model.AccessLevel
	expanded map[string]bool
	drafts   bool
	// read, write, and shell are the bot's own tools: on or off, as the Runner's permission is.
	read, write, shell model.AccessLevel
	// browser is the one switch for Browser, whichever browser the Runner uses.
	browser bool
	// skills are the ones this bot may read, each switch on or off. Lorca's own are not listed:
	// they are how a bot reads its docs and writes one, and no switch hides them.
	skills []model.PlaybookSummary
	shown  map[string]bool
	// problem is why the tools could not be listed.
	problem string
	closed  bool
}

func (w *appWindow) presentBotAccess(botID string) *botAccessSheet {
	bot := store.Bot(botID)
	if bot == nil {
		return nil
	}
	saved := bot.Permissions
	if saved == nil {
		saved = model.FullAccess()
	}
	st := &botAccessSheet{
		botID: botID, botName: bot.Name, runner: store.Device(bot.RunnerID), saved: saved.Clone(),
		levels: map[string]model.AccessLevel{}, tools: map[string]map[string]model.AccessLevel{}, expanded: map[string]bool{},
		drafts:  saved.Drafts,
		read:    saved.Tools.Read.Level(),
		write:   saved.Tools.Write.Level(),
		shell:   saved.Tools.Shell.Level(),
		browser: saved.Level(model.BrowserPluginID) != model.AccessNone,
		shown:   map[string]bool{},
	}
	// The skills a bot sees, other than Lorca's own, each with its switch where it starts.
	var chatID string
	if chat := selectedChat(); chat != nil {
		chatID = chat.ID
	}
	scope, ok := model.SkillScope(store.Chat(chatID), []*model.Bot{bot})
	if ok {
		store.EnsureSkills(bot.ID, chatID)
		st.skills = append(store.Skills(scope), store.SharedSkills()...)
	}
	st.skills = slices.DeleteFunc(st.skills, func(skill model.PlaybookSummary) bool { return skill.Builtin })
	for _, skill := range st.skills {
		key := skill.Scope.Kind + "/" + skill.ID
		st.shown[key] = saved.ShowsSkill(skill.Scope.Kind, skill.ID, skill.Builtin)
	}
	// The Runner's own list first, so the sheet opens complete while it is asked for tools.
	var listed []model.AccessPlugin
	if st.runner != nil {
		for _, plugin := range st.runner.Plugins {
			if !plugin.IsBrowser() {
				listed = append(listed, model.AccessPlugin{ID: plugin.ID, Name: plugin.Name})
			}
		}
	}
	st.show(listed)
	w.present(st.view, func() { st.closed = true })
	store.BotAccessCatalog(botID, func(catalog model.AccessCatalog, err error) {
		if st.closed {
			return
		}
		if err != nil {
			st.problem = L("Couldn't get the tools from %@.", st.runnerName())
			return
		}
		st.show(catalog.Connections)
	})
	return st
}

func (st *botAccessSheet) runnerName() string {
	if st.runner != nil {
		return st.runner.Name
	}
	return L("its Runner")
}

// show lists `listed`, keeping what the user already chose for a plugin listed before.
func (st *botAccessSheet) show(listed []model.AccessPlugin) {
	listed = slices.DeleteFunc(slices.Clone(listed), func(plugin model.AccessPlugin) bool { return plugin.ID == model.BrowserPluginID })
	st.plugins = listed
	for _, plugin := range listed {
		if _, ok := st.levels[plugin.ID]; ok {
			continue
		}
		st.levels[plugin.ID] = st.saved.Level(plugin.ID)
		if st.saved.Connections != nil {
			own := map[string]model.AccessLevel{}
			for name, permission := range (*st.saved.Connections)[plugin.ID].Tools {
				own[name] = permission.Level()
			}
			if len(own) > 0 {
				st.tools[plugin.ID] = own
			}
		}
	}
}

// toolsSummary is "All tools", or how many of them the bot may use. Nothing before the plugin
// has connected once, or when it is off.
func (st *botAccessSheet) toolsSummary(plugin model.AccessPlugin) string {
	level := st.levels[plugin.ID]
	if len(plugin.Tools) == 0 || level == model.AccessNone {
		return ""
	}
	on := 0
	for _, tool := range plugin.Tools {
		if st.toolLevel(plugin.ID, tool.Name) != model.AccessNone {
			on++
		}
	}
	if on == len(plugin.Tools) {
		return L("All tools")
	}
	return L("%d of %d tools", on, len(plugin.Tools))
}

// toolLevel is a tool's own switch, else the plugin's.
func (st *botAccessSheet) toolLevel(pluginID, tool string) model.AccessLevel {
	if own, ok := st.tools[pluginID]; ok {
		if level, ok := own[tool]; ok {
			return level
		}
	}
	return st.levels[pluginID]
}

// setTool gives one tool its own switch, or takes it back when it matches the plugin's again.
func (st *botAccessSheet) setTool(pluginID, tool string, level model.AccessLevel) {
	if level == st.levels[pluginID] {
		delete(st.tools[pluginID], tool)
		if len(st.tools[pluginID]) == 0 {
			delete(st.tools, pluginID)
		}
		return
	}
	if st.tools[pluginID] == nil {
		st.tools[pluginID] = map[string]model.AccessLevel{}
	}
	st.tools[pluginID][tool] = level
}

// policy is what Save writes. Every plugin fully open is the Runner's every plugin, one installed
// later included.
func (st *botAccessSheet) policy() *model.BotPermissions {
	policy := &model.BotPermissions{Drafts: st.drafts}
	// The bot's own reads are not on this sheet: they carry over as saved, and a bot that may
	// change files needs them, so they come back on if they were off.
	// The bot's own tools are each on or off, and reading comes back on when changing files does:
	// a bot that may write reads what it writes.
	read := st.read
	if st.write != model.AccessNone {
		read = model.AccessOn
	}
	policy.Tools = model.LocalPermissions{Shell: st.shell.Permission(), Read: read.Permission(), Write: st.write.Permission()}
	if st.saved != nil {
		policy.Skills = st.saved.Skills
	}
	// Each skill's switch is where the bot turned it from the default.
	for _, skill := range st.skills {
		key := skill.Scope.Kind + "/" + skill.ID
		on, was := st.shown[key], st.saved.ShowsSkill(skill.Scope.Kind, skill.ID, skill.Builtin)
		if on != was {
			policy.SetSkill(skill.Scope.Kind, skill.ID, on, skill.Builtin)
		}
	}
	open := st.browser
	for _, plugin := range st.plugins {
		// A plugin's own switch at "on" with no tool of its own changed keeps it open, so a
		// plugin installed later starts on rather than off.
		changed := len(st.tools[plugin.ID]) > 0
		if st.levels[plugin.ID] != model.AccessOn || changed {
			open = false
		}
	}
	if !open {
		connections := map[string]model.ConnectionPermissions{}
		for _, plugin := range st.plugins {
			level := st.levels[plugin.ID]
			if level == model.AccessNone {
				continue
			}
			grant := model.ConnectionPermissions{Enabled: true}
			for _, tool := range plugin.Tools {
				if own, ok := st.tools[plugin.ID][tool.Name]; ok {
					if grant.Tools == nil {
						grant.Tools = map[string]model.ToolPermission{}
					}
					grant.Tools[tool.Name] = own.Permission()
				}
			}
			connections[plugin.ID] = grant
		}
		if st.browser {
			connections[model.BrowserPluginID] = model.ConnectionPermissions{Enabled: true}
		}
		policy.Connections = &connections
	}
	return policy
}

// A tool's three choices, as the icons their control shows: on, ask first, and off.
var toolChoices = []iconTab{
	{Symbol: "checkmark.circle.fill", Title: L("On")},
	{Symbol: "questionmark.circle", Title: L("Ask first")},
	{Symbol: "xmark.circle", Title: L("Off")},
}

// toolChoice is the segment a tool's switch sits on.
func toolChoice(level model.AccessLevel) int {
	switch level {
	case model.AccessNone:
		return 2
	case model.AccessAsk:
		return 1
	}
	return 0
}

// accessLevel is the switch a segment stands for.
func accessLevel(choice int) model.AccessLevel {
	switch choice {
	case 2:
		return model.AccessNone
	case 1:
		return model.AccessAsk
	}
	return model.AccessOn
}

// toolChoice draws a tool's on, ask, or off choice at its row's end: the inspector's own tab
// control, with a symbol per choice. It is short enough to sit in a row: 26 high, the capsule 20.
func (st *botAccessSheet) toolChoice(c *ui.Context, key string, choice *int, name string) bool {
	before := *choice
	c.Key(key)
	track := iconTabsSized(c, choice, L("%@ access", name), toolChoices, 24).Width(78).Shrink(0)
	// A tool that is off reads as set aside, not as a choice among equals.
	if *choice == 2 {
		track.Opacity(0.55)
	}
	return *choice != before
}

func (st *botAccessSheet) view(c *ui.Context, s *sheet) {
	p := colors(c)
	result := sheetFrame(c, sheetOptions{Title: L("Access for %@", st.botName), Width: 560, Confirm: L("Save")}, func() {
		if len(st.plugins) > 0 {
			ui.Column(c).Gap(6).Children(func() {
				section(c, L("Plugins"), sectionCaption, nil, func(k *card) {
					for _, plugin := range st.plugins {
						ui.Column(c.Key("plugin:" + plugin.ID)).Children(func() { st.pluginRow(c, k, plugin) })
					}
				})
				if st.problem != "" {
					ui.Text(c, st.problem).FontSize(textCaption).TextColor(p.Label2).LineHeight(1.4)
				}
			})
		}
		ui.Column(c).Gap(6).Margin(4, 0, 0, 0).Children(func() {
			section(c, L("Skills"), sectionCaption, nil, func(k *card) {
				if len(st.skills) == 0 {
					noteRow(c, k, L("No skills on this bot's Runner yet."), nil)
					return
				}
				for _, skill := range st.skills {
					skill := skill
					key := skill.Scope.Kind + "/" + skill.ID
					on := st.shown[key]
					accessoryRow(c, k, skill.Name, skill.Description, func() {
						settingsSwitch(c, &on, skill.Name, false)
						if on != st.shown[key] {
							st.shown[key] = on
						}
					})
				}
			})
			ui.Text(c, L("Lorca's own skills always show: they are how %@ reads its docs and writes one.", st.botName)).FontSize(textCaption).TextColor(p.Label2).LineHeight(1.4)
		})
		ui.Column(c).Gap(6).Margin(4, 0, 0, 0).Children(func() {
			section(c, L("Messages"), sectionCaption, nil, func(k *card) {
				accessoryRow(c, k, L("Draft first"), "", func() {
					settingsSwitch(c, &st.drafts, L("Draft first"), false)
				})
			})
			ui.Text(c, L("Emails and Slack messages wait in the chat for you to send. Off, %@ sends them itself where the service can.", st.botName)).FontSize(textCaption).TextColor(p.Label2).LineHeight(1.4)
		})
		ui.Column(c).Gap(6).Margin(4, 0, 0, 0).Children(func() {
			// Files and shell commands are the Runner's, so its card goes by the Runner's name.
			title := L("Runner")
			if st.runner != nil {
				title = st.runner.Name
			}
			section(c, title, sectionCaption, nil, func(k *card) {
				// The own tools are each on or off, as the Runner's own permission is.
				for _, own := range []struct {
					title string
					level *model.AccessLevel
				}{
					{L("Read files"), &st.read},
					{L("Change files"), &st.write},
					{L("Shell commands"), &st.shell},
				} {
					own := own
					on := *own.level != model.AccessNone
					accessoryRow(c, k, own.title, "", func() {
						settingsSwitch(c, &on, own.title, false)
						*own.level = model.AccessOn
						if !on {
							*own.level = model.AccessNone
						}
					})
				}
				accessoryRow(c, k, L("Browser"), "", func() {
					settingsSwitch(c, &st.browser, L("Browser"), false)
				})
			})
			ui.Text(c, L("Shell commands run as you on %@ and can reach anything you can there.", st.runnerName())).FontSize(textCaption).TextColor(p.Label2).LineHeight(1.4)
		})
	})
	switch {
	case result.Cancelled:
		s.dismiss()
	case result.Confirmed:
		if policy := st.policy(); !model.EqualBotPermissions(policy, st.saved) {
			store.SetBotPermissions(st.botID, policy)
		}
		s.dismiss()
	}
}

// pluginRow is a plugin, after the Mac's PluginAccessRow: its mark and name, how many of its tools
// the bot may use, and how far. A click on the row shows the tools under it.
func (st *botAccessSheet) pluginRow(c *ui.Context, k *card, plugin model.AccessPlugin) {
	p := colors(c)
	level := st.levels[plugin.ID]
	canExpand := len(plugin.Tools) > 0 && level != model.AccessNone
	if !canExpand {
		delete(st.expanded, plugin.ID)
	}
	expanded := st.expanded[plugin.ID]
	r := k.row(rowBox(c).MinHeight(44).Padding(0, 12, 0, 6).Gap(4).Label(plugin.Name))
	r.Children(func() {
		// Everything but the pop-up shows or hides the tools.
		head := ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).AlignSelf(ui.Stretch).AlignItems(ui.Center)
		if canExpand {
			head.Cursor(ui.CursorPointer)
			if head.Clicked() {
				st.expanded[plugin.ID] = !expanded
			}
		}
		head.Children(func() {
			disclosure := ui.Row(c).Size(16, 16).Shrink(0).Center().TextColor(p.Label2)
			if canExpand {
				name := "chevron.right"
				if expanded {
					name = "chevron.down"
				}
				disclosure.Label(L("%@ tools", plugin.Name)).Children(func() { symbol(c, name, 12, 2) })
			}
			ui.Row(c).Width(18).Shrink(0).Justify(ui.Center).TextColor(p.Label2).Children(func() {
				// A named account (Gmail · Work) has its service's mark.
				symbolName, markID := "puzzlepiece.extension", plugin.ID
				if st.runner != nil {
					for _, installed := range st.runner.Plugins {
						if installed.ID == plugin.ID {
							symbolName, markID = installed.Symbol(), installed.MarketplaceID()
						}
					}
				}
				pluginTile(c, markID, symbolName, 18)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Margin(0, 0, 0, 6).Children(func() {
				ui.Text(c, plugin.Name).FontSize(12.5).FontWeight(500).SingleLine()
				if summary := st.toolsSummary(plugin); summary != "" {
					ui.Text(c, summary).FontSize(textCaption).TextColor(p.Label2).SingleLine()
				}
			})
		})
		if canExpand && head.Hovered() {
			r.Background(p.RowHover)
		}
		// A plugin has an on switch; each of its tools has its own on and ask switches.
		enabled := level != model.AccessNone
		if settingsSwitch(c, &enabled, L("Access to %@", plugin.Name), false) {
			st.levels[plugin.ID] = model.AccessOn
			if !enabled {
				st.levels[plugin.ID] = model.AccessNone
			}
		}
	})
	if !expanded {
		return
	}
	// The tools, each with what it does and one three-way choice: on, ask first, or off. Long
	// lists scroll.
	k.row(ui.Scroll(c.Key("tools")).MaxHeight(180).Padding(2, 0, 2, 0)).Children(func() {
		for _, tool := range plugin.Tools {
			capability := tool.Capability
			if capability == "" {
				capability = "write"
			}
			toolLevel := st.toolLevel(plugin.ID, tool.Name)
			row := ui.Row(c.Key(tool.Name)).Height(30).Gap(10).Padding(0, 10, 0, 8).Radius(radiusSM).AlignItems(ui.Center)
			row.Children(func() {
				ui.Text(c, tool.Shown()).Grow(1).Shrink(1).MinWidth(0).FontSize(12).TextColor(p.Label).SingleLine()
				does := map[string]string{"read": L("Reads"), "draft": L("Drafts")}[capability]
				if does == "" {
					does = L("Changes")
				}
				ui.Text(c, does).Shrink(0).FontSize(11).TextColor(p.Label3).SingleLine()
				// On, ask first, or off, in one control: asking waits for the user every time,
				// whatever Auto-review says.
				choice := toolChoice(toolLevel)
				if st.toolChoice(c, "tool:"+tool.Name, &choice, tool.Shown()) {
					st.setTool(plugin.ID, tool.Name, accessLevel(choice))
				}
			})
			if row.Hovered() {
				row.Background(p.RowHover)
			}
		}
	})
}
