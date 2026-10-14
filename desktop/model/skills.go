package model

import (
	"encoding/base64"
	"slices"
	"strings"
	"time"
)

// Skills are folders on a Runner: the CLI lists them for a bot in a chat, and a skill's body and
// files are fetched when one opens. The sheet edits them as it edited the playbooks they replace.

// skillsFresh is how long a list stands before the next draw of the Skills section asks again,
// since a bot writes its skills as files.
const skillsFresh = 15 * time.Second

type skillRow struct {
	Scope       PlaybookScope `json:"scope"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Path        string        `json:"path"`
	Builtin     bool          `json:"builtin"`
	Source      string        `json:"source"`
	UpdatedAt   float64       `json:"updated_at"`
}

func (r skillRow) summary() PlaybookSummary {
	return PlaybookSummary{ID: r.Name, Scope: r.Scope, Name: r.Name, Description: r.Description, Status: "saved", Revision: 1,
		UpdatedAt: r.UpdatedAt, Path: r.Path, Builtin: r.Builtin, Source: r.Source}
}

type skillList struct {
	Skills []skillRow `json:"skills"`
}

// skillView is one skill as `skills.get` and `skills.save` answer: its instructions, and its files
// beside SKILL.md with their text where they have any.
type skillView struct {
	skillRow
	Instructions string `json:"instructions"`
	Hash         string `json:"hash"`
	Files        []struct {
		Path string  `json:"path"`
		Size int64   `json:"size"`
		Text *string `json:"text"`
	} `json:"files"`
}

// record is the skill as the sheet holds it: files under references/ and scripts/ in their tabs,
// the other text files kept to be sent back. A file with no text, a binary or a very large one, is
// the CLI's to keep.
func (v skillView) record() PlaybookRecord {
	content := PlaybookContent{Name: v.Name, Description: v.Description, Instructions: v.Instructions,
		References: []PlaybookFile{}, Scripts: []PlaybookFile{}}
	for _, file := range v.Files {
		if file.Text == nil {
			continue
		}
		entry := PlaybookFile{Path: file.Path, Text: *file.Text}
		switch {
		case strings.HasPrefix(file.Path, "references/"):
			content.References = append(content.References, entry)
		case strings.HasPrefix(file.Path, "scripts/"):
			content.Scripts = append(content.Scripts, entry)
		default:
			content.Other = append(content.Other, entry)
		}
	}
	return PlaybookRecord{ID: v.Name, Scope: v.Scope, Status: "saved", Revision: 1, Hash: v.Hash, Content: &content, Path: v.Path, Builtin: v.Builtin, Source: v.Source}
}

// skillInput is what a save sends: the examples go into the instructions, since a skill has one
// body, and every text file goes back, the ones the sheet does not show included.
func (c PlaybookContent) skillInput() map[string]any {
	instructions := c.Instructions
	if examples := strings.TrimSpace(c.Examples); examples != "" {
		instructions += "\n\n## Examples\n\n" + examples
	}
	files := []map[string]string{}
	for _, group := range [][]PlaybookFile{c.References, c.Scripts, c.Other} {
		for _, file := range group {
			files = append(files, map[string]string{"path": file.Path, "text": file.Text})
		}
	}
	return map[string]any{"name": c.Name, "description": c.Description, "instructions": instructions, "files": files}
}

// SharedSkills are the skills every bot on the Runner uses, other than the ones Lorca ships.
func (s *Store) SharedSkills() []PlaybookSummary {
	var skills []PlaybookSummary
	for _, skill := range s.Skills(SharedScope()) {
		if !skill.Builtin {
			skills = append(skills, skill)
		}
	}
	return skills
}

// EnsureSkills lists the skills of a bot in a chat when the list is missing or old. The Skills
// section calls it as it draws; the answer redraws it.
func (s *Store) EnsureSkills(botID, chatID string) {
	if s.IsMock || botID == "" {
		return
	}
	key := [2]string{botID, chatID}
	if s.skillsKey == key && (s.skillsLoading || time.Since(s.skillsAt) < skillsFresh) {
		return
	}
	s.skillsKey = key
	s.fetchSkills()
}

// invalidateSkills lists them again at once, after a change.
func (s *Store) invalidateSkills() {
	s.skillsAt = time.Time{}
	s.fetchSkills()
}

// fetchSkills asks for the skills of the bot and chat last shown, and puts them in place of the
// ones this Device held for the same bot, group, and the shared folder.
func (s *Store) fetchSkills() {
	if s.IsMock || s.skillsKey[0] == "" || s.skillsLoading {
		return
	}
	key := s.skillsKey
	s.skillsLoading = true
	Async(s, func() (skillList, error) {
		return call[skillList](s, "skills.list", map[string]any{"bot_id": key[0], "chat_id": key[1]})
	}, func(list skillList, err error) {
		s.skillsLoading = false
		s.skillsAt = time.Now()
		if err != nil {
			return
		}
		covered := []PlaybookScope{SharedScope(), BotScope(key[0]), GroupScope(key[1])}
		s.Playbooks = slices.DeleteFunc(slices.Clone(s.Playbooks), func(each PlaybookSummary) bool { return slices.Contains(covered, each.Scope) })
		for _, row := range list.Skills {
			s.Playbooks = append(s.Playbooks, row.summary())
		}
		s.emit(Event{Kind: EventRosterChanged})
	})
}

func decodeArchive(data string) ([]byte, error) { return base64.StdEncoding.DecodeString(data) }

// SkillsBot is the bot whose skills the Skills section last showed: the shared skills it lists are
// that bot's Runner's.
func (s *Store) SkillsBot() string { return s.skillsKey[0] }

// scoped is a call's parameters with its scope, and for the shared skills the bot on screen, so the
// CLI sends the call to that bot's Runner rather than this Device's.
func (s *Store) scoped(scope PlaybookScope, params map[string]any) map[string]any {
	return s.scopedVia(scope, s.skillsKey[0], params)
}

// scopedVia is scoped with the shared skills of `via`'s Runner, or this computer's for none.
func (s *Store) scopedVia(scope PlaybookScope, via string, params map[string]any) map[string]any {
	params["scope"] = scope
	if scope.IsShared() && via != "" {
		params["bot_id"] = via
	}
	return params
}
