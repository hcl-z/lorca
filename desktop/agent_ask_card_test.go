package main

import (
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// agentAsks puts a coding agent's question at the end of the Developer's chat.
func agentAsks(id string, ask *model.AgentAsk) {
	ask.Kind, ask.Name, ask.RowID = "claude", "Claude Code", "msg-agent-row"
	store.Append(&model.Message{
		ID:        id,
		Author:    model.BotAuthor("bot-patch"),
		Body:      model.Body{Kind: model.BodyPermission, Request: &model.PermissionRequest{Tool: "agent_ask", PluginName: "Claude Code", Summary: "Claude Code asks: " + ask.Message, Decision: model.DecisionPending, AgentAsk: ask}},
		State:     model.MessageState{Kind: model.StateComplete},
		CreatedAt: time.Now(),
	}, "chat-patch")
}

// A question that asks for one pick sends on its click, and the card says who answered and what.
func TestAnAgentsQuestionSendsOnePickAndSaysWhatWasAnswered(t *testing.T) {
	m := demoWindow(t)
	agentAsks("msg-agent-ask", &model.AgentAsk{Message: "Which database should the tests use?", Fields: []model.AgentAskField{{
		Key: "question_0", Label: "Database", Kind: "one", Other: true,
		Choices: []model.AgentAskChoice{{Value: "pg", Title: "Postgres", Description: "A real server, as in production"}, {Value: "lite", Title: "SQLite"}},
	}}})
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	for _, text := range []string{L("%@ asks", "Claude Code"), "Which database should the tests use?", "Database", "Postgres", "A real server, as in production", L("Other"), L("Decline"), L("Waiting for your answer")} {
		if !showsLine(tt, text) {
			t.Fatalf("no %q in %q", text, tt.Texts())
		}
	}
	if tt.HasText(L("Submit")) {
		t.Error("one pick needs no Submit")
	}
	renderBoth(t, tt, "agent-ask-card")
	if err := tt.Click("SQLite"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	request := askRequest("msg-agent-ask")
	if request.Decision != model.DecisionAllowed || request.AgentAsk.AnswersText() != "Database: SQLite" || request.AgentAsk.By != "you" {
		t.Fatalf("answered %+v, %q", request.AgentAsk, request.Decision)
	}
	if !showsLine(tt, "SQLite") || tt.HasText(L("Decline")) || tt.HasText(L("Waiting for your answer")) {
		t.Fatalf("the answered card: %q", tt.Texts())
	}
	renderBoth(t, tt, "agent-ask-card-answered")
}

// A form of several fields sends with Submit once each required field has an answer; Decline
// leaves the agent to go on without one; a question the bot answered says so.
func TestAnAgentsFormSendsWithSubmitAndMayBeDeclined(t *testing.T) {
	m := demoWindow(t)
	agentAsks("msg-agent-form", &model.AgentAsk{Message: "How should I set up the release?", Fields: []model.AgentAskField{
		{Key: "targets", Label: "Targets", Kind: "many", Required: true, Choices: []model.AgentAskChoice{{Value: "mac", Title: "macOS"}, {Value: "win", Title: "Windows"}, {Value: "linux", Title: "Linux"}}},
		{Key: "notes", Label: "Release notes", Kind: "text"},
		{Key: "draft", Label: "Draft first", Kind: "flag", Required: true},
	}})
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 900)
	settle(tt)
	if !showsLine(tt, L("%@ (optional)", "Release notes")) {
		t.Fatalf("an optional field is not marked: %q", tt.Texts())
	}
	if err := tt.Click(L("Submit")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !askRequest("msg-agent-form").IsPending() {
		t.Fatal("Submit sent a form missing what it requires")
	}
	for _, label := range []string{"macOS", "Linux", L("Yes")} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		settle(tt)
	}
	if !askRequest("msg-agent-form").IsPending() {
		t.Fatal("a pick sent the form before Submit")
	}
	renderBoth(t, tt, "agent-ask-form")
	if err := tt.Click(L("Submit")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if got := askRequest("msg-agent-form").AgentAsk.AnswersText(); got != "Targets: macOS, Linux · Draft first: "+L("Yes") {
		t.Fatalf("answered %q", got)
	}

	agentAsks("msg-agent-decline", &model.AgentAsk{Message: "Delete the old builds?", Fields: []model.AgentAskField{{Key: "ok", Label: "Delete the old builds?", Kind: "flag", Required: true}}})
	settle(tt)
	if err := tt.Click(L("Decline")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if got := askRequest("msg-agent-decline").Decision; got != model.DecisionDenied || !showsLine(tt, L("You declined it")) {
		t.Fatalf("declined: %q, %q", got, tt.Texts())
	}

	agentAsks("msg-agent-bot", &model.AgentAsk{Message: "Which branch?", Fields: []model.AgentAskField{{Key: "b", Label: "Branch", Kind: "text"}}})
	store.Update("msg-agent-bot", "chat-patch", func(message *model.Message) {
		request := *message.Body.Request
		ask := *request.AgentAsk
		ask.Answers, ask.By = []model.AgentSetting{{Name: "Branch", Value: "release/1.5"}}, "bot"
		request.AgentAsk, request.Decision = &ask, model.DecisionAllowed
		message.Body.Request = &request
	})
	settle(tt)
	if !showsLine(tt, "release/1.5") || !tt.HasText(L("Answered by %@", "Developer")) {
		t.Fatalf("the bot's answer: %q", tt.Texts())
	}
	renderBoth(t, tt, "agent-ask-answered-by-bot")
}
