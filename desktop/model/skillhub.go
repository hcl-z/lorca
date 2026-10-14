package model

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// Skills from public communities, through the CLI: it searches ClawHub and fetches a skill from
// ClawHub, skills.sh, or GitHub into the bot's or the group's skills.

// SkillHit is a skill a community lists. Reference is what Import takes back.
type SkillHit struct {
	Source    string `json:"source"`
	Reference string `json:"reference"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Owner     string `json:"owner"`
	Version   string `json:"version"`
	URL       string `json:"url"`
}

// ReferenceName is the skill's own name in a reference: `pdf` in `clawhub:owner/pdf` and in
// `owner/repo@pdf`.
func ReferenceName(reference string) string {
	if i := strings.LastIndexAny(reference, ":/@"); i >= 0 {
		return reference[i+1:]
	}
	return reference
}

// SearchSkills lists the community's skills matching the query, or its most downloaded when the
// query is empty.
func (s *Store) SearchSkills(query string, done func([]SkillHit, error)) {
	if s.IsMock {
		hits := mockSkillHits()
		query = strings.ToLower(strings.TrimSpace(query))
		hits = slices.DeleteFunc(hits, func(hit SkillHit) bool {
			return query != "" && !strings.Contains(strings.ToLower(hit.Name+" "+hit.Summary), query)
		})
		s.later(300*time.Millisecond, func() { done(hits, nil) })
		return
	}
	params := map[string]any{"query": query}
	Async(s, func() ([]SkillHit, error) {
		result, err := call[struct {
			Results []SkillHit `json:"results"`
		}](s, "skills.search", params)
		return result.Results, err
	}, done)
}

// ImportSkill adds the skill `source` names (a ClawHub, skills.sh, or GitHub address) to the scope:
// saved, so it is used from the next turn, when `activate`, else as a draft to review. skipped lists
// the files of the skill that did not come across.
func (s *Store) ImportSkill(scope PlaybookScope, source string, activate bool, done func(PlaybookRecord, []string, error)) {
	if s.IsMock {
		s.later(600*time.Millisecond, func() {
			content := PlaybookContent{Name: ReferenceName(source), Description: "A skill from a community.", Instructions: "Follow the steps in the skill."}
			for _, skill := range s.Skills(scope) {
				if skill.Name == content.Name {
					done(PlaybookRecord{}, nil, errors.New("A skill with this name already exists in this scope"))
					return
				}
			}
			status := "draft"
			if activate {
				status = "saved"
			}
			done(s.saveMockPlaybook(content, scope, nil, status), []string{"assets/logo.png"}, nil)
		})
		return
	}
	params := map[string]any{"scope": scope, "source": source, "activate": activate}
	type imported struct {
		PlaybookRecord
		Skipped []string `json:"skipped"`
	}
	Async(s, func() (imported, error) { return call[imported](s, "skills.import", params) }, func(result imported, err error) {
		done(result.PlaybookRecord, result.Skipped, err)
	})
}

func mockSkillHits() []SkillHit {
	return []SkillHit{
		{Source: "clawhub", Reference: "clawhub:openclaw/pdf-tools", Name: "PDF Tools", Summary: "Read, fill, and merge PDF files.", Owner: "openclaw", Version: "1.2.0", URL: "https://clawhub.ai/openclaw/skills/pdf-tools"},
		{Source: "clawhub", Reference: "clawhub:steipete/code-review", Name: "Code Review", Summary: "Review a change for bugs, security issues, and missing tests.", Owner: "steipete", Version: "0.9.1", URL: "https://clawhub.ai/steipete/skills/code-review"},
		{Source: "clawhub", Reference: "clawhub:openclaw/weekly-report", Name: "Weekly Report", Summary: "Compare the week's evidence and write the report.", Owner: "openclaw", Version: "1.0.0", URL: "https://clawhub.ai/openclaw/skills/weekly-report"},
		{Source: "clawhub", Reference: "clawhub:steipete/gifgrep", Name: "GifGrep", Summary: "Find a GIF for the moment.", Owner: "steipete", Version: "1.2.3", URL: "https://clawhub.ai/steipete/skills/gifgrep"},
	}
}
