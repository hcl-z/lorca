package main

import (
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// TestIslandStateOf reads the four states off the store's projection: a question first, a fresh
// result next, then work, then idle.
func TestIslandStateOf(t *testing.T) {
	working := model.RunningAgent{Agent: &model.AgentRun{State: model.AgentWorking}}
	asking := &islandAsk{Kind: islandAskChoices, ChatID: "chat-1", MessageID: "msg-1"}
	now := time.Now()
	fresh := &islandFinished{ChatID: "chat-1", Line: "Done", At: now}
	stale := &islandFinished{ChatID: "chat-1", Line: "Done", At: now.Add(-islandResultFor)}
	bot := []string{"bot-1"}
	cases := []struct {
		name        string
		ask         *islandAsk
		result      *islandFinished
		agents      []model.RunningAgent
		workingBots []string
		want        islandState
	}{
		{"nothing", nil, nil, nil, nil, islandIdle},
		{"one agent at work", nil, nil, []model.RunningAgent{working}, nil, islandWorking},
		{"a bot is replying", nil, nil, nil, bot, islandWorking},
		{"a question comes first", asking, nil, []model.RunningAgent{working}, bot, islandAsking},
		{"a fresh result comes before work", nil, fresh, []model.RunningAgent{working}, nil, islandResult},
		{"a question comes before a result", asking, fresh, nil, nil, islandAsking},
		{"a stale result is work again", nil, stale, []model.RunningAgent{working}, nil, islandWorking},
		{"a stale result alone is idle", nil, stale, nil, nil, islandIdle},
	}
	for _, c := range cases {
		if got := islandStateOf(c.ask, c.result, c.agents, c.workingBots, now, islandResultFor); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIslandAskOfProjectsTheQuestion makes the island speak for the agent that asks, before a bot's
// question, and read a bot's `ask_user` options by their place.
func TestIslandAskOfProjectsTheQuestion(t *testing.T) {
	demoWindow(t)

	// A coding agent waiting for an approval comes first, with the decisions its card shows.
	chat := store.Chat("chat-patch")
	var agentMessage *model.Message
	for _, message := range chat.Messages {
		if model.AgentOf(message) != nil {
			agentMessage = message
			break
		}
	}
	if agentMessage == nil {
		t.Fatal("the demo has no agent card")
	}
	store.Update(agentMessage.ID, chat.ID, func(message *model.Message) {
		tool := *message.Body.Tool
		agent := *tool.Agent
		agent.State = model.AgentAsking
		agent.Question = &model.AgentQuestion{Kind: "command", Command: "bun test", HasRule: true}
		tool.Agent = &agent
		message.Body.Tool = &tool
	})

	ask := islandAskOf(store)
	if ask == nil || ask.Kind != islandAskPermission {
		t.Fatalf("the agent's permission: %+v", ask)
	}
	if ask.Question != "$ bun test" || ask.Command != "bun test" {
		t.Fatalf("the command as the question: %q %q", ask.Question, ask.Command)
	}
	if len(ask.Decisions) != 3 {
		t.Fatalf("the three decisions of a question with a rule: %+v", ask.Decisions)
	}

	// Answered, the island moves on to the bot's own question.
	store.AnswerPermission(ask.ChatID, ask.MessageID, "allow")

	// The demo's own chat takes a bot's question, as ask_card.go draws one.
	botChat := store.Chats[0]
	last := botChat.Messages[len(botChat.Messages)-1]
	store.Update(last.ID, botChat.ID, func(message *model.Message) {
		message.Author = model.BotAuthor(store.Bots[0].ID)
		message.Body = model.Body{
			Kind: model.BodyPermission,
			Request: &model.PermissionRequest{
				Tool:     "ask",
				Decision: model.DecisionPending,
				Ask: &model.AskQuestion{
					Question: "Ship it?",
					Options:  []model.AskOption{{Label: "Ship now"}, {Label: "Wait"}, {Label: "Draft notes"}},
				},
			},
		}
	})
	ask = islandAskOf(store)
	if ask == nil || ask.Kind != islandAskChoices || len(ask.Options) != 3 || ask.Options[0].Label != "Ship now" {
		t.Fatalf("the bot's question: %+v", ask)
	}

	// A click on an option answers the card through store.AnswerAsk.
	i := &island{}
	tt := ui.NewTester(func(c *ui.Context) { i.draw(c, islandAsking, ask, false, defaultIslandOptions()) }, islandWindowWidth, islandWindowHeight)
	settle(tt)
	if !showsLine(tt, "Ship now") {
		t.Fatalf("no option in the island: %q", tt.Texts())
	}
	renderTo(t, tt, "island-ask")
	if err := tt.Click("Ship now"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	var answer *model.AskAnswer
	for _, message := range store.Chat(botChat.ID).Messages {
		if request := message.Body.Request; request != nil && request.IsAsk() {
			answer = request.Ask.Answer
		}
	}
	if answer == nil || len(answer.Choices) != 1 || answer.Choices[0] != 0 {
		t.Fatalf("the click did not answer the card: %+v", answer)
	}
}

// TestIslandRendersStates draws the folded states, for a look at the block as the window shows it.
func TestIslandRendersStates(t *testing.T) {
	demoWindow(t)
	i := &island{}
	for _, state := range []islandState{islandIdle, islandWorking, islandResult} {
		if state == islandResult {
			i.result = &islandFinished{ChatID: store.Chats[0].ID, Title: L("Atlas"), Line: L("4 files changed, tests pass"), At: time.Now()}
		}
		drawn := state
		tt := ui.NewTester(func(c *ui.Context) { i.draw(c, drawn, nil, false, defaultIslandOptions()) }, islandWindowWidth, islandWindowHeight)
		settle(tt)
		renderTo(t, tt, "island-"+state.String())
	}
	tt := ui.NewTester(func(c *ui.Context) { i.draw(c, islandIdle, nil, true, defaultIslandOptions()) }, islandWindowWidth, islandWindowHeight)
	settle(tt)
	renderTo(t, tt, "island-idle-hover")
}

// TestIslandGallery draws every state the island shows, cropped to the block, for a look at all of
// them side by side.
func TestIslandGallery(t *testing.T) {
	demoWindow(t)
	i := &island{}
	o := defaultIslandOptions()
	permission := &islandAsk{
		Kind:      islandAskPermission,
		BotName:   "Claude Code",
		Command:   "bun test --watch",
		Question:  "$ bun test --watch",
		Decisions: (&model.AgentQuestion{Kind: "command", Command: "bun test --watch", HasRule: true}).Decisions(),
	}
	choices := &islandAsk{
		Kind:     islandAskChoices,
		BotName:  "Atlas",
		Question: "Ship 1.0.31 now, or wait for the phone fix?",
		Options: []model.AskOption{
			{Label: "Ship now", Description: "Tag, build, and upload the draft release"},
			{Label: "Wait for the fix", Description: "Hold the release until the phone fix lands"},
			{Label: "Draft release notes only", Description: "Write the notes and decide later"},
		},
	}
	elsewhere := &islandAsk{Kind: islandAskElsewhere, BotName: "Claude Code", Question: "Which database should the migration use?"}
	states := []struct {
		name    string
		state   islandState
		ask     *islandAsk
		hovered bool
	}{
		{"1-idle", islandIdle, nil, false},
		{"2-working", islandWorking, nil, false},
		{"2b-working-open", islandWorking, nil, true},
		{"3-ask-choices", islandAsking, choices, false},
		{"4-ask-permission", islandAsking, permission, false},
		{"5-ask-elsewhere", islandAsking, elsewhere, false},
		{"6-result", islandResult, nil, false},
	}
	for _, st := range states {
		if st.state == islandResult {
			i.result = &islandFinished{ChatID: store.Chats[0].ID, Title: "Atlas", Line: "4 files changed, tests pass", At: time.Now()}
		}
		width, height := islandBlockSize(st.state, st.ask, st.hovered, o)
		drawn, ask, hovered := st.state, st.ask, st.hovered
		tt := ui.NewTester(func(c *ui.Context) { i.draw(c, drawn, ask, hovered, o) }, width, height)
		settle(tt)
		renderTo(t, tt, "gallery-"+st.name)
	}
}

// TestIslandDefersQuestionsAClickCannotAnswer keeps several picks and the user's own words in the
// chat, since one click cannot say either.
func TestIslandDefersQuestionsAClickCannotAnswer(t *testing.T) {
	demoWindow(t)
	chat := store.Chats[0]
	last := chat.Messages[len(chat.Messages)-1]
	store.Update(last.ID, chat.ID, func(message *model.Message) {
		message.Body = model.Body{
			Kind: model.BodyPermission,
			Request: &model.PermissionRequest{
				Tool:     "ask",
				Decision: model.DecisionPending,
				Ask: &model.AskQuestion{
					Question: "Which checks?",
					Options:  []model.AskOption{{Label: "Tests"}, {Label: "Lint"}},
					Multiple: true,
				},
			},
		}
	})
	if ask := islandAskOf(store); ask == nil || ask.Kind != islandAskElsewhere {
		t.Fatalf("a multi-pick question opens the chat: %+v", ask)
	}

	store.Update(last.ID, chat.ID, func(message *model.Message) {
		message.Body.Request.Ask.Multiple = false
		message.Body.Request.Ask.AllowOther = true
	})
	if ask := islandAskOf(store); ask == nil || ask.Kind != islandAskElsewhere {
		t.Fatalf("a question taking the user's words opens the chat: %+v", ask)
	}
}

// TestIslandSizesHaveRoomForTheirRows holds the block's size to what it draws: a question grows
// with its options, and every state is wide enough for what it says.
func TestIslandSizesHaveRoomForTheirRows(t *testing.T) {
	two := &islandAsk{Kind: islandAskChoices, Options: []model.AskOption{{Label: "a"}, {Label: "b"}}}
	five := &islandAsk{Kind: islandAskChoices, Options: []model.AskOption{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}}}
	o := defaultIslandOptions()
	_, twoHigh := islandBlockSize(islandAsking, two, false, o)
	_, fiveHigh := islandBlockSize(islandAsking, five, false, o)
	if fiveHigh <= twoHigh {
		t.Fatalf("five options %d do not make a taller island than two %d", fiveHigh, twoHigh)
	}
	if w, _ := islandBlockSize(islandAsking, two, false, o); w != islandExpandedWidth {
		t.Fatalf("question width: %d", w)
	}
	if w, h := islandBlockSize(islandIdle, nil, false, o); w != islandIdleWidth || h != islandCompactHeight {
		t.Fatalf("idle size: %dx%d", w, h)
	}
	if w, h := islandBlockSize(islandIdle, nil, true, o); w != islandIdleWidth+islandHoverGrowWidth || h != islandCompactHeight+islandHoverGrowHeight {
		t.Fatalf("hovered idle size: %dx%d", w, h)
	}
}
