package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

func TestTheAgentCardDecodesFromTheCLI(t *testing.T) {
	var wire model.WireMessage
	raw := `{"id":"m1","chat_id":"c1","author":{"kind":"bot","bot_id":"b1"},"state":{"kind":"complete"},"created_at":1,
	 "body":{"kind":"tool","name":"coding_agent","summary":"Started","detail":"","is_running":false,
	  "agent":{"id":"agent-1","kind":"codex","task":"Add dark mode","folder":"~/x/dark","branch":"dark",
	   "state":"asking","question":{"kind":"command","command":"git push","reason":"It publishes.","rule":"push branches"}}}}`
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	message := model.ToMessage(wire)
	agent := model.AgentOf(message)
	if agent == nil || !message.Body.Tool.IsShown() {
		t.Fatalf("no card shown: %+v", message.Body.Tool)
	}
	if agent.Name() != "Codex" || agent.Question == nil || !agent.Question.IsPermission() || len(agent.Question.Decisions()) != 3 {
		t.Errorf("decoded %+v %+v", agent, agent.Question)
	}
	if got := agentTitle(agent, "Developer"); got != "Codex wants to run a command" {
		t.Errorf("title %q", got)
	}

	// An agent over ACP names itself and says how its session is set.
	raw = `{"id":"m2","chat_id":"c1","author":{"kind":"bot","bot_id":"b1"},"state":{"kind":"complete"},"created_at":1,
	 "body":{"kind":"tool","name":"coding_agent","summary":"Started","detail":"","is_running":false,
	  "agent":{"id":"agent-2","kind":"claude-acp","name":"Claude Code","task":"Fix it","state":"idle",
	   "settings":[{"name":"Model","value":"Opus"},{"name":"Context","value":"34k / 200k"}]}}}`
	wire = model.WireMessage{}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	acp := model.AgentOf(model.ToMessage(wire))
	if acp == nil || acp.Name() != "Claude Code" || acp.SettingsLine() != "Model: Opus · Context: 34k / 200k" {
		t.Errorf("decoded %+v", acp)
	}
	if (&model.AgentRun{Kind: "claude"}).SettingsLine() != "" {
		t.Error("an agent that says nothing shows no settings")
	}
}

func TestTheAgentCardSaysHowItStands(t *testing.T) {
	demoWindow(t)
	p := &lightPalette
	agent := &model.AgentRun{Kind: "claude", Task: "Fix it", Branch: "fix-it", State: model.AgentWorking, Device: "Workbench", Tone: model.AgentToneBusy, Busy: true, StatusKey: "working"}
	if got := agentPlace(agent); got != "fix-it · Workbench" {
		t.Errorf("place %q", got)
	}
	if tint, words := agentLook(p, agent); tint != p.Accent || words != Lc("Working", "coding agent") || !agentBusy(agent) {
		t.Errorf("working %v %q", tint, words)
	}
	agent.Asking, agent.Tone, agent.Busy = "msg-ask", model.AgentToneAsk, false
	if tint, words := agentLook(p, agent); tint != p.Orange || words != Lc("Needs you", "coding agent") || agentBusy(agent) {
		t.Errorf("asking %v %q", tint, words)
	}
	agent.Asking, agent.State, agent.Tone, agent.Outcome = "", model.AgentFailed, model.AgentToneFail, "Claude Code exited with code 1"
	if tint, _ := agentLook(p, agent); tint != p.Red {
		t.Errorf("failed %v", tint)
	}
	agent.State = model.AgentIdle
	if agent.IsRunning() {
		t.Error("a finished agent offers no Stop")
	}
	agent.State, agent.Question = model.AgentAsking, &model.AgentQuestion{Kind: "start"}
	if got := agentTitle(agent, "Developer"); got != "Developer wants to start Claude Code on Workbench" {
		t.Errorf("start %q", got)
	}
	if agent.IsRunning() || agent.Started() {
		t.Error("nothing to stop or read before it starts")
	}
}

// The facts the card reads from what the agent says of its session and its work.
func TestTheAgentCardReadsItsContextItsStepAndItsPullRequest(t *testing.T) {
	agent := &model.AgentRun{Settings: []model.AgentSetting{{Name: "Context", Value: "150k / 200k"}}, Context: &model.AgentContextUse{Used: 150_000, Size: 200_000}, Now: "Bash(bun test)"}
	if share, words, ok := agentContext(agent); !ok || share != 0.75 || words != "150k / 200k" {
		t.Errorf("context %v %q %v", share, words, ok)
	}
	agent.Context = &model.AgentContextUse{Used: 1_500_000, Size: 1_000_000}
	if share, _, _ := agentContext(agent); share != 1 {
		t.Errorf("a context past its size is full, not %v", share)
	}
	if _, _, ok := agentContext(&model.AgentRun{}); ok {
		t.Error("an agent that says nothing of its context has no meter")
	}
	if got := agentNow(agent); got != "Bash(bun test)" {
		t.Errorf("now %q", got)
	}
	if got := pullRequestName(&model.AgentRun{PullRequestName: "acme/shop #7"}); got != "acme/shop #7" {
		t.Errorf("pull request %q", got)
	}
	if got := pullRequestName(&model.AgentRun{PullRequest: "https://example.com/x"}); got != L("Pull request") {
		t.Errorf("a link the CLI does not name %q", got)
	}
}

