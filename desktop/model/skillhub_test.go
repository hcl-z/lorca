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

// Add saves the skill at once; reviewing a draft first is the other way.
func TestImportSkillSaysWhetherToActivate(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"id":"playbook-one","scope":{"kind":"bot","id":"bot-one"},"status":"saved","revision":1,"hash":"h1","content":{"name":"pdf-tools"},"skipped":["assets/logo.png"]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	var record PlaybookRecord
	var skipped []string
	s.ImportSkill(BotScope("bot-one"), "clawhub:pdf-tools", true, func(r PlaybookRecord, files []string, err error) {
		if err != nil {
			t.Error(err)
		}
		record, skipped = r, files
	})
	waitPosts(posts)
	if transport.method != "skills.import" || transport.params["source"] != "clawhub:pdf-tools" || transport.params["activate"] != true {
		t.Fatalf("import params %q %v", transport.method, transport.params)
	}
	if record.ID != "playbook-one" || record.Status != "saved" || len(skipped) != 1 {
		t.Errorf("record %+v, skipped %v", record, skipped)
	}

	s.ImportSkill(BotScope("bot-one"), "owner/repo", false, func(PlaybookRecord, []string, error) {})
	waitPosts(posts)
	if transport.params["activate"] != false {
		t.Errorf("a draft import params %v", transport.params)
	}
}

func TestSearchSkills(t *testing.T) {
	posts := make(chan func(), 4)
	transport := &playbookTransport{reply: json.RawMessage(`{"results":[{"source":"clawhub","reference":"clawhub:gifgrep","name":"GifGrep"}]}`)}
	s := NewStore(transport, func(fn func()) { posts <- fn }, false)
	var hits []SkillHit
	s.SearchSkills(" gif ", func(found []SkillHit, err error) {
		if err != nil {
			t.Error(err)
		}
		hits = found
	})
	waitPosts(posts)
	if transport.method != "skills.search" || transport.params["query"] != " gif " {
		t.Fatalf("search params %q %v", transport.method, transport.params)
	}
	if len(hits) != 1 || hits[0].Reference != "clawhub:gifgrep" || hits[0].Name != "GifGrep" {
		t.Errorf("hits %+v", hits)
	}
}
