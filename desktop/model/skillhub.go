package model

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Skills from public communities, through the CLI: it searches skills.sh and fetches a skill from
// skills.sh, ClawHub, or GitHub into the bot's, the group's, or every bot's skills.

// SkillHit is a skill a community lists. Reference is what Import takes back; Owner is the
// repository it lives in, and Installs how many times the skills CLI has installed it.
type SkillHit struct {
	Source    string `json:"source"`
	Reference string `json:"reference"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Owner     string `json:"owner"`
	Installs  int64  `json:"installs"`
	URL       string `json:"url"`
	// Duplicate is a copy of another repository's skill, as skills.sh marks it.
	Duplicate bool `json:"duplicate"`
}

// SkillHold is what a scope has of a listed skill: nothing, that very skill, or another skill
// under its name, which an import would refuse.
type SkillHold int

const (
	SkillNotHeld SkillHold = iota
	SkillHeld
	SkillNameTaken
)

// Holds is what `skills` (a scope's) has of the skill a reference names: the skill imported from
// that address, or another skill whose folder has its name.
func Holds(skills []PlaybookSummary, reference string) SkillHold {
	name := ReferenceName(reference)
	hold := SkillNotHeld
	for _, skill := range skills {
		switch {
		case skill.Source == reference:
			return SkillHeld
		case skill.Name == name:
			hold = SkillNameTaken
		}
	}
	return hold
}

// SkillSearchMin is the shortest query skills.sh answers.
const SkillSearchMin = 2

// ReferenceName is the skill's own name in a reference: `pdf` in `clawhub:owner/pdf` and in
// `owner/repo@pdf`.
func ReferenceName(reference string) string {
	if i := strings.LastIndexAny(reference, ":/@"); i >= 0 {
		return reference[i+1:]
	}
	return reference
}

// SearchSkills lists the skills on skills.sh matching the query; a query shorter than
// SkillSearchMin lists none.
func (s *Store) SearchSkills(query string, done func([]SkillHit, error)) {
	if s.IsMock {
		hits := mockSkillHits()
		query = strings.ToLower(strings.TrimSpace(query))
		hits = slices.DeleteFunc(hits, func(hit SkillHit) bool {
			return len([]rune(query)) < SkillSearchMin || !strings.Contains(strings.ToLower(hit.Name+" "+hit.Owner), query)
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

// ImportSkill adds the skill `source` names (a ClawHub, skills.sh, or GitHub address) to the scope,
// folder and all, so it is used from the next turn. skipped lists the files of the skill that did
// not come across.
// `via` is the bot whose Runner keeps a shared skill, or "" for this computer.
func (s *Store) ImportSkill(scope PlaybookScope, via, source string, done func(PlaybookRecord, []string, error)) {
	if s.IsMock {
		s.later(600*time.Millisecond, func() {
			content := PlaybookContent{Name: ReferenceName(source), Description: "A skill from a community.", Instructions: "Follow the steps in the skill."}
			if Holds(s.Skills(scope), source) != SkillNotHeld {
				done(PlaybookRecord{}, nil, errors.New("A skill with this name already exists in this scope"))
				return
			}
			record := s.saveMockPlaybook(content, scope, nil, "saved")
			record.Source = source
			for i := range s.mockPlaybooks {
				if s.mockPlaybooks[i].ID == record.ID && s.mockPlaybooks[i].Scope == scope {
					s.mockPlaybooks[i].Source = source
				}
			}
			s.mockPlaybooksChanged()
			done(record, []string{"scripts/browser-steps.json"}, nil)
		})
		return
	}
	params := s.scopedVia(scope, via, map[string]any{"source": source})
	type imported struct {
		skillView
		Skipped []string `json:"skipped"`
	}
	Async(s, func() (imported, error) { return call[imported](s, "skills.import", params) }, func(result imported, err error) {
		if err == nil {
			s.invalidateSkills()
		}
		done(result.skillView.record(), result.Skipped, err)
	})
}

func mockSkillHits() []SkillHit {
	hit := func(repo, name string, installs int64) SkillHit {
		return SkillHit{Source: "skills-sh", Reference: repo + "@" + name, Name: name, Owner: repo, Installs: installs, URL: "https://skills.sh/" + repo + "/" + name}
	}
	return []SkillHit{
		hit("anthropics/skills", "pdf", 210224),
		hit("anthropics/skills", "docx", 200892),
		hit("anthropics/skills", "webapp-testing", 177038),
		hit("vercel-labs/agent-skills", "vercel-react-best-practices", 799510),
		hit("vercel-labs/agent-skills", "web-design-guidelines", 412003),
		hit("obra/superpowers", "test-driven-development", 96120),
		hit("obra/superpowers", "systematic-debugging", 88410),
	}
}

// InstallsText is how many installs a skill has, short: 1.2K, 3.4M.
func InstallsText(n int64) string {
	switch {
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1_000_000, 'f', 1, 64) + "M"
	case n >= 1_000:
		return strconv.FormatFloat(float64(n)/1_000, 'f', 1, 64) + "K"
	}
	return strconv.FormatInt(n, 10)
}
