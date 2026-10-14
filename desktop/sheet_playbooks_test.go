package main

import (
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// A DM shows its bot's skills and a group its own, with the ones every bot uses below; a chat with
// none shows the section with a note. A row opens the skill's sheet, which says who can use it.
func TestInspectorSkills(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	m.inspector.tab = tabSkills
	tt := ui.NewTester(m.frame(m.view), 1180, 900)
	settle(tt)
	wantText(t, tt, L("Skills"), "flaky-tests", "release-notes", "ship-checklist", "plain-replies", L("Every bot uses this skill"))
	if tt.HasText(L("Every Bot's Skills")) {
		t.Error("the skills every bot uses are listed with the bot's, not in a section of their own")
	}
	renderBoth(t, tt, "skills-inspector")

	m.selectChat("chat-scout")
	settle(tt)
	// A bot with no skills of its own still lists the ones every bot uses.
	wantText(t, tt, "plain-replies", L("Every bot uses this skill"))
	if tt.HasText("release-notes") {
		t.Error("a bot lists another bot's own skills")
	}
	renderBoth(t, tt, "skills-inspector-shared-only")
	m.selectChat("chat-relay")
	settle(tt)
	wantText(t, tt, "launch-post")
	if tt.HasText("release-notes") {
		t.Error("the group shows a member's own skills")
	}

	m.selectChat("chat-patch")
	settle(tt)
	if err := tt.Click("release-notes"); err != nil {
		t.Fatal(err)
	}
	settleTransitions(tt)
	wantText(t, tt, L("%@ can use it in every chat.", "Developer"), L("Instructions"), L("Delete…"))
	if tt.HasText(L("History")) {
		t.Error("a skill is a folder, with no history to show")
	}
	renderBoth(t, tt, "skills-sheet")
	for i, name := range []string{"references", "scripts"} {
		if err := tt.Click([]string{L("References"), L("Scripts")}[i]); err != nil {
			t.Fatal(err)
		}
		settle(tt)
		renderTo(t, tt, "skills-sheet-"+name)
	}
}

// A new skill saves only once it has a name the CLI takes, a description, and instructions; a name
// typed the way people write it goes as a slug.
func TestSkillSheetNeedsAName(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 900)
	settle(tt)
	s := m.presentPlaybook(model.BotScope("bot-patch"), nil)
	settleTransitions(tt)
	if s.canSave() {
		t.Fatal("an empty skill can be saved")
	}
	s.name, s.description, s.instructions = "Weekly Report", "For the Friday report", "Compare the numbers"
	settle(tt)
	if !s.canSave() || s.content().Name != "weekly-report" {
		t.Fatalf("name %q, can save %v", s.content().Name, s.canSave())
	}
	s.name = "weekly/report!"
	settle(tt)
	wantText(t, tt, L("Use lowercase letters, numbers, and hyphens."))
	renderBoth(t, tt, "skills-new-bad-name")
	if s.canSave() {
		t.Error("a name the CLI refuses can be saved")
	}
	if !safeFileName("checklist.md") || safeFileName("../outside.md") || safeFileName(".env") {
		t.Error("bundled file names")
	}
}

