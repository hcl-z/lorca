package model

import "testing"

func TestChatPreviewUsesLastMessageWhenMessagesAreEmpty(t *testing.T) {
	s := &Store{}
	chat := &Chat{LastMessage: &Message{Body: Body{Kind: BodyText, Text: "hello"}}}
	if got := s.Preview(chat); got != "hello" {
		t.Fatalf("got %q", got)
	}
}
