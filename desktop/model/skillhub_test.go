package model

import (
	"encoding/json"
	"testing"
)

func TestReferenceName(t *testing.T) {
	for reference, want := range map[string]string{
		"clawhub:pdf":              "pdf",
		"clawhub:awspace/pdf":      "pdf",
		"lobehub/lobehub@react":    "react",
		"pdf":                      "pdf",
		"clawhub:owner/skill-name": "skill-name",
	} {
		if got := ReferenceName(reference); got != want {
			t.Errorf("ReferenceName(%q) = %q, want %q", reference, got, want)
		}
	}
}

// Add copies the skill's folder and saves it at once.
func TestImportSkillSavesTheFolder(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"scope":{"kind":"bot","id":"bot-one"},"name":"pdf-tools","description":"Fill PDFs","instructions":"Use the scripts.","hash":"h1","files":[],"skipped":["scripts/browser-steps.json"]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	var record PlaybookRecord
	var skipped []string
	s.ImportSkill(BotScope("bot-one"), "", "clawhub:pdf-tools", func(r PlaybookRecord, files []string, err error) {
		if err != nil {
			t.Error(err)
		}
		record, skipped = r, files
	})
	waitPosts(posts)
	if transport.method != "skills.import" || transport.params["source"] != "clawhub:pdf-tools" {
		t.Fatalf("import params %q %v", transport.method, transport.params)
	}
	if _, asked := transport.params["activate"]; asked {
		t.Errorf("an import is always saved: %v", transport.params)
	}
	if record.ID != "pdf-tools" || record.Status != "saved" || record.Content.Instructions != "Use the scripts." || len(skipped) != 1 {
		t.Errorf("record %+v, skipped %v", record, skipped)
	}
}

func TestSearchSkills(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"results":[{"source":"skills-sh","reference":"anthropics/skills@pdf","name":"pdf","owner":"anthropics/skills","installs":210224}]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	var hits []SkillHit
	s.SearchSkills(" pdf ", func(found []SkillHit, err error) {
		if err != nil {
			t.Error(err)
		}
		hits = found
	})
	waitPosts(posts)
	if transport.method != "skills.search" || transport.params["query"] != " pdf " {
		t.Fatalf("search params %q %v", transport.method, transport.params)
	}
	if len(hits) != 1 || hits[0].Reference != "anthropics/skills@pdf" || hits[0].Installs != 210224 {
		t.Errorf("hits %+v", hits)
	}
	if got := InstallsText(hits[0].Installs); got != "210.2K" {
		t.Errorf("installs %q", got)
	}
}

// A listed skill is Added only when the scope's skill came from that very address; another skill
// of the same name only takes the name.
func TestHolds(t *testing.T) {
	skills := []PlaybookSummary{
		{Name: "github", Source: "clawhub:steipete/github"},
		{Name: "pdf"},
	}
	for reference, want := range map[string]SkillHold{
		"clawhub:steipete/github": SkillHeld,
		"clawhub:eohmig/github":   SkillNameTaken,
		"anthropics/skills@pdf":   SkillNameTaken,
		"anthropics/skills@docx":  SkillNotHeld,
	} {
		if got := Holds(skills, reference); got != want {
			t.Errorf("Holds(%q) = %v, want %v", reference, got, want)
		}
	}
}

// A shared skill added from the sidebar stays on this computer; one added for a bot's Runner names
// that bot, which the CLI routes by.
func TestSharedSkillsNameTheirRunner(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"scope":{"kind":"shared","id":""},"name":"pdf","description":"","instructions":"x","hash":"h","files":[]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	s.ImportSkill(SharedScope(), "", "anthropics/skills@pdf", func(PlaybookRecord, []string, error) {})
	waitPosts(posts)
	if _, named := transport.params["bot_id"]; named {
		t.Errorf("this computer's shared skills name no bot: %v", transport.params)
	}
	s.ImportSkill(SharedScope(), "bot-one", "anthropics/skills@pdf", func(PlaybookRecord, []string, error) {})
	waitPosts(posts)
	if transport.params["bot_id"] != "bot-one" {
		t.Errorf("a Runner's shared skills name its bot: %v", transport.params)
	}
	s.ImportSkill(BotScope("bot-one"), "bot-two", "anthropics/skills@pdf", func(PlaybookRecord, []string, error) {})
	waitPosts(posts)
	if _, named := transport.params["bot_id"]; named {
		t.Errorf("a bot's own skill is routed by its scope: %v", transport.params)
	}
}