// TestRenderAgentCard draws the developer's chat with its agent's card in each state, and the
// transcript it opens.
func TestRenderAgentCard(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	chat := store.Chat("chat-patch")
	var rowID string
	var base model.AgentRun
	for _, message := range chat.Messages {
		if agent := model.AgentOf(message); agent != nil {
			rowID, base = message.ID, *agent
		}
	}
	if rowID == "" {
		t.Fatal("the demo has no agent card")
	}
	set := func(change func(agent *model.AgentRun)) {
		store.Update(rowID, "chat-patch", func(message *model.Message) {
			tool := *message.Body.Tool
			agent := base
			change(&agent)
			tool.Agent = &agent
			message.Body.Tool = &tool
		})
	}
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	states := []struct {
		name   string
		change func(agent *model.AgentRun)
	}{
		{"working", func(agent *model.AgentRun) {}},
		{"start", func(agent *model.AgentRun) {
			agent.State = model.AgentAsking
			agent.Question = &model.AgentQuestion{Kind: "start", Reason: "Claude Code edits files and runs commands on Workbench.", Rule: "start coding agents in the site repository", HasRule: true}
		}},
		{"command", func(agent *model.AgentRun) {
			agent.State = model.AgentAsking
			agent.Question = &model.AgentQuestion{Kind: "command", Command: "git push origin fix-docs-link", Reason: "Pushing sends the branch to GitHub, where others can see it.", Rule: "push branches to GitHub", HasRule: true}
		}},
		{"asks-in-the-chat", func(agent *model.AgentRun) {
			agent.State, agent.Asking = model.AgentAsking, "msg-agent-ask"
		}},
		{"done", func(agent *model.AgentRun) {
			agent.State, agent.StatusKey, agent.Tone, agent.Busy, agent.Tail = model.AgentIdle, "done", model.AgentToneDone, false, nil
			agent.PullRequest, agent.PullRequestName = "https://github.com/acme/site/pull/412", "acme/site #412"
		}},
		{"failed", func(agent *model.AgentRun) {
			agent.State, agent.StatusKey, agent.Tone, agent.Busy, agent.Tail = model.AgentFailed, "failed", model.AgentToneFail, false, nil
			agent.Outcome = "Claude Code exited with code 1"
		}},
	}
	for _, state := range states {
		set(state.change)
		settle(tt)
		renderBoth(t, tt, "agent-card-"+state.name)
		if state.name == "asks-in-the-chat" && !showsLine(tt, L("Asks you in the chat")) {
			t.Errorf("the card does not point to the question: %q", tt.Texts())
		}
	}
	set(func(agent *model.AgentRun) {})
	settle(tt)
	if !tt.HasText(base.Task) {
		t.Fatalf("no task in %q", tt.Texts())
	}
	if err := tt.Click(base.Task); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	renderBoth(t, tt, "agent-transcript")
	// What it was sent and said is text, its reads and searches fold into one line, each tool is
	// a line in the past or present tense, and the plan a checklist.
	for _, want := range []string{"Fix the broken docs link on the download page, with a test.", "I'll find the link first.", "Explored 1 search", "Edited src/pages/download.astro", "Failed: bun test links", "Run the link test"} {
		if !tt.HasText(want) {
			t.Errorf("the transcript has no %q: %q", want, tt.Texts())
		}
	}
	// The last command's output is on a terminal, whose cells are not text elements; where the
	// plugin's library cannot load, it is text.
	if screen := newAgentScreen(11); screen != nil {
		screen.close()
		if !tt.HasText("Terminal") {
			t.Errorf("the open output has no terminal: %q", tt.Texts())
		}
	} else if !tt.HasText("14 pass") {
		t.Errorf("the open output is not shown: %q", tt.Texts())
	}
	// An edit that ended well is closed, and a click on its line opens its diff.
	if tt.HasText(`+    Read the <a href="/docs/install">install guide</a>.`) {
		t.Error("an edit that is not the last tool is open")
	}
	// The edit is above the latest entries: scroll up to it.
	for range 6 {
		tt.Scroll(590, 380, 0, -400)
		settle(tt)
	}
	if err := tt.Click("Edited src/pages/download.astro"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !tt.HasText(`+    Read the <a href="/docs/install">install guide</a>.`) {
		t.Errorf("the edit did not open to its diff: %q", tt.Texts())
	}
	renderBoth(t, tt, "agent-transcript-diff")
}

func TestPagesOfATranscriptMergeInOrder(t *testing.T) {
	have, _ := model.MergeAgentEntries(nil, model.AgentTranscriptPage{Entries: []model.AgentEntry{{Seq: 0, Text: "a"}, {Seq: 1, Text: "b"}}})
	have, changed := model.MergeAgentEntries(have, model.AgentTranscriptPage{Entries: []model.AgentEntry{{Seq: 1, Text: "b2"}, {Seq: 3, Text: "d"}}})
	if !changed || len(have) != 3 || have[1].Text != "b2" || have[2].Seq != 3 {
		t.Errorf("merged %+v", have)
	}
	have, _ = model.MergeAgentEntries(have, model.AgentTranscriptPage{Entries: []model.AgentEntry{{Seq: 2, Text: "c"}}, First: 1})
	if len(have) != 3 || have[0].Seq != 1 || have[1].Text != "c" {
		t.Errorf("an entry the Runner no longer keeps stays, or one between lands out of order: %+v", have)
	}
	if _, changed := model.MergeAgentEntries(have, model.AgentTranscriptPage{First: 1}); changed {
		t.Error("a page with nothing new changed the transcript")
	}
}

// TestTheCardOffersAWorkingAgentsModelAndEffort draws the card of an agent that offers to change
// its model and how hard it thinks, as pop-ups of its own, and keeps them off while it asks.
func TestTheCardOffersAWorkingAgentsModelAndEffort(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	var rowID string
	for _, message := range store.Chat("chat-patch").Messages {
		if model.AgentOf(message) != nil {
			rowID = message.ID
		}
	}
	if rowID == "" {
		t.Fatal("the demo has no agent card")
	}
	set := func(change func(agent *model.AgentRun)) {
		store.Update(rowID, "chat-patch", func(message *model.Message) {
			tool := *message.Body.Tool
			agent := *tool.Agent
			change(&agent)
			tool.Agent = &agent
			message.Body.Tool = &tool
		})
	}
	set(func(agent *model.AgentRun) {
		agent.Settings = []model.AgentSetting{{Name: "Model", Value: "Opus"}, {Name: "Effort", Value: "High"}, {Name: "Context", Value: "34k / 200k"}}
		agent.Context = &model.AgentContextUse{Used: 34_000, Size: 200_000}
		agent.Controls = []model.AgentControl{
			{What: "model", Name: "Model", Value: "opus", Choices: []model.AgentChoice{{Value: "sonnet", Name: "Sonnet"}, {Value: "opus", Name: "Opus"}}},
			{What: "effort", Name: "Effort", Value: "high", Choices: []model.AgentChoice{{Value: "low", Name: "Low"}, {Value: "high", Name: "High"}}},
		}
	})
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	for _, want := range []string{"Model", "Opus", "Effort", "High"} {
		if !showsLine(tt, want) {
			t.Errorf("the card has no %q: %q", want, tt.Texts())
		}
	}
	// What the pop-ups already show is not said twice; the rest of the settings still is.
	if !showsLine(tt, "34k / 200k · 17%") || tt.HasText("Model: Opus") {
		t.Errorf("the settings line repeats the pop-ups: %q", tt.Texts())
	}
	renderBoth(t, tt, "agent-controls")

	set(func(agent *model.AgentRun) {
		agent.State = model.AgentAsking
		agent.Question = &model.AgentQuestion{Kind: "command", Command: "git push origin main"}
		agent.Controls = []model.AgentControl{{What: "model", Name: "Model", Value: "opus", Choices: []model.AgentChoice{{Value: "opus", Name: "Opus"}, {Value: "sonnet", Name: "Sonnet"}}}}
	})
	settle(tt)
	if showsLine(tt, "Opus") {
		t.Errorf("a card that asks offers its model: %q", tt.Texts())
	}
}

func TestAnAgentScreenAddsWhatTheOutputAdds(t *testing.T) {
	screen := newAgentScreen(11)
	if screen == nil {
		t.Skip("the terminal plugin's library is not available")
	}
	defer screen.close()
	screen.show("one\n")
	screen.show("one\ntwo\n")
	if got := screen.term.Text(); !strings.Contains(got, "one") || !strings.Contains(got, "two") || strings.Count(got, "one") != 1 {
		t.Errorf("the screen shows %q", got)
	}
	screen.show("other")
	if got := screen.term.Text(); strings.Contains(got, "one") || !strings.Contains(got, "other") {
		t.Errorf("other output replaces what was there: %q", got)
	}
}

// What an agent's command printed never drives the terminal: its colors pass, and no other escape,
// bell, or C1 control gets through, so no clipboard write, link, or title can.
func TestAnAgentScreenKeepsOnlyColors(t *testing.T) {
	got := string(terminalBytes("a\x1b]52;c;ZXZpbA==\x07b\tc\r\nd\u009be\x1b[1;31mred\x1b[0m\x1b[2J\x1b]8;;https://x\x1b\\link\nend"))
	if want := "ab\tc\r\nde\x1b[1;31mred\x1b[0mlink\r\nend"; got != want {
		t.Errorf("the screen gets %q, not %q", got, want)
	}
	if got := outputPlain("\x1b[32m50%\r100%\x1b[0m\r\nnext\r\n"); got != "100%\nnext" {
		t.Errorf("plain output %q", got)
	}
}
