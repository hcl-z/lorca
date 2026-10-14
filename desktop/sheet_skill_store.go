package main

import (
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// The Skill Store, opened by the + of a Skills section: skills from skills.sh for the bot, the
// group, or every bot on the Runner, whichever section opened it. The field searches skills.sh as
// it is typed, or takes a link to a skill on skills.sh, ClawHub, or GitHub. Add copies the skill's
// folder, and the bots use it from their next turn; New Blank Skill writes one from scratch.

// skillTopics start a search with one click while the field is empty.
var skillTopics = []string{"pdf", "docx", "react", "testing", "design", "git", "data", "browser", "deploy", "writing"}

type skillStoreState struct {
	w     *appWindow
	scope model.PlaybookScope
	// via is the bot whose Runner keeps what is added for every bot, or "" for this computer.
	via string
	// owner is who uses what is added: the bot's name, the group's, or every bot.
	owner string
	query string
	// searched is the query the hits answer.
	searched string
	hits     []model.SkillHit
	// adding and added are the references being added and added in this sheet.
	adding, added  map[string]bool
	loading        bool
	status, notice string
	failed, closed bool
	generation     int
	timer          *time.Timer
}

// presentSharedSkillStore is the sidebar's Create › Add a Skill for Every Bot…: the store for the
// skills every bot on this computer uses.
func (w *appWindow) presentSharedSkillStore() {
	w.openSkillStore(model.SharedScope(), "")
}

// presentSkillStore is a Skills section's +: the store for that bot or group.
func (w *appWindow) presentSkillStore(scope model.PlaybookScope) {
	w.openSkillStore(scope, store.SkillsBot())
}

func (w *appWindow) openSkillStore(scope model.PlaybookScope, via string) {
	st := &skillStoreState{w: w, scope: scope, via: via, adding: map[string]bool{}, added: map[string]bool{}, owner: L("every bot on this computer")}
	switch {
	case scope.IsGroup():
		if chat := store.Chat(scope.ID); chat != nil {
			st.owner = store.Title(chat)
		}
	case !scope.IsShared():
		if bot := store.Bot(scope.ID); bot != nil {
			st.owner = bot.Name
		}
	}
	w.present(st.view, func() {
		st.closed = true
		if st.timer != nil {
			st.timer.Stop()
		}
	})
}

// skillStore is Chat › Skill Store…: for the bot in the DM on screen, or the group.
func (m *mainWindow) skillStore() {
	chat := store.Chat(m.selection.ChatID)
	if scope, ok := model.SkillScope(chat, store.BotsIn(chat)); ok {
		m.presentSkillStore(scope)
	}
}

// isSkillAddress is whether the field holds a link or owner/repo rather than words to search for.
func isSkillAddress(text string) bool {
	return !strings.ContainsAny(text, " \t") && strings.ContainsAny(text, "/:")
}

// hold is what the scope has of a listed skill: added here or imported from that address before, or
// another skill under its name, which Add would be refused for.
func (st *skillStoreState) hold(reference string) model.SkillHold {
	if st.added[reference] {
		return model.SkillHeld
	}
	return model.Holds(store.Skills(st.scope), reference)
}

// queryChanged searches a moment after the last keystroke.
func (st *skillStoreState) queryChanged() {
	st.generation++
	generation := st.generation
	if st.timer != nil {
		st.timer.Stop()
	}
	query := strings.TrimSpace(st.query)
	if isSkillAddress(query) || len([]rune(query)) < model.SkillSearchMin {
		st.loading, st.status, st.failed, st.hits, st.searched = false, "", false, nil, ""
		return
	}
	st.timer = store.After(250*time.Millisecond, func() {
		if !st.closed && generation == st.generation {
			st.search(generation)
		}
	})
}

// searchFor puts a topic in the field and searches it at once.
func (st *skillStoreState) searchFor(topic string) {
	st.query = topic
	st.generation++
	st.search(st.generation)
}

func (st *skillStoreState) search(generation int) {
	query := strings.TrimSpace(st.query)
	st.loading, st.status, st.failed = true, "", false
	store.SearchSkills(query, func(hits []model.SkillHit, err error) {
		if st.closed || generation != st.generation {
			return
		}
		st.loading, st.searched = false, query
		if err != nil {
			st.hits, st.status, st.failed = nil, model.ErrorText(err), true
			return
		}
		st.hits = hits
		if len(hits) == 0 {
			st.status = L("Nothing on skills.sh matches “%@”. Try other words, or paste a link to a skill.", query)
		}
	})
}

// add copies the skill into the scope, at once.
func (st *skillStoreState) add(reference string) {
	if st.adding[reference] {
		return
	}
	st.adding[reference], st.notice, st.failed = true, "", false
	store.ImportSkill(st.scope, st.via, reference, func(skill model.PlaybookRecord, skipped []string, err error) {
		delete(st.adding, reference)
		if st.closed {
			return
		}
		if err != nil {
			st.notice, st.failed = model.ErrorText(err), true
			return
		}
		st.added[reference] = true
		name := reference
		if skill.Content != nil {
			name = skill.Content.Name
		}
		st.notice = L("Added “%@” for %@. It's used from the next turn.", name, st.owner)
		if len(skipped) > 0 {
			st.notice += " " + L("%d files weren't copied: a browser recording is made by you, not imported.", len(skipped))
		}
	})
}

// row is one skill: Add; Added when the scope has this very skill; or Name in Use when another
// skill of the scope has its name, which Add would be refused for. A copy skills.sh marks is
// dimmed, as its page dims it. A click elsewhere on the row opens its page.
func (st *skillStoreState) row(c *ui.Context, key, title, byline, subtitle, link, reference string, copied bool) {
	media := func() { marketPluginIcon(c, "", "book.closed", 36) }
	accessory := func() {
		p := colors(c)
		switch hold := st.hold(reference); {
		case st.adding[reference]:
			spinner(c, 14).Label(L("Adding %@", title))
		case hold == model.SkillHeld:
			ui.Row(c).Gap(5).Children(func() {
				symbol(c, "checkmark", 12, 2.6).TextColor(p.Green)
				ui.Text(c, L("Added")).FontSize(12.5).TextColor(p.Label2).SingleLine()
			})
		case hold == model.SkillNameTaken:
			ui.Text(c, L("Name in Use")).FontSize(12.5).TextColor(p.Label3).SingleLine().
				Tooltip(L("%@ already has a skill named “%@”. Rename or delete it to add this one.", st.owner, model.ReferenceName(reference)))
		default:
			if pushButton(c, L("Add"), pushOptions{Small: true, Tooltip: L("Add %@ for %@", title, st.owner)}).Clicked() {
				st.add(reference)
			}
		}
	}
	if copied {
		subtitle = L("%@ · a copy of another skill", subtitle)
	}
	box := ui.Box(c.Key(key + "/row"))
	if copied {
		box.Opacity(0.55)
	}
	box.Children(func() {
		if marketRow(c, key, marketRowOptions{Title: title, Byline: byline, Subtitle: subtitle}, media, accessory) && link != "" {
			openExternal(link)
		}
	})
}

// topics are the one-click searches shown while the field is empty.
func (st *skillStoreState) topics(c *ui.Context) {
	p := colors(c)
	ui.Column(c).Gap(10).Padding(6, 2).Children(func() {
		ui.Text(c, L("Search the skills people share on skills.sh, or paste a link to one on skills.sh, ClawHub, or GitHub.")).
			FontSize(12).LineHeight(1.45).TextColor(p.Label2)
		ui.Text(c, L("Try")).FontSize(11.5).FontWeight(500).TextColor(p.Label3)
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, topic := range skillTopics {
				if pushButton(c.Key("topic-"+topic), topic, pushOptions{Small: true}).Clicked() {
					st.searchFor(topic)
				}
			}
		})
	})
}

