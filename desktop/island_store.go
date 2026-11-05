package main

import (
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
)

// The store as the island's sessions: each bot with a turn in a chat, and each coding agent, read
// into one islandObservation per change for the state machine (island_state.go).

// islandObservations reads every session the store holds now.
func islandObservations(s *model.Store) []islandObservation {
	var out []islandObservation
	now := time.Now()
	for _, chat := range s.Chats {
		muted := chat.IsMuted(now)
		title := s.Title(chat)
		agentMessages := map[string]bool{}
		// The bots with a coding agent of theirs at work in this chat: the agent's row speaks for
		// the bot's turn, which only waits on it.
		delegating := map[string]bool{}
		// A coding agent is a session of its own, keyed by its card.
		for _, message := range islandRecent(chat.Messages) {
			agent := model.AgentOf(message)
			// A card before its agent starts still counts: the row opens "starting", goes to
			// Auto-review ("checking"), and may ask the user to allow the start, all before the
			// agent runs. Dropping it then would read as the agent finishing.
			if agent == nil || !(agent.Started() || agent.State == model.AgentChecking || agent.Question != nil) {
				continue
			}
			agentMessages[message.ID] = true
			if islandAgentRunning(agent) {
				delegating[message.Author.BotID] = true
			}
			o := islandObservation{
				Key:       chat.ID + "/" + message.ID,
				ChatID:    chat.ID,
				BotID:     message.Author.BotID,
				AgentKind: agent.Kind,
				Title:     agent.Name(),
				Running:   islandAgentRunning(agent),
				Muted:     muted,
			}
			if agent.Now != "" {
				o.Tool, o.ToolID = agent.Now, agent.Now
			}
			if n := len(agent.Tail); n > 0 {
				o.Reply, o.ReplyID, o.ReplyFinal = agent.Tail[n-1], message.ID, !agent.Busy
			}
			if agent.State == model.AgentFailed || agent.Error != "" {
				o.Failure, o.FailureID = agent.Error, message.ID+":"+agent.Error
				if o.Failure == "" {
					o.Failure = L("Error")
				}
			}
			if agent.Task != "" {
				o.Prompt, o.PromptID = agent.Task, message.ID
			}
			o.Ask = islandAgentAsk(chat, message, agent)
			out = append(out, o)
		}
		// Each bot in the chat with a turn running, or something it asked still pending, is one.
		bots := s.WorkingBots(chat.ID)
		asks := islandBotAsks(chat, agentMessages)
		for botID := range asks {
			if !containsString(bots, botID) {
				bots = append(bots, botID)
			}
		}
		for _, botID := range bots {
			if delegating[botID] && asks[botID] == nil {
				// Its coding agent's row says what it does; a second row for the bot would only
				// say it waits.
				continue
			}
			o := islandObservation{
				Key:     chat.ID + "#" + botID,
				ChatID:  chat.ID,
				BotID:   botID,
				Title:   islandSessionTitle(chat, title, botID),
				Running: s.IsResponding(chat.ID) && containsString(s.WorkingBots(chat.ID), botID),
				Ask:     asks[botID],
				Muted:   muted,
			}
			islandReadTurn(chat, botID, agentMessages, &o)
			out = append(out, o)
		}
	}
	return out
}

// islandRecent is the chat's latest messages, where the cards of what is at work are.
func islandRecent(messages []*model.Message) []*model.Message {
	const lookback = 80
	if len(messages) > lookback {
		return messages[len(messages)-lookback:]
	}
	return messages
}

// islandSessionTitle names a bot's session: the bot in its DM, the bot and the group in a group.
func islandSessionTitle(chat *model.Chat, chatTitle, botID string) string {
	name := islandBotName(botID)
	if chat.IsGroup() && chatTitle != "" && chatTitle != name {
		return name + " · " + chatTitle
	}
	return name
}

// islandReadTurn reads a bot's turn off the chat's latest messages: the user's last words, the
// bot's reply as it streams, the tool it runs, and a failure.
func islandReadTurn(chat *model.Chat, botID string, agents map[string]bool, o *islandObservation) {
	messages := islandRecent(chat.Messages)
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Author.Kind == model.AuthorYou {
			if o.PromptID == "" && message.Body.Kind == model.BodyText {
				o.Prompt, o.PromptID = message.Body.Text, message.ID
			}
			// Before the user's last words is the turn before.
			break
		}
		if message.Author.Kind != model.AuthorBot || message.Author.BotID != botID || agents[message.ID] {
			continue
		}
		switch message.Body.Kind {
		case model.BodyText:
			if message.State.Kind == model.StateFailed {
				if o.FailureID == "" {
					o.Failure, o.FailureID = message.State.Error, message.ID
				}
				continue
			}
			if o.ReplyID == "" && strings.TrimSpace(message.Body.Text) != "" {
				o.Reply, o.ReplyID = message.Body.Text, message.ID
				o.ReplyFinal = message.State.Kind == model.StateComplete
			}
		case model.BodyTool:
			tool := message.Body.Tool
			if tool != nil && tool.IsRunning && o.ToolID == "" && o.ReplyID == "" {
				command := ""
				if tool.Run != nil {
					command = tool.Run.Command
				}
				o.Tool, o.ToolID = islandToolText(tool.Description, command, tool.Summary), message.ID
			}
		}
	}
}

