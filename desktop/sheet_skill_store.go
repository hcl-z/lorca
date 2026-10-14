package main

import (
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// The Skill Store, opened from a bot's Skills section: ClawHub's skills for that bot (or the
// group), as the marketplace lists plugins. The field searches as it is typed, or takes a link to a
// skill on ClawHub, skills.sh, or GitHub. Add saves the skill, and the bot uses it from its next
// turn.

type skillStoreState struct {
	w     *appWindow
	scope model.PlaybookScope
	// owner is whose skills these are: the bot's name, or the group's.
	owner string
	query string
	hits  []model.SkillHit
	// adding and added are the references being added and added in this sheet.
	adding, added  map[string]bool
	loading        bool
	status, notice string
	failed, closed bool
	generation     int
	timer          *time.Timer
}

func (w *appWindow) presentSkillStore(scope model.PlaybookScope) {
	st := &skillStoreState{w: w, scope: scope, adding: map[string]bool{}, added: map[string]bool{}}
	if scope.IsGroup() {
		if chat := store.Chat(scope.ID); chat != nil {
			st.owner = store.Title(chat)
		}
	} else if bot := store.Bot(scope.ID); bot != nil {
		st.owner = bot.Name
	}
	w.present(st.view, func() {
		st.closed = true
		if st.timer != nil {
			st.timer.Stop()
		}
	})
	st.search(st.generation)
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

// has is whether the skill is already the scope's: added here, or saved under the name ClawHub
// gives it.
func (st *skillStoreState) has(hit model.SkillHit) bool {
	if st.added[hit.Reference] {
		return true
	}
	slug := model.ReferenceName(hit.Reference)
	for _, skill := range store.Skills(st.scope) {
		if !skill.IsDraft() && skill.Name == slug {
			return true
		}
	}
	return false
}

// queryChanged searches a moment after the last keystroke.
func (st *skillStoreState) queryChanged() {
	st.generation++
	generation := st.generation
	if st.timer != nil {
		st.timer.Stop()
	}
	if isSkillAddress(strings.TrimSpace(st.query)) {
		st.loading, st.status, st.failed = false, "", false
		return
	}
	st.timer = store.After(350*time.Millisecond, func() {
		if !st.closed && generation == st.generation {
			st.search(generation)
		}
	})
}

func (st *skillStoreState) search(generation int) {
	st.loading, st.status, st.failed = true, "", false
	store.SearchSkills(strings.TrimSpace(st.query), func(hits []model.SkillHit, err error) {
		if st.closed || generation != st.generation {
			return
		}
		st.loading = false
		if err != nil {
			st.hits, st.status, st.failed = nil, model.ErrorText(err), true
			return
		}
		st.hits = hits
		if len(hits) == 0 {
			st.status = L("No skills found. Paste a link to a skill instead.")
		}
	})
}

// add saves the skill for the scope, at once.
func (st *skillStoreState) add(reference string) {
	if st.adding[reference] {
		return
	}
	st.adding[reference], st.notice, st.failed = true, "", false
	store.ImportSkill(st.scope, reference, true, func(skill model.PlaybookRecord, skipped []string, err error) {
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
		st.notice = L("Added “%@”. %@ uses it from now on.", name, st.owner)
		if len(skipped) > 0 {
			st.notice += " " + L("%d files didn't come across: only the instructions and the text files in references and scripts do.", len(skipped))
		}
	})
}

// row is one skill: Add, or Added when the scope has it already.
func (st *skillStoreState) row(c *ui.Context, key, title, byline, subtitle, link, reference string, has bool) {
	media := func() { marketPluginIcon(c, "", "book.closed", 40) }
	accessory := func() {
		p := colors(c)
		switch {
		case st.adding[reference]:
			spinner(c, 14).Label(L("Adding %@", title))
		case st.added[reference] || has:
			ui.Row(c).Gap(5).Children(func() {
				symbol(c, "checkmark", 12, 2.6).TextColor(p.Green)
				ui.Text(c, L("Added")).FontSize(12.5).TextColor(p.Label2).SingleLine()
			})
		default:
			if pushButton(c, L("Add"), pushOptions{Tooltip: L("Add %@ to %@", title, st.owner)}).Clicked() {
				st.add(reference)
			}
		}
	}
	if marketRow(c, key, marketRowOptions{Title: title, Byline: byline, Subtitle: subtitle}, media, accessory) && link != "" {
		openExternal(link)
	}
}

// rows lists the skills one under another.
func (st *skillStoreState) rows(c *ui.Context, hits []model.SkillHit) {
	ui.Column(c).Gap(2).Children(func() {
		for _, hit := range hits {
			byline := ""
			if hit.Owner != "" {
				byline = L("by %@", hit.Owner)
			}
			st.row(c, hit.Reference, hit.Name, byline, hit.Summary, hit.URL, hit.Reference, st.has(hit))
		}
	})
}

func (st *skillStoreState) view(c *ui.Context, sh *sheet) {
	p := colors(c)
	var leading func()
	if st.loading {
		leading = func() { spinner(c, 14) }
	}
	address := strings.TrimSpace(st.query)
	result := sheetFrame(c, sheetOptions{
		Title:    L("Skill Store"),
		Subtitle: L("Skills from ClawHub for %@. They work as soon as you add them.", st.owner),
		Width:    640,
		Confirm:  L("Done"),
		NoCancel: true,
		Leading:  leading,
	}, func() {
		textField(c.Key("skill-query"), &st.query, fieldOptions{Placeholder: L("Search skills or paste a link"), Label: L("Search skills or paste a link"), AutoFocus: true}).
			OnChange(st.queryChanged)
		if st.notice != "" {
			tint := p.Label2
			if st.failed {
				tint = p.Red
			}
			ui.Text(c, st.notice).FontSize(11.5).LineHeight(1.4).TextColor(tint)
		}
		switch {
		case isSkillAddress(address):
			ui.Column(c).Gap(2).Children(func() {
				st.row(c, "skill-address", address, "", L("A skill at this address"), "", address, false)
			})
		case len(st.hits) > 0:
			title := L("Popular on ClawHub")
			if address != "" {
				title = L("Results")
			}
			ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(p.Label2).Padding(0, 2)
			ui.Scroll(c.Key("skill-hits")).MaxHeight(380).Children(func() { st.rows(c, st.hits) })
		}
		if st.status != "" {
			tint := p.Label3
			if st.failed {
				tint = p.Red
			}
			ui.Text(c, st.status).FontSize(11.5).LineHeight(1.4).TextColor(tint)
		}
	})
	if result.Confirmed || result.Cancelled {
		sh.dismiss()
	}
}
