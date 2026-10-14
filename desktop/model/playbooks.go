package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// A bot's or a group's skills, after the macOS app's Playbooks.swift. The CLI owns them: the
// roster lists them without their bodies, and a body is fetched when one opens.

// PlaybookScope is whose skill it is: one bot's, in every chat it is in, or a group's, for the bots
// in that group.
type PlaybookScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func BotScope(id string) PlaybookScope   { return PlaybookScope{Kind: "bot", ID: id} }
func GroupScope(id string) PlaybookScope { return PlaybookScope{Kind: "project", ID: id} }

// SharedScope is the skills every bot on a Runner uses.
func SharedScope() PlaybookScope { return PlaybookScope{Kind: "shared"} }

func (s PlaybookScope) IsGroup() bool  { return s.Kind == "project" }
func (s PlaybookScope) IsShared() bool { return s.Kind == "shared" }

// SkillScope is whose skills a chat shows: the bot's in a DM, the group's in a group.
func SkillScope(chat *Chat, members []*Bot) (PlaybookScope, bool) {
	if chat == nil {
		return PlaybookScope{}, false
	}
	if chat.IsGroup() {
		return GroupScope(chat.ID), true
	}
	if len(members) == 0 {
		return PlaybookScope{}, false
	}
	return BotScope(members[0].ID), true
}

// PlaybookFile is a reference or a script the skill carries, as text: `references/checklist.md`.
type PlaybookFile struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type PlaybookContent struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Instructions string         `json:"instructions"`
	Examples     string         `json:"examples"`
	References   []PlaybookFile `json:"references"`
	Scripts      []PlaybookFile `json:"scripts"`
	// Other are the skill's text files outside references/ and scripts/. The sheet does not show
	// them; a save sends them back, so editing a skill does not drop them.
	Other []PlaybookFile `json:"-"`
}

func (c PlaybookContent) clone() PlaybookContent {
	c.References, c.Scripts = slices.Clone(c.References), slices.Clone(c.Scripts)
	if c.References == nil {
		c.References = []PlaybookFile{}
	}
	if c.Scripts == nil {
		c.Scripts = []PlaybookFile{}
	}
	return c
}

