package main

import (
	"testing"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// lastExchange puts a plain exchange at the end of the Developer's chat: the user's message and
// the bot's reply, which the toolbar can run again.
func lastExchange() (asked, reply *model.Message) {
	asked = &model.Message{ID: "msg-asked", Author: model.You, Body: model.Body{Kind: model.BodyText, Text: "Summarize the pull request."},
		State: model.MessageState{Kind: model.StateComplete}, CreatedAt: time.Now().Add(-time.Minute)}
	reply = &model.Message{ID: "msg-reply", Author: model.BotAuthor("bot-patch"), Body: model.Body{Kind: model.BodyText, Text: "It fixes the broken link in the guide."},
		State: model.MessageState{Kind: model.StateComplete}, CreatedAt: time.Now()}
	store.Append(asked, "chat-patch")
	store.Append(reply, "chat-patch")
	return asked, reply
}

func hoverText(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no %q in %q", text, tt.Texts())
	}
	tt.Move(r.X+10, r.Y+4)
	settle(tt)
}

// Regenerate shows on the bot's latest reply alone, and takes the reply out to be answered anew.
func TestRegenerateTheLatestReply(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	m.sidebarCollapsed = true
	_, reply := lastExchange()
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	hoverText(t, tt, "Summarize the pull request.")
	if _, ok := tt.Find(L("Regenerate")); ok {
		t.Fatal("the user's message offers Regenerate")
	}
	if _, ok := tt.Find(L("Edit")); !ok {
		t.Fatal("the user's latest message offers no Edit")
	}
	hoverText(t, tt, reply.Body.Text)
	if err := tt.Click(L("Regenerate")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if store.Chat("chat-patch").Message(reply.ID) != nil || tt.HasText(reply.Body.Text) {
		t.Fatal("the old reply stayed")
	}
}

// Edit takes the message into the composer; sending puts the new words in its place, marked
// Edited, and Escape gives the composer back what it held.
func TestEditAndSendAgain(t *testing.T) {
	m := demoWindow(t)
	m.selectChat("chat-patch")
	m.sidebarCollapsed = true
	asked, reply := lastExchange()
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)
	m.chat.composer.draft = "half a thought"
	hoverText(t, tt, asked.Body.Text)
	if err := tt.Click(L("Edit")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !tt.HasText(L("Editing your message")) || m.chat.composer.draft != asked.Body.Text {
		t.Fatalf("not editing: draft %q, %q", m.chat.composer.draft, tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	settle(tt)
	if m.chat.composer.editing != nil || m.chat.composer.draft != "half a thought" {
		t.Fatalf("Escape kept the edit: draft %q", m.chat.composer.draft)
	}
	hoverText(t, tt, asked.Body.Text)
	if err := tt.Click(L("Edit")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	m.chat.composer.draft = "Summarize the pull request in one line."
	if err := tt.Click(L("Send")); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	edited := store.Chat("chat-patch").Message(asked.ID)
	if edited == nil || edited.Body.Text != "Summarize the pull request in one line." || edited.EditedAt.IsZero() {
		t.Fatalf("the message after the edit: %+v", edited)
	}
	if store.Chat("chat-patch").Message(reply.ID) != nil {
		t.Error("the old reply stayed")
	}
	if !tt.HasText(L("Edited")) || m.chat.composer.editing != nil || m.chat.composer.draft != "half a thought" {
		t.Errorf("after sending: draft %q, %q", m.chat.composer.draft, tt.Texts())
	}
	renderBoth(t, tt, "edited-message")
}
