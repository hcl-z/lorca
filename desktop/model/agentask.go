package model

import (
	"errors"
	"strings"
)

// What a coding agent asks the user, on a card of its own in the chat (`tool` = `agent_ask`): the
// agent's message and a form, field by field. The card is the bot's; its answer goes to the agent
// through the `coding_agent` row that started it (`coding.answer`), here or on the bot's Runner.
// Answered, the card keeps the form and shows what was given and by whom: DecisionAllowed
// (answered), DecisionDenied (declined), DecisionExpired (nobody answered in time), or
// DecisionDismissed (the agent stopped first).

// AgentAsk is the question: the agent's handle, kind, and name, the row it answers through, its
// message, its fields, and once answered, each field's answer in words and who gave it.
type AgentAsk struct {
	AgentID string          `json:"agent_id"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	RowID   string          `json:"row_id"`
	Message string          `json:"message"`
	Fields  []AgentAskField `json:"fields"`
	Answers []AgentSetting  `json:"answers"`
	// By is who answered or declined it: "you" or "bot".
	By string `json:"by"`
}

// AgentAskField is one field: Kind is "one" (a pick among Choices), "many" (any of them), "flag"
// (yes or no), "text", "number", or "integer". Other is whether a pick may be the user's own words.
type AgentAskField struct {
	Key      string           `json:"key"`
	Label    string           `json:"label"`
	Kind     string           `json:"kind"`
	Required bool             `json:"required"`
	Choices  []AgentAskChoice `json:"choices"`
	Other    bool             `json:"other"`
}

// AgentAskChoice is one pick: what the agent is told, what the user reads, and a line about it.
type AgentAskChoice struct {
	Value       string `json:"value"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// IsAgentAsk is a coding agent's question.
func (r *PermissionRequest) IsAgentAsk() bool { return r.Tool == "agent_ask" && r.AgentAsk != nil }

// AnswerAgentAsk sends the form filled in, by field key: a pick's value, a list of them, the user's
// own words, true or false, or a number. The card reads the answer at once, in `answers`; a failure
// (the Runner is offline, the agent no longer asks) puts it back and says why.
func (s *Store) AnswerAgentAsk(chatID, cardID string, values map[string]any, answers []AgentSetting, done func(error)) {
	s.settleAgentAsk(chatID, cardID, DecisionAllowed, answers, map[string]any{"values": values}, done)
}

// DeclineAgentAsk declines the question: the agent goes on without an answer.
func (s *Store) DeclineAgentAsk(chatID, cardID string, done func(error)) {
	s.settleAgentAsk(chatID, cardID, DecisionDenied, nil, map[string]any{"decline": true}, done)
}

func (s *Store) settleAgentAsk(chatID, cardID string, decision PermissionDecision, answers []AgentSetting, params map[string]any, done func(error)) {
	var before *PermissionRequest
	s.Update(cardID, chatID, func(message *Message) {
		if message.Body.Kind != BodyPermission || !message.Body.Request.IsAgentAsk() || !message.Body.Request.IsPending() {
			return
		}
		before = message.Body.Request
		request := *before
		ask := *request.AgentAsk
		ask.Answers, ask.By = answers, "you"
		request.AgentAsk, request.Decision = &ask, decision
		message.Body.Request = &request
	})
	if before == nil {
		s.post(func() { done(errors.New(L("It no longer asks."))) })
		return
	}
	if s.IsMock {
		s.post(func() { done(nil) })
		return
	}
	params["chat_id"], params["message_id"] = chatID, before.AgentAsk.RowID
	s.simple(func(err error) {
		if err != nil {
			s.Update(cardID, chatID, func(message *Message) {
				if message.Body.Kind == BodyPermission {
					message.Body.Request = before
				}
			})
		}
		done(err)
	}, "coding.answer", params)
}

// AnswersText is the answer in words, as one line: "Database: SQLite · Targets: A, B".
func (a *AgentAsk) AnswersText() string {
	parts := make([]string, 0, len(a.Answers))
	for _, answer := range a.Answers {
		parts = append(parts, answer.Name+": "+answer.Value)
	}
	return strings.Join(parts, " · ")
}
