package main

import (
	"testing"

	"github.com/egoist/lorca/desktop/model"
)

func tool(kind, title, detail, status string) *model.AgentEntry {
	return &model.AgentEntry{Kind: "tool", Tool: &model.AgentTool{Kind: kind, Title: title, Detail: detail, Status: status}}
}

func TestExploredTextFoldsReadsAndSearches(t *testing.T) {
	cases := []struct {
		name  string
		items []*model.AgentEntry
		want  string
	}{
		{"one file", []*model.AgentEntry{tool("read", "src/a.rs", "", "completed")}, "Explored 1 file"},
		{"files", []*model.AgentEntry{tool("read", "src/a.rs", "", "completed"), tool("read", "src/b.rs", "", "completed")}, "Explored 2 files"},
		{"search", []*model.AgentEntry{tool("search", "login", "", "completed")}, "Explored 1 search"},
		{"both", []*model.AgentEntry{tool("read", "src/a.rs", "", "completed"), tool("search", "login", "", "completed")}, "Explored 1 file 1 search"},
		{"running", []*model.AgentEntry{tool("read", "src/a.rs", "", "in_progress")}, "Exploring 1 file"},
	}
	for _, c := range cases {
		if got := exploredText(c.items); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAgentStepsFoldsConsecutiveExplores(t *testing.T) {
	entries := []model.AgentEntry{
		*tool("read", "src/a.rs", "", "completed"),
		*tool("search", "login", "", "completed"),
		*tool("edit", "src/a.rs", "", "completed"),
		*tool("read", "src/b.rs", "", "completed"),
	}
	steps := agentSteps(entries)
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	if len(steps[0].explored) != 2 || steps[1].explored != nil || len(steps[2].explored) != 1 {
		t.Fatalf("unexpected fold: %#v", steps)
	}
}

func TestAgentToolTextUsesTenseAndDropsADoubleVerb(t *testing.T) {
	cases := []struct {
		name string
		item *model.AgentEntry
		want string
	}{
		{"ran", tool("execute", "bun test", "", "completed"), "Ran bun test"},
		{"running", tool("execute", "bun test", "", "in_progress"), "Running bun test"},
		{"edited", tool("edit", "Edit src/a.rs", "", "completed"), "Edited src/a.rs"},
		{"prefers detail", tool("edit", "Edit", "src/a.rs", "completed"), "Edited src/a.rs"},
		{"failed", tool("execute", "bun test", "", "failed"), "Failed: bun test"},
	}
	for _, c := range cases {
		if got := agentToolText(c.item.Tool); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
