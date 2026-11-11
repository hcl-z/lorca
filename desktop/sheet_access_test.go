package main

import (
	"testing"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

func accessSheet(t *testing.T, botID string) (*mainWindow, *botAccessSheet, *ui.Tester) {
	t.Helper()
	m := demoWindow(t)
	m.userWantsInspector = false
	st := m.presentBotAccess(botID)
	tt := ui.NewTester(m.frame(m.view), 1080, 900)
	runPosts()
	settleTransitions(tt)
	return m, st, tt
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	settle(tt)
}

func choose(t *testing.T, tt *ui.Tester, popUp, item string) {
	t.Helper()
	if err := tt.Click(popUp); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(item); err != nil {
		t.Fatal(err)
	}
	settle(tt)
}

func TestAccessSheetSavesLevelsAndTools(t *testing.T) {
	m, _, tt := accessSheet(t, "bot-nova")
	click(t, tt, L("%@ tools", "GitHub"))
	if !tt.HasText("Merge a pull request") || !tt.HasText(L("All tools")) {
		t.Fatal("GitHub's tools did not show")
	}
	// A tool's choice: on, ask first, or off, as the three symbols at its row's end. The first
	// "Off" is the first row's, which is Search issues.
	offIcon, _ := tt.Find(L("Off"))
	tt.ClickAt(offIcon.X+offIcon.W/2, offIcon.Y+offIcon.H/2)
	settle(tt)
	if !tt.HasText(L("%d of %d tools", 4, 5)) {
		t.Fatal("the tools summary did not follow the choice")
	}
	// The plugin's own switch.
	linear, _ := tt.Find(L("Access to %@", "Linear"))
	tt.ClickAt(linear.X+linear.W-18, linear.Y+linear.H/2)
	settle(tt)
	click(t, tt, L("Save"))
	if m.hasSheet() {
		t.Fatal("Save did not close the sheet")
	}
	policy := store.Bot("bot-nova").Permissions
	if policy == nil || policy.Connections == nil {
		t.Fatalf("saved policy: %+v", policy)
	}
	grants := *policy.Connections
	if _, ok := grants["linear"]; ok || policy.Level("deepwiki") != model.AccessOn || policy.Level("filesystem") != model.AccessOn {
		t.Fatalf("plugin levels: %+v", grants)
	}
	if policy.ToolLevel("github", "search_issues") != model.AccessNone {
		t.Fatalf("a tool switched off: %+v", policy.ToolLevel("github", "search_issues"))
	}
	// A tool left alone keeps its plugin's switch.
	if policy.ToolLevel("github", "create_issue") != model.AccessOn {
		t.Fatal("a tool left alone keeps its plugin's switch")
	}
}

func TestAccessSheetLeavesFullAccessOpenToNewPlugins(t *testing.T) {
	_, st, tt := accessSheet(t, "bot-nova")
	click(t, tt, L("%@ tools", "GitHub"))
	click(t, tt, "Merge a pull request")
	click(t, tt, "Merge a pull request")
	if policy := st.policy(); policy.Connections != nil {
		t.Fatalf("every plugin open should stay every plugin: %+v", policy)
	}
	click(t, tt, L("Save"))
	if store.Bot("bot-nova").Permissions != nil {
		t.Fatal("an unchanged sheet wrote a policy")
	}
}

func TestAccessSheetKeepsChoicesWhenTheToolsArrive(t *testing.T) {
	m := demoWindow(t)
	st := m.presentBotAccess("bot-quill")
	if len(st.plugins) == 0 || len(st.plugins[0].Tools) != 0 {
		t.Fatal("the sheet opens on the Runner's own list")
	}
	st.levels["linear"] = model.AccessAsk
	runPosts()
	if st.levels["linear"] != model.AccessAsk || len(st.plugins[0].Tools) == 0 {
		t.Fatal("the tools replaced what the user chose")
	}
	closed := m.presentBotAccess("bot-quill")
	m.sheets[len(m.sheets)-1].dismiss()
	runPosts()
	if !closed.closed || len(closed.plugins[0].Tools) != 0 {
		t.Fatal("a closed sheet took the tools")
	}
}

func TestAccessRequestOpensTheSheetOrIsDismissed(t *testing.T) {
	m := demoWindow(t)
	m.userWantsInspector = false
	m.selectChat("chat-quill")
	tt := ui.NewTester(m.frame(m.view), 1080, 880)
	settleTransitions(tt)
	if tt.HasText(L("Always allow")) || tt.HasText(L("Allow once")) || !tt.HasText(L("Not allowed in this bot's Access settings.")) {
		t.Fatal("the access request offers to allow the call")
	}
	click(t, tt, L("Edit Access…"))
	settleTransitions(tt)
	if !m.hasSheet() || !tt.HasText(L("Access for %@", "Writer")) {
		t.Fatal("Edit Access did not open the sheet")
	}
	click(t, tt, L("Cancel"))
	settleTransitions(tt)
	click(t, tt, L("Dismiss"))
	for _, message := range store.Chat("chat-quill").Messages {
		if request := message.Body.Request; request != nil && request.IsAccess() && request.Decision != model.DecisionDismissed {
			t.Fatal("Dismiss did not dismiss the request")
		}
	}
}

func TestRenderDesktopAccess(t *testing.T) {
	m := demoWindow(t)
	writer := store.Bot("bot-quill")
	inspector := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Padding(16).Gap(20).Children(func() {
			m.inspectorProfile(c, writer)
			m.inspectorPlugins(c, writer)
		})
	}, 300, 520)
	inspector.SetScale(2)
	settle(inspector)
	if !inspector.HasText(L("Limited")) || !inspector.HasText(L("No access")) {
		t.Fatal("the inspector does not show the Writer's Access")
	}
	renderBoth(t, inspector, "access-inspector")

	m.userWantsInspector = false
	m.selectChat("chat-quill")
	chat := ui.NewTester(m.frame(m.view), 1000, 620)
	chat.SetScale(2)
	settleTransitions(chat)
	renderBoth(t, chat, "access-card")

	m.presentBotAccess("bot-quill")
	runPosts()
	sheet := ui.NewTester(m.frame(m.view), 1000, 760)
	sheet.SetScale(2)
	settleTransitions(sheet)
	renderBoth(t, sheet, "access-sheet")
	click(t, sheet, L("%@ tools", "GitHub"))
	sheet.Move(2, 2)
	renderBoth(t, sheet, "access-sheet-tools")
}