// islandBotAsks are the questions each bot in the chat has pending: an `ask_user` card, a permission
// card, or a command card waiting for an approval.
func islandBotAsks(chat *model.Chat, agents map[string]bool) map[string]*islandAsk {
	asks := map[string]*islandAsk{}
	for _, message := range chat.Messages {
		if message.Author.Kind != model.AuthorBot || agents[message.ID] || asks[message.Author.BotID] != nil {
			continue
		}
		if ask := islandMessageAsk(chat, message); ask != nil {
			asks[message.Author.BotID] = ask
		}
	}
	return asks
}

// islandMessageAsk is the question one message holds, as the island answers it.
func islandMessageAsk(chat *model.Chat, message *model.Message) *islandAsk {
	body := message.Body
	base := islandAsk{ChatID: chat.ID, MessageID: message.ID, BotName: islandAuthorName(message)}
	switch {
	case body.Kind == model.BodyPermission && body.Request != nil && body.Request.IsPending() && body.Request.IsAsk():
		ask := base
		ask.Kind, ask.Question, ask.Options = islandAskChoices, body.Request.Ask.Question, body.Request.Ask.Options
		// Several picks, or the user's own words: a click cannot say that, so the chat asks.
		if body.Request.Ask.Multiple || body.Request.Ask.AllowOther {
			ask.Kind = islandAskElsewhere
		}
		return &ask
	case body.Kind == model.BodyPermission && body.Request != nil && body.Request.IsPending() && body.Request.IsAgentAsk():
		ask := base
		ask.Kind, ask.Question = islandAskElsewhere, body.Request.Summary
		return &ask
	case body.Kind == model.BodyPermission && body.Request != nil && body.Request.IsPending() && !body.Request.IsAccess() && !body.Request.IsSecret() && !body.Request.IsConnect():
		ask := base
		ask.Kind, ask.Decisions = islandAskPermission, body.Request.Choices()
		ask.Command = model.FirstLine(body.Request.Command)
		if ask.Command == "" {
			ask.Command = body.Request.Summary
		}
		ask.Question = islandToolText("", ask.Command, body.Request.Summary)
		return &ask
	case body.Kind == model.BodyPermission && body.Request != nil && body.Request.IsPending():
		// An access request, a secret, or a sign-in: the chat's card holds what the island cannot.
		ask := base
		ask.Kind, ask.Question = islandAskElsewhere, body.Request.Summary
		return &ask
	case body.Kind == model.BodyTool && body.Tool != nil && body.Tool.Run != nil && body.Tool.Run.State == model.CommandAsking:
		run := body.Tool.Run
		ask := base
		ask.Kind, ask.Command = islandAskPermission, model.FirstLine(run.Command)
		ask.Question = "$ " + ask.Command
		ask.Decisions = []model.Answer{{Title: L("Allow once"), Decision: "allow"}, {Title: L("Deny"), Decision: "deny"}}
		if run.HasRule {
			ask.Decisions = []model.Answer{{Title: L("Allow once"), Decision: "allow"}, {Title: L("Always allow"), Decision: "always"}, {Title: L("Deny"), Decision: "deny"}}
		}
		return &ask
	}
	return nil
}

// islandAgentAsk is what a coding agent asks: an approval the island answers, or a form the chat
// draws.
func islandAgentAsk(chat *model.Chat, message *model.Message, agent *model.AgentRun) *islandAsk {
	if q := agent.Question; q != nil {
		ask := &islandAsk{ChatID: chat.ID, MessageID: message.ID, BotName: agent.Name(), Question: islandQuestionText(q)}
		if q.IsPermission() {
			ask.Kind, ask.Decisions = islandAskPermission, q.Decisions()
			ask.Command = model.FirstLine(q.Command)
			if ask.Command == "" {
				ask.Command = model.FirstLine(q.Text)
			}
		} else {
			ask.Kind = islandAskElsewhere
		}
		return ask
	}
	if agent.Asking != "" {
		// The agent asks with a form of its own (agent_ask_card.go), which the chat draws.
		return &islandAsk{ChatID: chat.ID, MessageID: message.ID, BotName: agent.Name(), Kind: islandAskElsewhere}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// islandAgentRunning is a coding agent at work: running, or between its turns while its session
// stays open and it has not finished (idle with nothing to say yet), as its card's pill reads.
// An agent idle after its last answer is done.
func islandAgentRunning(agent *model.AgentRun) bool {
	if agent.IsRunning() || agent.State == model.AgentChecking {
		return true
	}
	return agent.State == model.AgentIdle && agent.Outcome == "" && agent.Output == "" && agent.Error == ""
}
