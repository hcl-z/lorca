package main

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// askQuestion puts a bot's question at the end of the Developer's chat.
func askQuestion(id string, ask *model.AskQuestion) {
	store.Append(&model.Message{
		ID:        id,
		Author:    model.BotAuthor("bot-patch"),
		Body:      model.Body{Kind: model.BodyPermission, Request: &model.PermissionRequest{Tool: "ask", Summary: ask.Question, Decision: model.DecisionPending, Ask: ask}},
		State:     model.MessageState{Kind: model.StateComplete},
		CreatedAt: time.Now(),
	}, "chat-patch")
}

func askRequest(id string) *model.PermissionRequest {
	for _, message := range store.Chat("chat-patch").Messages {
		if message.ID == id {
			return message.Body.Request
		}
	}
	return nil
}

// One pick sends at once and the card says what was picked; the recommendation is marked.
func TestAskCardSendsOnePick(t *testing.T) {
	m := demoWindow(t)
	askQuestion("msg-question", &model.AskQuestion{
		Question:    "Which branch should I release from?",
		Options:     []model.AskOption{{Label: "main", Description: "What is on the site now"}, {Label: "next", Description: "With the new pairing flow"}},
		Recommended: 1,
	})
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	for _, text := range []string{"Which branch should I release from?", "Pick one", "With the new pairing flow", "Recommended", "Waiting for your answer"} {
		if !tt.HasText(text) {
			t.Fatalf("no %q in %q", text, tt.Texts())
		}
	}
	if tt.HasText("Submit") {
		t.Error("one pick needs no Submit")
	}
	renderBoth(t, tt, "ask-card")
	if err := tt.Click("next"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	request := askRequest("msg-question")
	if request.IsPending() || request.Ask.Answer == nil || !slices.Equal(request.Ask.Answer.Choices, []int{1}) {
		t.Fatalf("answer %+v, decision %q", request.Ask.Answer, request.Decision)
	}
	if !tt.HasText("You chose next") || tt.HasText("Pick one") {
		t.Fatalf("the answered card: %q", tt.Texts())
	}
	renderBoth(t, tt, "ask-card-answered")
}

// Several picks and the user's own words go together with Submit, which waits for one of them;
// Skip leaves the choice to the bot.
func TestAskCardSeveralAndOther(t *testing.T) {
	m := demoWindow(t)
	askQuestion("msg-several", &model.AskQuestion{
		Question:    "What should the release notes cover?",
		Options:     []model.AskOption{{Label: "Pairing"}, {Label: "Routines"}, {Label: "Channels"}},
		Multiple:    true,
		AllowOther:  true,
		Recommended: -1,
	})
	m.selectChat("chat-patch")
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	if !tt.HasText("Pick any that apply") || tt.HasText("Recommended") {
		t.Fatalf("hint or mark: %q", tt.Texts())
	}
	if err := tt.Click("Submit"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !askRequest("msg-several").IsPending() {
		t.Fatal("Submit sent nothing picked")
	}
	for _, label := range []string{"Pairing", "Channels", "Other"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		settle(tt)
	}
	if !askRequest("msg-several").IsPending() {
		t.Fatal("a pick of several sent before Submit")
	}
	tt.Type("Thanks to the testers")
	settle(tt)
	renderBoth(t, tt, "ask-card-several")
	if err := tt.Click("Submit"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	answer := askRequest("msg-several").Ask.Answer
	if answer == nil || !slices.Equal(answer.Choices, []int{0, 2}) || answer.Other != "Thanks to the testers" {
		t.Fatalf("answer %+v", answer)
	}
	if !tt.HasText("You chose Pairing, Channels, “Thanks to the testers”") {
		t.Fatalf("the answered card: %q", tt.Texts())
	}

	askQuestion("msg-skip", &model.AskQuestion{Question: "Ship today?", Options: []model.AskOption{{Label: "Yes"}, {Label: "Tomorrow"}}, Recommended: -1})
	settle(tt)
	if err := tt.Click("Skip"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if got := askRequest("msg-skip").Decision; got != model.DecisionDenied || !tt.HasText("Skipped") {
		t.Fatalf("skip: %q, %q", got, tt.Texts())
	}
}