func (st *skillStoreState) view(c *ui.Context, sh *sheet) {
	p := colors(c)
	address := strings.TrimSpace(st.query)
	subtitle := L("Skills for %@. They work as soon as you add them, so read one before you rely on it.", st.owner)
	result := sheetFrame(c, sheetOptions{
		Title:    L("Skill Store"),
		Subtitle: subtitle,
		Width:    640,
		Confirm:  L("Done"),
		NoCancel: true,
		Leading: func() {
			if pushButton(c, L("New Blank Skill…"), pushOptions{Symbol: "square.and.pencil", Tooltip: L("Write a skill for %@ from scratch", st.owner)}).Clicked() {
				scope, via := st.scope, st.via
				sh.dismiss()
				st.w.presentPlaybook(scope, nil).via = via
			}
		},
	}, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			textField(c.Key("skill-query"), &st.query, fieldOptions{Placeholder: L("Search skills.sh or paste a link"), Label: L("Search skills.sh or paste a link"), AutoFocus: true}).
				Grow(1).OnChange(st.queryChanged)
			if st.loading {
				spinner(c, 14).Label(L("Searching"))
			}
		})
		if st.notice != "" {
			tint := p.Label2
			if st.failed {
				tint = p.Red
			}
			ui.Text(c, st.notice).FontSize(11.5).LineHeight(1.4).TextColor(tint)
		}
		ui.Column(c.Key("skill-body")).Height(400).Children(func() {
			switch {
			case isSkillAddress(address):
				ui.Column(c).Gap(2).Children(func() {
					st.row(c, "skill-address", address, "", L("The skill at this address"), "", address, false)
				})
			case len(st.hits) > 0:
				// In skills.sh's own order, as its search page lists them.
				ui.Text(c, L("%d skills on skills.sh", len(st.hits))).FontSize(12).FontWeight(500).TextColor(p.Label2).Padding(0, 2, 4, 2)
				ui.Scroll(c.Key("skill-hits")).Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).Gap(2).Children(func() {
						for _, hit := range st.hits {
							installs := L("%@ installs", model.InstallsText(hit.Installs))
							st.row(c, hit.Reference, hit.Name, hit.Owner, installs, hit.URL, hit.Reference, hit.Duplicate)
						}
					})
				})
			case st.status != "":
				tint := p.Label3
				if st.failed {
					tint = p.Red
				}
				ui.Text(c, st.status).FontSize(12).LineHeight(1.45).TextColor(tint).Padding(6, 2)
			case len([]rune(address)) < model.SkillSearchMin:
				st.topics(c)
			}
		})
	})
	if result.Confirmed || result.Cancelled {
		sh.dismiss()
	}
}