// PlaybookSummary is a skill as the roster lists it, without its body.
type PlaybookSummary struct {
	// ID is the skill's name, which is its folder's.
	ID          string        `json:"id"`
	Scope       PlaybookScope `json:"scope"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Status      string        `json:"status"`
	Revision    uint64        `json:"revision"`
	Hash        string        `json:"hash"`
	UpdatedAt   float64       `json:"updated_at"`
	// Path is the skill's folder on its Runner. Builtin is a skill Lorca ships.
	Path    string `json:"path"`
	Builtin bool   `json:"builtin"`
	// Source is the address the skill was imported from, empty for one written here.
	Source string `json:"source"`
}

func (s PlaybookSummary) IsDraft() bool { return s.Status == "draft" }

// PlaybookRevision is one step of a skill's history: who changed it where, and what it said then.
type PlaybookRevision struct {
	ID         string           `json:"id"`
	Revision   uint64           `json:"revision"`
	Status     string           `json:"status"`
	Content    *PlaybookContent `json:"content"`
	Provenance struct {
		Kind       string   `json:"kind"`
		ChatID     *string  `json:"chat_id"`
		MessageIDs []string `json:"message_ids"`
	} `json:"provenance"`
	DeviceID  string  `json:"device_id"`
	CreatedAt float64 `json:"created_at"`
}

// PlaybookRecord is a skill with its body and history, fetched when it opens.
type PlaybookRecord struct {
	ID        string             `json:"id"`
	Scope     PlaybookScope      `json:"scope"`
	Status    string             `json:"status"`
	Revision  uint64             `json:"revision"`
	Hash      string             `json:"hash"`
	Content   *PlaybookContent   `json:"content"`
	Revisions []PlaybookRevision `json:"revisions"`
	// Path is the skill's folder on its Runner. Builtin is a skill Lorca ships, which is not edited.
	// Source is the address it was imported from.
	Path    string `json:"path"`
	Builtin bool   `json:"builtin"`
	Source  string `json:"source"`
}

func (r PlaybookRecord) IsDraft() bool { return r.Status == "draft" }

func (r PlaybookRecord) summary() PlaybookSummary {
	s := PlaybookSummary{ID: r.ID, Scope: r.Scope, Status: r.Status, Revision: r.Revision, Hash: r.Hash, Source: r.Source}
	if r.Content != nil {
		s.Name, s.Description = r.Content.Name, r.Content.Description
	}
	for _, step := range r.Revisions {
		s.UpdatedAt = max(s.UpdatedAt, step.CreatedAt)
	}
	return s
}

// ErrPlaybookChanged is a save or a delete refused because the skill changed on another Device.
var ErrPlaybookChanged = errors.New("This skill changed on another Device")

func playbookError(err error) error {
	if err != nil && strings.Contains(err.Error(), "changed since") {
		return ErrPlaybookChanged
	}
	return err
}

// Skills are a bot's or a group's skills: drafts waiting for review first, then the latest changed.
func (s *Store) Skills(scope PlaybookScope) []PlaybookSummary {
	var skills []PlaybookSummary
	for _, skill := range s.Playbooks {
		if skill.Scope == scope {
			skills = append(skills, skill)
		}
	}
	slices.SortStableFunc(skills, func(a, b PlaybookSummary) int {
		if a.IsDraft() != b.IsDraft() {
			if a.IsDraft() {
				return -1
			}
			return 1
		}
		switch {
		case a.UpdatedAt > b.UpdatedAt:
			return -1
		case a.UpdatedAt < b.UpdatedAt:
			return 1
		}
		return 0
	})
	return skills
}

func (s *Store) Playbook(scope PlaybookScope, id string, done func(PlaybookRecord, error)) {
	if s.IsMock {
		record, ok := PlaybookRecord{}, false
		for _, each := range s.mockPlaybooks {
			if each.ID == id && each.Scope == scope {
				record, ok = each, true
			}
		}
		s.post(func() {
			if !ok {
				done(record, errors.New(L("This skill was deleted.")))
				return
			}
			done(record, nil)
		})
		return
	}
	Async(s, func() (PlaybookRecord, error) {
		view, err := call[skillView](s, "skills.get", s.scoped(scope, map[string]any{"name": id}))
		return view.record(), err
	}, done)
}

// SavePlaybook saves a new skill, or rewrites the one `over` holds. The CLI refuses it with
// ErrPlaybookChanged when the skill changed since `over` was read.
func (s *Store) SavePlaybook(scope PlaybookScope, content PlaybookContent, over *PlaybookRecord, done func(PlaybookRecord, error)) {
	s.SavePlaybookVia(scope, s.skillsKey[0], content, over, done)
}

// SavePlaybookVia is SavePlaybook with a shared skill kept on `via`'s Runner, or this computer
// for "".
func (s *Store) SavePlaybookVia(scope PlaybookScope, via string, content PlaybookContent, over *PlaybookRecord, done func(PlaybookRecord, error)) {
	if s.IsMock {
		saved := s.saveMockPlaybook(content.clone(), scope, over, "saved")
		s.post(func() { done(saved, nil) })
		return
	}
	// A draft from the chat has no folder yet, so it saves as a new skill.
	params := s.scopedVia(scope, via, map[string]any{"current": "", "skill": content.clone().skillInput(), "expected_hash": ""})
	if over != nil && over.ID != "" {
		params["current"], params["expected_hash"] = over.ID, over.Hash
	}
	Async(s, func() (PlaybookRecord, error) {
		view, err := call[skillView](s, "skills.save", params)
		return view.record(), playbookError(err)
	}, func(record PlaybookRecord, err error) {
		if err == nil {
			s.invalidateSkills()
		}
		done(record, err)
	})
}

func (s *Store) RemovePlaybook(record PlaybookRecord, done func(error)) {
	if s.IsMock {
		s.mockPlaybooks = slices.DeleteFunc(slices.Clone(s.mockPlaybooks), func(each PlaybookRecord) bool { return each.ID == record.ID && each.Scope == record.Scope })
		s.mockPlaybooksChanged()
		s.post(func() { done(nil) })
		return
	}
	params := s.scoped(record.Scope, map[string]any{"name": record.ID, "expected_hash": record.Hash})
	Async(s, func() (struct{}, error) {
		_, err := call[json.RawMessage](s, "skills.remove", params)
		return struct{}{}, playbookError(err)
	}, func(_ struct{}, err error) {
		if err == nil {
			s.invalidateSkills()
		}
		done(err)
	})
}

// DraftPlaybook has the bot's provider write a draft from the messages picked in the chat; the
// draft is used once it is saved.
func (s *Store) DraftPlaybook(scope PlaybookScope, botID, chatID, kind string, messageIDs []string, done func(PlaybookRecord, error)) {
	if s.IsMock {
		s.later(1200*time.Millisecond, func() {
			content := mockDraftedSkill(kind)
			for _, skill := range s.Skills(scope) {
				if skill.Name == content.Name {
					content.Name += "-2"
				}
			}
			done(s.saveMockPlaybook(content, scope, nil, "draft"), nil)
		})
		return
	}
	params := map[string]any{"scope": scope, "bot_id": botID, "chat_id": chatID, "kind": kind, "message_ids": slices.Clone(messageIDs)}
	Async(s, func() (PlaybookRecord, error) {
		draft, err := call[struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			Instructions string `json:"instructions"`
		}](s, "skills.generate", params)
		content := PlaybookContent{Name: draft.Name, Description: draft.Description, Instructions: draft.Instructions, References: []PlaybookFile{}, Scripts: []PlaybookFile{}}
		// Nothing is saved yet: the sheet opens the draft, and Save writes the folder.
		return PlaybookRecord{Scope: scope, Status: "draft", Content: &content}, err
	}, done)
}

// ExportPlaybook is the skill as a zip of its folder, scrubbed of credentials: its files, nothing
// about the account.
func (s *Store) ExportPlaybook(scope PlaybookScope, id string, done func([]byte, error)) {
	indent := func(data []byte) ([]byte, error) {
		var out bytes.Buffer
		err := json.Indent(&out, data, "", "  ")
		return out.Bytes(), err
	}
	if s.IsMock {
		s.Playbook(scope, id, func(record PlaybookRecord, err error) {
			if err != nil {
				done(nil, err)
				return
			}
			data, _ := json.Marshal(map[string]any{"format": "lorca-playbook", "version": 1, "content": record.Content})
			done(indent(data))
		})
		return
	}
	// The CLI answers with a zip of the skill's folder, which any agent's skills folder takes.
	Async(s, func() ([]byte, error) {
		archive, err := call[struct {
			Data string `json:"data"`
		}](s, "skills.export", s.scoped(scope, map[string]any{"name": id}))
		if err != nil {
			return nil, err
		}
		return decodeArchive(archive.Data)
	}, done)
}

// saveMockPlaybook is the demo's save: the folder is the skill's name, and saving a skill over
// another name moves it.
func (s *Store) saveMockPlaybook(content PlaybookContent, scope PlaybookScope, over *PlaybookRecord, status string) PlaybookRecord {
	record := PlaybookRecord{ID: content.Name, Scope: scope, Status: status, Revision: 1, Hash: fmt.Sprintf("hash-%d", time.Now().UnixNano()), Content: &content}
	s.mockPlaybooks = slices.DeleteFunc(slices.Clone(s.mockPlaybooks), func(each PlaybookRecord) bool {
		return each.Scope == scope && (each.ID == record.ID || (over != nil && each.ID == over.ID))
	})
	if status != "draft" {
		s.mockPlaybooks = append(s.mockPlaybooks, record)
		s.mockPlaybooksChanged()
	}
	return record
}

func (s *Store) mockPlaybooksChanged() {
	s.Playbooks = s.Playbooks[:0:0]
	for _, record := range s.mockPlaybooks {
		s.Playbooks = append(s.Playbooks, record.summary())
	}
	s.emit(Event{Kind: EventRosterChanged})
}

// CaptureSources are the messages Save as Skill or Save as Standing Instruction offer around the
// one clicked: up to it, the user's and the bot's for a workflow, the user's alone for
// corrections, the last twelve. The clicked message starts picked, and for a workflow the request
// before it. The CLI checks the picks again.
func CaptureSources(chat *Chat, clicked *Message) (botID, kind string, sources []*Message, picked map[string]bool) {
	if chat == nil || clicked == nil || !clicked.CanBeQuoted() || strings.TrimSpace(clicked.Body.Text) == "" {
		return
	}
	botID, kind = clicked.Author.BotID, "workflow"
	if clicked.Author.Kind == AuthorYou {
		botID, kind = chat.Owner(), "corrections"
		if botID == "" && len(chat.BotIDs) > 0 {
			botID = chat.BotIDs[0]
		}
	}
	if !slices.Contains(chat.BotIDs, botID) {
		return "", "", nil, nil
	}
	for _, m := range chat.Messages {
		if !m.CanBeQuoted() || strings.TrimSpace(m.Body.Text) == "" {
			continue
		}
		if m.Author.Kind == AuthorYou || (kind == "workflow" && m.Author.BotID == botID) {
			sources = append(sources, m)
		}
		if m.ID == clicked.ID {
			break
		}
	}
	if len(sources) > 12 {
		sources = sources[len(sources)-12:]
	}
	picked = map[string]bool{clicked.ID: true}
	if kind == "workflow" {
		for i := len(sources) - 1; i >= 0; i-- {
			if sources[i].Author.Kind == AuthorYou {
				picked[sources[i].ID] = true
				break
			}
		}
	}
	return
}