// Save as Skill on a reply offers the messages up to it with the request and the reply picked, and
// opens the draft for review; Save as Standing Instruction needs two corrections.
func TestSaveAsSkill(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 900)
	settle(tt)
	chat := store.Chat("chat-patch")
	reply := chat.Messages[len(chat.Messages)-1]
	s := m.presentPlaybookCapture(chat.ID, reply)
	settleTransitions(tt)
	wantText(t, tt, L("Save as Skill"), L("You"), "Developer")
	if !s.enough() {
		t.Fatal("the reply and its request are not enough")
	}
	renderBoth(t, tt, "skills-capture")
	before := len(store.Skills(model.BotScope("bot-patch")))
	if err := tt.Click(L("Continue")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	wantText(t, tt, L("Writing a draft…"))
	renderTo(t, tt, "skills-capture-drafting-light")
	time.Sleep(1300 * time.Millisecond)
	settleTransitions(tt)
	wantText(t, tt, "relay-deploy", L("A draft. Once you save it, %@ can use it in every chat.", "Developer"))
	renderBoth(t, tt, "skills-captured-draft")
	if after := store.Skills(model.BotScope("bot-patch")); len(after) != before {
		t.Errorf("a draft is not a skill until it is saved: %+v", after)
	}
	if err := tt.Click(L("Cancel")); err != nil {
		t.Fatal(err)
	}
	settleTransitions(tt)
	if after := store.Skills(model.BotScope("bot-patch")); len(after) != before {
		t.Errorf("a cancelled draft left a skill behind: %+v", after)
	}

	m.selectChat("chat-relay")
	settle(tt)
	group := store.Chat("chat-relay")
	var last *model.Message
	for _, message := range group.Messages {
		if message.Author.Kind == model.AuthorYou {
			last = message
		}
	}
	corrections := m.presentPlaybookCapture(group.ID, last)
	settleTransitions(tt)
	wantText(t, tt, L("Save as Standing Instruction"), L("Used by"), L("The bots in %@", "Launch room"))
	if corrections.enough() {
		t.Error("one correction is enough")
	}
	for _, source := range corrections.sources {
		corrections.picked[source.ID] = true
	}
	settle(tt)
	if !corrections.enough() {
		t.Error("two corrections are not enough")
	}
	renderBoth(t, tt, "skills-corrections")
}

// The Skill Store opens from a Skills section's + for that bot: topics to try while the field is
// empty, skills.sh's skills as a topic or the field searches, an address, and Add, which saves the
// skill for the bot at once.
func TestSkillStore(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-scout")
	m.inspector.tab = tabSkills
	tt := ui.NewTester(m.frame(m.view), 1180, 900)
	settle(tt)
	chat := store.Chat("chat-scout")
	scope, _ := model.SkillScope(chat, store.BotsIn(chat))
	bot := store.Bot(scope.ID)

	if err := tt.Click(L("Add a Skill")); err != nil {
		t.Fatal(err)
	}
	settleTransitions(tt)
	wantText(t, tt, L("Skill Store"), L("New Blank Skill…"), "pdf", "react")
	renderBoth(t, tt, "skill-store")

	if err := tt.Click("pdf"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	settle(tt)
	wantText(t, tt, L("%d skills on skills.sh", 1), "anthropics/skills", L("%@ installs", "210.2K"))
	renderBoth(t, tt, "skill-store-results")

	if err := tt.Click(L("Add")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	time.Sleep(750 * time.Millisecond)
	settle(tt)
	skills := store.Skills(scope)
	if len(skills) != 1 || skills[0].Name != "pdf" {
		t.Fatalf("Add saves one skill at once: %+v", skills)
	}
	wantText(t, tt, L("Added"), L("Added “%@” for %@. It's used from the next turn.", "pdf", bot.Name))

	if err := tt.Click(L("Search skills.sh or paste a link")); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Type("zz")
	// The search waits for typing to pause, then the demo answers a moment later.
	time.Sleep(400 * time.Millisecond)
	settle(tt)
	time.Sleep(500 * time.Millisecond)
	settle(tt)
	wantText(t, tt, L("Nothing on skills.sh matches “%@”. Try other words, or paste a link to a skill.", "zz"))

	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Type("anthropics/skills@pdf")
	settle(tt)
	wantText(t, tt, "anthropics/skills@pdf", L("The skill at this address"))
}

// The sidebar's Create › Add a Skill for Every Bot… opens the store for the skills every bot on
// this computer uses, and what it adds is listed with them.
func TestSharedSkillStore(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-scout")
	tt := ui.NewTester(m.frame(m.view), 1180, 900)
	settle(tt)
	m.presentSharedSkillStore()
	settleTransitions(tt)
	wantText(t, tt, L("Skills for %@. They work as soon as you add them, so read one before you rely on it.", L("every bot on this computer")))
	before := len(store.SharedSkills())
	if err := tt.Click("react"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	settle(tt)
	if err := tt.Click(L("Add")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(750 * time.Millisecond)
	settle(tt)
	if shared := store.SharedSkills(); len(shared) != before+1 {
		t.Fatalf("the skill is not one every bot uses: %+v", shared)
	}
}
