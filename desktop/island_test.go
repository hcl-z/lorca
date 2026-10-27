package main

import (
	"testing"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// TestIslandStateOf reads the three states off the turns at work: a coding agent's or a bot's,
// a question or an approval, and nothing at work.
func TestIslandStateOf(t *testing.T) {
	working := model.RunningAgent{Agent: &model.AgentRun{State: model.AgentWorking}}
	asking := model.RunningAgent{Agent: &model.AgentRun{State: model.AgentAsking, Asking: "msg-ask"}}
	approving := model.RunningAgent{Agent: &model.AgentRun{State: model.AgentAsking, Question: &model.AgentQuestion{Kind: "command"}}}
	bot := []string{"bot-1"}
	cases := []struct {
		name        string
		agents      []model.RunningAgent
		workingBots []string
		want        islandState
	}{
		{"no turn", nil, nil, islandIdle},
		{"one agent at work", []model.RunningAgent{working}, nil, islandWorking},
		{"two agents at work", []model.RunningAgent{working, working}, nil, islandWorking},
		{"an agent asks", []model.RunningAgent{asking}, nil, islandAsking},
		{"an agent waits for approval", []model.RunningAgent{approving}, nil, islandAsking},
		{"an asker comes before work", []model.RunningAgent{working, asking}, nil, islandAsking},
		{"a bot is replying", nil, bot, islandWorking},
		{"a bot comes after an agent at work", []model.RunningAgent{working}, bot, islandWorking},
		{"an asker comes before a bot", []model.RunningAgent{asking}, bot, islandAsking},
	}
	for _, c := range cases {
		if got := islandStateOf(c.agents, c.workingBots); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIslandAgentOfPicksTheAgentThatAsks makes the island speak for the agent that needs the
// user, not the first one at work.
func TestIslandAgentOfPicksTheAgentThatAsks(t *testing.T) {
	agents := []model.RunningAgent{
		{Agent: &model.AgentRun{State: model.AgentWorking}},
		{Agent: &model.AgentRun{State: model.AgentAsking, Asking: "msg-ask"}},
	}
	if got := islandAgentOf(agents, islandAsking); got == nil || got.Agent.Asking != "msg-ask" {
		t.Fatalf("the agent that asks: %+v", got)
	}
	if got := islandAgentOf(agents, islandWorking); got != &agents[0] {
		t.Fatalf("the first agent at work: %+v", got)
	}
	if got := islandAgentOf(nil, islandIdle); got != nil {
		t.Fatalf("no agent when the island is idle: %+v", got)
	}
}

// TestIslandApprovalButtonsAnswer draws the island over a permission question the demo's agent
// asks: the three decisions are there, and a click answers through store.AnswerPermission.
func TestIslandApprovalButtonsAnswer(t *testing.T) {
	demoWindow(t)
	chat := store.Chat("chat-patch")
	var id string
	for _, message := range chat.Messages {
		if model.AgentOf(message) != nil {
			id = message.ID
			break
		}
	}
	if id == "" {
		t.Fatal("the demo has no agent card")
	}
	store.Update(id, chat.ID, func(message *model.Message) {
		tool := *message.Body.Tool
		agent := *tool.Agent
		agent.State = model.AgentAsking
		agent.Question = &model.AgentQuestion{Kind: "command", Command: "bun test", HasRule: true}
		tool.Agent = &agent
		message.Body.Tool = &tool
	})

	tt := ui.NewTester((&island{}).view, islandAskingWidth, islandAskingHeight)
	settle(tt)
	for _, text := range []string{L("Allow once"), L("Always allow"), L("Deny")} {
		if !showsLine(tt, text) {
			t.Fatalf("no %q in the island: %q", text, tt.Texts())
		}
	}
	renderTo(t, tt, "island-approval")

	if err := tt.Click(L("Always allow")); err != nil {
		t.Fatal(err)
	}
	settle(tt)

	var agent *model.AgentRun
	for _, message := range store.Chat("chat-patch").Messages {
		if message.ID == id {
			agent = model.AgentOf(message)
			break
		}
	}
	if agent == nil || agent.Question != nil {
		t.Fatalf("the decision did not answer the question: %+v", agent)
	}
}
