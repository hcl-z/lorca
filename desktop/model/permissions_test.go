package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPermissionsWireKeepsFullAndEmptyGrantsApart(t *testing.T) {
	base := `{"id":"b","name":"Inbox","provider":"deepseek","created_at":1,"permissions":%s}`
	full := ToBot(decodeJSON[WireBot](t, sprintf(base, `{}`))).Permissions
	if full == nil || full.Connections != nil || !full.Tools.Shell.Enabled || full.Level("github") != AccessOn || full.Summary() != L("Full access") {
		t.Fatalf("full policy: %+v", full)
	}
	none := ToBot(decodeJSON[WireBot](t, sprintf(base, `{"connections":{},"tools":{"shell":{"enabled":false},"read":{"enabled":false},"write":{"enabled":false}}}`))).Permissions
	data, err := json.Marshal(none)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	_ = json.Unmarshal(data, &wire)
	if !reflect.DeepEqual(wire["connections"], map[string]any{}) || wire["tools"].(map[string]any)["shell"].(map[string]any)["enabled"] != false {
		t.Fatalf("empty grant widened: %s", data)
	}
	if none.Level("github") != AccessNone || none.Summary() != L("Limited") {
		t.Fatal("a plugin the map leaves out should be off")
	}
	if ToBot(decodeJSON[WireBot](t, sprintf(base, `null`))).Permissions != nil {
		t.Fatal("a bot with no policy should have none")
	}
	copy := none.Clone()
	(*copy.Connections)["work"] = ConnectionPermissions{Enabled: true}
	if len(*none.Connections) != 0 {
		t.Fatal("a clone shares its grants")
	}
}

func TestSwitchesAreOnAskOrOff(t *testing.T) {
	if ToolOn.Level() != AccessOn || ToolAsks.Level() != AccessAsk || ToolOff.Level() != AccessNone {
		t.Fatal("a switch's three states")
	}
	if AccessAsk.Permission() != ToolAsks || !AccessAsk.Permission().Enabled || !AccessAsk.Permission().Ask {
		t.Fatal("asking is on, and asks")
	}
	// A tool with no switch of its own takes the plugin's.
	grant := ConnectionPermissions{Enabled: true}
	if grant.Tool("any").Level() != AccessOn {
		t.Fatal("an unlisted tool takes its plugin's switch")
	}
	pluginOff := ConnectionPermissions{Enabled: false, Tools: map[string]ToolPermission{"kept": ToolOn}}
	if pluginOff.Tool("other").Level() != AccessNone || pluginOff.Tool("kept").Level() != AccessOn {
		t.Fatal("a tool's own switch decides over a plugin that is off")
	}
}

func TestAccessRequestOnlyOpensAccessOrIsDismissed(t *testing.T) {
	store := NewStore(nil, func(fn func()) { fn() }, true)
	store.Start()
	request := &PermissionRequest{PluginID: "computer", Tool: "access", Summary: "Shell commands", Decision: DecisionPending}
	store.Append(&Message{ID: "access", Author: BotAuthor("bot-nova"), Body: Body{Kind: BodyPermission, Request: request}}, "chat-nova")
	if request.IsShell() || !reflect.DeepEqual(request.Choices(), []Answer{{L("Edit Access…"), "access"}, {L("Dismiss"), "deny"}}) {
		t.Fatal("an access request offers to allow the call")
	}
	if request.ShownSummary() != L("Shell commands") || request.ShownReason() != L("Not allowed in this bot's Access settings.") {
		t.Fatal("access request wording")
	}
	store.AnswerPermission("chat-nova", "access", "deny")
	messages := store.Chat("chat-nova").Messages
	if messages[len(messages)-1].Body.Request.Decision != DecisionDismissed {
		t.Fatal("Dismiss should dismiss the request")
	}
}

func TestMockWriterAccessMatchesTheCatalog(t *testing.T) {
	access := mockWriterAccess()
	if access.Level("github") != AccessOn || access.Level("linear") != AccessNone || access.Tools.Shell.Enabled {
		t.Fatalf("writer access: %+v", access)
	}
	if access.ToolLevel("github", "create_pull_request_review") != AccessAsk {
		t.Fatal("reviewing a pull request asks first")
	}
	if len(MockAccessCatalog().Connections) != 5 {
		t.Fatal("the catalog lists Workbench's enabled plugins")
	}
}
