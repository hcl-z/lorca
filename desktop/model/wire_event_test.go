package model

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// Every event the CLI sends decodes to a typed event: `events.rs` is the contract.
func TestEveryCLIEventDecodes(t *testing.T) {
	source, err := os.ReadFile("../../crates/cli/src/events.rs")
	if err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile(`#\[serde\(rename = "([a-z_.]+)"\)\]`).FindAllStringSubmatch(string(source), -1)
	if len(names) < 20 {
		t.Fatalf("found %d event names in events.rs", len(names))
	}
	for _, name := range names {
		if event, ok := DecodeEvent(name[1], json.RawMessage(`{}`)); !ok || event == nil {
			t.Errorf("%s does not decode", name[1])
		}
	}
}

func TestEventsDecodeTheirPayloads(t *testing.T) {
	event, ok := DecodeEvent("message.updated", json.RawMessage(`{"chat_id":"c","message":{"id":"m","chat_id":"c","author":{"kind":"bot","bot_id":"b1"},"body":{"kind":"text","text":"hi"},"state":{"kind":"streaming"},"created_at":1}}`))
	message, isMessage := event.(MessageEvent)
	if !ok || !isMessage || message.IsNew || message.ChatID != "c" || message.Message.ID != "m" || str(message.Message.Author.BotID) != "b1" {
		t.Fatalf("message: %#v", event)
	}
	event, _ = DecodeEvent("job.finished", json.RawMessage(`{"chat_id":"c","bot_id":"b1","job_id":"j","routine_id":"r"}`))
	if job, isJob := event.(JobEvent); !isJob || !job.Finished || job.Job.JobID != "j" || str(job.Job.RoutineID) != "r" {
		t.Fatalf("job: %#v", event)
	}
	event, _ = DecodeEvent("identity.changed", json.RawMessage(`{"has_identity":true}`))
	if identity, isIdentity := event.(IdentityChangedEvent); !isIdentity || !identity.HasIdentity {
		t.Fatalf("identity: %#v", event)
	}
}

// A frame the desktop cannot read applies nothing.
func TestAnUnknownOrMalformedEventDecodesToNothing(t *testing.T) {
	if _, ok := DecodeEvent("chat.renamed", json.RawMessage(`{"chat_id":"c"}`)); ok {
		t.Fatal("unknown event decoded")
	}
	if _, ok := DecodeEvent("message.added", json.RawMessage(`{"chat_id":7}`)); ok {
		t.Fatal("malformed event decoded")
	}
}
