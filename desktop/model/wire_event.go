package model

import (
	"encoding/json"
	"log"
)

// WireEvent is one event frame from the CLI, decoded once. `crates/cli/src/events.rs` is the
// contract; this file is the app's side of it, so the store applies typed events and reads no
// JSON field name.
type WireEvent interface{ wireEvent() }

type SnapshotEvent struct{ Snapshot WireSnapshot }

// RosterChangedEvent replaces the Devices, bots, and chat metadata the account shares.
type RosterChangedEvent struct{ Roster WireRosterChanged }

// MessageEvent is a new message in a chat (`message.added`), or one whose body, state, or
// usage changed (`message.updated`).
type MessageEvent struct {
	ChatID  string
	Message WireMessage
	IsNew   bool
}

type MessageRemovedEvent struct{ ChatID, MessageID string }

type ChatRemovedEvent struct{ ChatID string }

// JobEvent is a turn in flight on some Runner (`job.started`), or its end (`job.finished`).
type JobEvent struct {
	Job      WireJobEvent
	Finished bool
}

type JobRetryEvent struct{ Retry WireJobRetry }

// JobThinkingEvent is the bot's model starting to think; its next message is what came of it.
type JobThinkingEvent struct{ ChatID, BotID string }

type ChatUsageEvent struct {
	ChatID string
	Usage  WireChatUsage
}

type RelayStatusEvent struct{ Status WireRelayStatus }

type IdentityChangedEvent struct{ HasIdentity bool }

type BudgetsChangedEvent struct{ Budgets []BudgetState }

type AttentionChangedEvent struct{ View AttentionView }

type ReviewChangedEvent struct{ Item ReviewItem }

type TaskChangedEvent struct{ Task DurableTask }

// ProjectContextChangedEvent says a group's shared project context changed.
type ProjectContextChangedEvent struct{ ChatID string }

// FeedbackChangedEvent says a bot's feedback changed; its sheet reads it again.
type FeedbackChangedEvent struct{ BotID string }

// OtherEvent is an event the desktop keeps no state for: a phone's sign-in pages
// (`provider.auth`, `plugin.auth`, `plugin.auth.done`), which a computer opens itself; pairing
// (`pair.posted`, `pair.completed`), which the pairing request's own reply carries; and the
// phone's push key (`account.key_changed`).
type OtherEvent struct{ Name string }

func (SnapshotEvent) wireEvent()              {}
func (RosterChangedEvent) wireEvent()         {}
func (MessageEvent) wireEvent()               {}
func (MessageRemovedEvent) wireEvent()        {}
func (ChatRemovedEvent) wireEvent()           {}
func (JobEvent) wireEvent()                   {}
func (JobRetryEvent) wireEvent()              {}
func (JobThinkingEvent) wireEvent()           {}
func (ChatUsageEvent) wireEvent()             {}
func (RelayStatusEvent) wireEvent()           {}
func (IdentityChangedEvent) wireEvent()       {}
func (BudgetsChangedEvent) wireEvent()        {}
func (AttentionChangedEvent) wireEvent()      {}
func (ReviewChangedEvent) wireEvent()         {}
func (TaskChangedEvent) wireEvent()           {}
func (ProjectContextChangedEvent) wireEvent() {}
func (FeedbackChangedEvent) wireEvent()       {}
func (OtherEvent) wireEvent()                 {}

// DecodeEvent decodes the frame `name` carries `data` for. An unknown name or a payload that
// does not decode is logged and yields false: the store applies nothing of it.
func DecodeEvent(name string, data json.RawMessage) (WireEvent, bool) {
	event, err := decodeEvent(name, data)
	if err != nil {
		log.Printf("event %s: %v", name, err)
		return nil, false
	}
	if event == nil {
		log.Printf("event %s: unknown", name)
		return nil, false
	}
	return event, true
}

func decodeEvent(name string, data json.RawMessage) (WireEvent, error) {
	switch name {
	case "snapshot":
		var snapshot WireSnapshot
		err := json.Unmarshal(data, &snapshot)
		return SnapshotEvent{snapshot}, err
	case "roster.changed":
		var roster WireRosterChanged
		err := json.Unmarshal(data, &roster)
		return RosterChangedEvent{roster}, err
	case "message.added", "message.updated":
		var payload struct {
			ChatID  string      `json:"chat_id"`
			Message WireMessage `json:"message"`
		}
		err := json.Unmarshal(data, &payload)
		return MessageEvent{ChatID: payload.ChatID, Message: payload.Message, IsNew: name == "message.added"}, err
	case "message.removed":
		var payload struct {
			ChatID    string `json:"chat_id"`
			MessageID string `json:"message_id"`
		}
		err := json.Unmarshal(data, &payload)
		return MessageRemovedEvent{payload.ChatID, payload.MessageID}, err
	case "chat.removed":
		var payload struct {
			ChatID string `json:"chat_id"`
		}
		err := json.Unmarshal(data, &payload)
		return ChatRemovedEvent{payload.ChatID}, err
	case "job.started", "job.finished":
		var job WireJobEvent
		err := json.Unmarshal(data, &job)
		return JobEvent{Job: job, Finished: name == "job.finished"}, err
	case "job.retry":
		var retry WireJobRetry
		err := json.Unmarshal(data, &retry)
		return JobRetryEvent{retry}, err
	case "job.thinking":
		var payload struct {
			ChatID string `json:"chat_id"`
			BotID  string `json:"bot_id"`
		}
		err := json.Unmarshal(data, &payload)
		return JobThinkingEvent{payload.ChatID, payload.BotID}, err
	case "chat.usage":
		var payload struct {
			ChatID string        `json:"chat_id"`
			Usage  WireChatUsage `json:"usage"`
		}
		err := json.Unmarshal(data, &payload)
		return ChatUsageEvent{payload.ChatID, payload.Usage}, err
	case "relay.status":
		var status WireRelayStatus
		err := json.Unmarshal(data, &status)
		return RelayStatusEvent{status}, err
	case "identity.changed":
		var payload struct {
			HasIdentity bool `json:"has_identity"`
		}
		err := json.Unmarshal(data, &payload)
		return IdentityChangedEvent{payload.HasIdentity}, err
	case "budgets.changed":
		var payload struct {
			Budgets []BudgetState `json:"budgets"`
		}
		err := json.Unmarshal(data, &payload)
		return BudgetsChangedEvent{payload.Budgets}, err
	case "attention.changed":
		var view AttentionView
		err := json.Unmarshal(data, &view)
		return AttentionChangedEvent{view}, err
	case "reviews.changed":
		var payload struct {
			Item ReviewItem `json:"item"`
		}
		err := json.Unmarshal(data, &payload)
		return ReviewChangedEvent{payload.Item}, err
	case "tasks.changed":
		var payload struct {
			Task DurableTask `json:"task"`
		}
		err := json.Unmarshal(data, &payload)
		return TaskChangedEvent{payload.Task}, err
	case "projects.changed":
		var payload struct {
			ChatID string `json:"chat_id"`
		}
		err := json.Unmarshal(data, &payload)
		return ProjectContextChangedEvent{payload.ChatID}, err
	case "feedback.changed":
		var payload struct {
			BotID string `json:"bot_id"`
		}
		err := json.Unmarshal(data, &payload)
		return FeedbackChangedEvent{payload.BotID}, err
	case "provider.auth", "plugin.auth", "plugin.auth.done", "pair.posted", "pair.completed", "account.key_changed":
		return OtherEvent{name}, nil
	}
	return nil, nil
}
