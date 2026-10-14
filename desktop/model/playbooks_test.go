package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type playbookTransport struct {
	method string
	params map[string]any
	reply  json.RawMessage
	err    error
}

func (t *playbookTransport) Reconnect() {}
func (t *playbookTransport) Request(method string, params any) (json.RawMessage, error) {
	data, _ := json.Marshal(params)
	t.method, t.params = method, nil
	_ = json.Unmarshal(data, &t.params)
	return t.reply, t.err
}

func waitPosts(posts chan func()) {
	select {
	case fn := <-posts:
		fn()
	case <-time.After(2 * time.Second):
	}
}

// An edit names the skill it was opened as and the hash it held, a new skill and a draft from a chat
// name none, and every text file goes back, the ones the sheet does not show included; a refusal
// for a newer version reads as one.
func TestSavePlaybookGuards(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"scope":{"kind":"bot","id":"bot-one"},"name":"weekly-report","description":"For Fridays","instructions":"Compare","hash":"h3","files":[]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	content := PlaybookContent{Name: "weekly-report", Description: "For Fridays", Instructions: "Compare", Examples: "A good report",
		Scripts: []PlaybookFile{{Path: "scripts/run.sh", Text: "echo"}}, Other: []PlaybookFile{{Path: "LICENSE.txt", Text: "MIT"}}}
	over := &PlaybookRecord{ID: "weekly-report", Scope: BotScope("bot-one"), Revision: 1, Hash: "h2"}
	var saved PlaybookRecord
	s.SavePlaybook(over.Scope, content, over, func(record PlaybookRecord, err error) { saved = record })
	waitPosts(posts)
	if transport.method != "skills.save" || transport.params["current"] != "weekly-report" || transport.params["expected_hash"] != "h2" {
		t.Fatalf("save params %v", transport.params)
	}
	skill := transport.params["skill"].(map[string]any)
	if skill["name"] != "weekly-report" || !strings.Contains(skill["instructions"].(string), "## Examples\n\nA good report") {
		t.Errorf("skill %v", skill)
	}
	if files := skill["files"].([]any); len(files) != 2 {
		t.Errorf("files %v", files)
	}
	if saved.ID != "weekly-report" || saved.Hash != "h3" {
		t.Errorf("saved %+v", saved)
	}

	s.SavePlaybook(BotScope("bot-one"), content, nil, func(PlaybookRecord, error) {})
	waitPosts(posts)
	if transport.params["current"] != "" || transport.params["expected_hash"] != "" {
		t.Errorf("new skill params %v", transport.params)
	}
	draft := &PlaybookRecord{Scope: BotScope("bot-one"), Status: "draft", Content: &content}
	s.SavePlaybook(draft.Scope, content, draft, func(PlaybookRecord, error) {})
	waitPosts(posts)
	if transport.params["current"] != "" {
		t.Errorf("a draft from a chat has no folder yet: %v", transport.params)
	}

	transport.err = errors.New("The skill changed since you opened it; reload before saving")
	var got error
	s.SavePlaybook(over.Scope, content, over, func(_ PlaybookRecord, err error) { got = err })
	waitPosts(posts)
	if !errors.Is(got, ErrPlaybookChanged) {
		t.Errorf("error %v", got)
	}
}

// The CLI lists the skills of a bot in a chat, the shared ones included, without their bodies; a
// bot's list puts the latest changed first, and the skills Lorca ships are not the user's to list.
func TestSkillsListedByTheCLI(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"skills":[
		{"scope":{"kind":"bot","id":"bot-one"},"name":"old","description":"","path":"/p/old","builtin":false,"updated_at":10},
		{"scope":{"kind":"bot","id":"bot-one"},"name":"new","description":"","path":"/p/new","builtin":false,"updated_at":20},
		{"scope":{"kind":"project","id":"room"},"name":"group","description":"","path":"/p/group","builtin":false,"updated_at":30},
		{"scope":{"kind":"shared","id":""},"name":"mine","description":"","path":"/p/mine","builtin":false,"updated_at":5},
		{"scope":{"kind":"shared","id":""},"name":"lorca","description":"","path":"/p/lorca","builtin":true,"updated_at":1}]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	s.EnsureSkills("bot-one", "room")
	s.EnsureSkills("bot-one", "room")
	waitPosts(posts)
	if transport.method != "skills.list" || transport.params["bot_id"] != "bot-one" || transport.params["chat_id"] != "room" {
		t.Fatalf("list params %q %v", transport.method, transport.params)
	}
	var names []string
	for _, skill := range s.Skills(BotScope("bot-one")) {
		names = append(names, skill.Name)
	}
	if len(names) != 2 || names[0] != "new" || names[1] != "old" {
		t.Errorf("skills %v", names)
	}
	if group := s.Skills(GroupScope("room")); len(group) != 1 || group[0].ID != "group" {
		t.Errorf("group skills %+v", group)
	}
	if shared := s.SharedSkills(); len(shared) != 1 || shared[0].Name != "mine" {
		t.Errorf("shared skills %+v", shared)
	}
	select {
	case <-posts:
		t.Error("a second ask went out while the list was fresh")
	default:
	}
}

// Save as Skill offers the user's and the bot's messages up to the one clicked, with the request
// before it picked; Save as Standing Instruction offers the user's alone.
func TestCaptureSources(t *testing.T) {
	at := time.Now()
	message := func(id string, author Author) *Message {
		at = at.Add(time.Second)
		return &Message{ID: id, Author: author, Body: Body{Kind: BodyText, Text: id}, CreatedAt: at}
	}
	you, chef, writer := Author{Kind: AuthorYou}, Author{Kind: AuthorBot, BotID: "chef"}, Author{Kind: AuthorBot, BotID: "writer"}
	chat := &Chat{ID: "room", Kind: ChatGroup, BotIDs: []string{"chef", "writer"}, Messages: []*Message{
		message("ask", you), message("other", writer), message("reply", chef), message("fix", you), message("again", you)}}
	botID, kind, sources, picked := CaptureSources(chat, chat.Messages[2])
	if botID != "chef" || kind != "workflow" || len(sources) != 2 || !picked["ask"] || !picked["reply"] {
		t.Errorf("workflow %s %s %d %v", botID, kind, len(sources), picked)
	}
	_, kind, sources, picked = CaptureSources(chat, chat.Messages[4])
	if kind != "corrections" || len(sources) != 3 || len(picked) != 1 {
		t.Errorf("corrections %s %d %v", kind, len(sources), picked)
	}
}
