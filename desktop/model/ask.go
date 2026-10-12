package model

// Questions a bot asks with options (`ask_user`): the card offers the options, and the answer goes
// to the bot's Runner through `chats.permission`, which keeps it on the card for every Device.

// AnswerAsk answers a question with the options picked, by their place, and the user's own words.
// The card reads the answer at once; a failure (the Runner is offline, the question expired)
// puts it back and says why.
func (s *Store) AnswerAsk(chatID, messageID string, choices []int, other string, done func(error)) {
	var before *PermissionRequest
	s.Update(messageID, chatID, func(message *Message) {
		if message.Body.Kind != BodyPermission || !message.Body.Request.IsAsk() || !message.Body.Request.IsPending() {
			return
		}
		before = message.Body.Request
		request := *before
		ask := *request.Ask
		ask.Answer = &AskAnswer{Choices: choices, Other: other}
		request.Ask, request.Decision = &ask, DecisionAllowed
		message.Body.Request = &request
	})
	if s.IsMock {
		s.post(func() { done(nil) })
		return
	}
	answer := map[string]any{"choices": choices}
	if other != "" {
		answer["other"] = other
	}
	s.simple(func(err error) {
		if err != nil && before != nil {
			s.Update(messageID, chatID, func(message *Message) {
				if message.Body.Kind == BodyPermission {
					message.Body.Request = before
				}
			})
		}
		done(err)
	}, "chats.permission", map[string]any{"chat_id": chatID, "message_id": messageID, "decision": "allow", "answer": answer})
}

// SkipAsk leaves a question unanswered; the bot goes on with what it thinks best.
func (s *Store) SkipAsk(chatID, messageID string) {
	if s.IsMock {
		s.Update(messageID, chatID, func(message *Message) {
			if message.Body.Kind == BodyPermission && message.Body.Request.IsPending() {
				request := *message.Body.Request
				request.Decision = DecisionDenied
				message.Body.Request = &request
			}
		})
		return
	}
	s.AnswerPermission(chatID, messageID, "deny")
}
