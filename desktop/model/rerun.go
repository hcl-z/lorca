package model

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// Running the latest exchange of a direct chat again (`chats.rerun`): Regenerate on the bot's
// reply, and Edit on the user's message, which sends it again with new words. The bot's Runner
// takes what came after the message out of the chat on every Device and answers it anew.

// Rerunnable is the user's message whose exchange can run again, with the rows after it: the
// user's latest message in a direct chat with no turn at work, whose reply left nothing the user
// still has to act on or that lives on elsewhere (a draft, a published output, work handed to
// another bot). Nil when there is none; the CLI checks the same, and the compaction too.
func (s *Store) Rerunnable(chatID string) (*Message, []*Message) {
	chat := s.Chat(chatID)
	if chat == nil || !chat.IsDM() || chat.Channel != nil || s.IsResponding(chatID) {
		return nil, nil
	}
	at := -1
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		if chat.Messages[i].Author.Kind == AuthorYou {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, nil
	}
	message := chat.Messages[at]
	if message.Queued || message.State.Kind != StateComplete || message.Body.Kind != BodyText {
		return nil, nil
	}
	after := chat.Messages[at+1:]
	for _, later := range after {
		switch {
		case later.Body.Kind == BodyDraft, later.Output != nil:
			return nil, nil
		case later.Body.Kind == BodyTool && later.Body.Tool.TargetBotID != "":
			return nil, nil
		}
	}
	return message, after
}

// CanRegenerate is whether `message` is the bot's reply to the user's rerunnable message: its last
// text, so the button shows once, under the end of the answer.
func (s *Store) CanRegenerate(chatID string, message *Message) bool {
	asked, after := s.Rerunnable(chatID)
	if asked == nil || message.Author.Kind != AuthorBot {
		return false
	}
	for i := len(after) - 1; i >= 0; i-- {
		if after[i].Author.Kind == AuthorBot && after[i].Body.Kind == BodyText {
			return after[i].ID == message.ID
		}
	}
	return false
}

// CanEdit is whether `message` is the user's rerunnable message.
func (s *Store) CanEdit(chatID string, message *Message) bool {
	asked, _ := s.Rerunnable(chatID)
	return asked != nil && asked.ID == message.ID
}

// Rerun runs the user's message `messageID` again, with `text` when they edited it (empty keeps
// it). The rows after it go at once and the bot shows at work; a refusal puts them back and says
// why.
func (s *Store) Rerun(chatID, messageID, text string, done func(error)) {
	chat := s.Chat(chatID)
	asked, after := s.Rerunnable(chatID)
	if chat == nil || asked == nil || asked.ID != messageID {
		s.post(func() { done(errors.New(L("This reply can no longer run again."))) })
		return
	}
	before := slices.Clone(chat.Messages)
	edited := strings.TrimSpace(text)
	removed := map[string]bool{}
	for _, later := range after {
		removed[later.ID] = true
	}
	chat.Messages = slices.DeleteFunc(slices.Clone(chat.Messages), func(m *Message) bool { return removed[m.ID] })
	for id := range removed {
		s.emit(Event{Kind: EventMessageRemoved, ChatID: chatID, MessageID: id})
	}
	if edited != "" && edited != asked.Body.Text {
		s.Update(messageID, chatID, func(m *Message) {
			m.Body.Text, m.EditedAt = edited, time.Now()
		})
	}
	if s.IsMock {
		if s.replies != nil {
			if current := chat.Message(messageID); current != nil {
				s.replies.respond(current.Body.Text, chat, messageID)
			}
		}
		s.post(func() { done(nil) })
		return
	}
	s.expectTurn(chat)
	params := map[string]any{"chat_id": chatID, "message_id": messageID}
	if edited != "" && edited != asked.Body.Text {
		params["text"] = edited
	}
	s.simple(func(err error) {
		if err != nil {
			// The Runner refused: the chat is as it was.
			if current := s.Chat(chatID); current != nil {
				current.Messages = before
				s.emit(Event{Kind: EventChatsChanged})
			}
		}
		done(err)
	}, "chats.rerun", params)
}
