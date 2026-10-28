package model

import "slices"

// AgentEntry is one part of a coding agent's transcript as the Runner keeps it for the apps:
// what it was sent, what it said, a tool it used with its command's output and its edits, its
// plan, or a note of Lorca's. Seq is its place, kept when it changes; Rev the change it was last
// part of.
type AgentEntry struct {
	Seq  uint64 `json:"seq"`
	Rev  uint64 `json:"rev"`
	Kind string `json:"kind"` // sent, said, thought, tool, plan, note, notice, compaction, log
	Text string `json:"text"`
	// Level is a notice's severity: info, warning, or error. Status is how a compaction stands:
	// in_progress, completed, failed, or cancelled.
	Level  string `json:"level"`
	Status string `json:"status"`
	// Seconds is how long a thought took, when the agent says.
	Seconds *float64 `json:"seconds"`
	// From is `user` on what the user sent it, empty on what the bot sent.
	From  string      `json:"from"`
	Tool  *AgentTool  `json:"tool"`
	Steps []AgentStep `json:"steps"`
}

// AgentTool is a tool call: Output is what its command's terminal wrote, with only its colors.
type AgentTool struct {
	Title string `json:"title"`
	// Name is the tool's own, when the agent gives it: Bash, an MCP tool's.
	Name     string          `json:"name"`
	Detail   string          `json:"detail"`
	Kind     string          `json:"kind"`
	Status   string          `json:"status"` // pending, in_progress, completed, failed
	Output   string          `json:"output"`
	ExitCode *int            `json:"exit_code"`
	Result   string          `json:"result"`
	Diffs    []AgentFileDiff `json:"diffs"`
}

// Running is whether it has not ended yet.
func (t *AgentTool) Running() bool { return t.Status == "pending" || t.Status == "in_progress" }

// Failed is whether it ended in error, or its command did.
func (t *AgentTool) Failed() bool {
	return t.Status == "failed" || (t.ExitCode != nil && *t.ExitCode != 0)
}

// AgentFileDiff is an edit: Lines are each "+", "-", or " " and the line, with "@@" between parts.
type AgentFileDiff struct {
	Path    string   `json:"path"`
	Lines   []string `json:"lines"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
}

type AgentStep struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// AgentTranscriptPage is what `coding.transcript` answers: the bot's text transcript, the entries
// changed after the change asked from, the latest change, and the first entry the Runner keeps.
type AgentTranscriptPage struct {
	Text    string       `json:"text"`
	Card    *AgentRun    `json:"card"`
	Entries []AgentEntry `json:"entries"`
	Rev     uint64       `json:"rev"`
	First   uint64       `json:"first"`
}

// MergeAgentEntries puts a page's entries into what an app has: each in place of the one with its
// seq, or in order after the rest, and none from before the first the Runner keeps. It answers
// whether anything changed.
func MergeAgentEntries(have []AgentEntry, page AgentTranscriptPage) ([]AgentEntry, bool) {
	changed := false
	for _, entry := range page.Entries {
		at, found := slices.BinarySearchFunc(have, entry.Seq, func(e AgentEntry, seq uint64) int {
			switch {
			case e.Seq < seq:
				return -1
			case e.Seq > seq:
				return 1
			}
			return 0
		})
		if found {
			have[at] = entry
		} else {
			have = slices.Insert(have, at, entry)
		}
		changed = true
	}
	start := 0
	for start < len(have) && have[start].Seq < page.First {
		start++
	}
	if start > 0 {
		have, changed = slices.Clone(have[start:]), true
	}
	return have, changed
}

// MockAgentEntries are the demo's agent's transcript, as the apps draw it.
func MockAgentEntries() []AgentEntry {
	zero, one := 0, 1
	return []AgentEntry{
		{Seq: 0, Kind: "sent", Text: "Fix the broken docs link on the download page, with a test."},
		{Seq: 1, Kind: "said", Text: "I'll find the link first."},
		{Seq: 2, Kind: "tool", Tool: &AgentTool{Title: "Grep", Detail: "docs/install", Kind: "search", Status: "completed", Result: "src/pages/download.astro:42"}},
		{Seq: 3, Kind: "tool", Tool: &AgentTool{Title: "Edit src/pages/download.astro", Kind: "edit", Status: "completed", Diffs: []AgentFileDiff{{
			Path: "src/pages/download.astro", Added: 1, Removed: 1,
			Lines: []string{"   <p>", `-    Read the <a href="/docs/instal">install guide</a>.`, `+    Read the <a href="/docs/install">install guide</a>.`, "   </p>"},
		}}}},
		{Seq: 4, Kind: "tool", Tool: &AgentTool{Title: "bun test links", Kind: "execute", Status: "failed", ExitCode: &one, Output: "\x1b[1mbun test\x1b[0m v1.2.0\r\n\x1b[31m✗\x1b[0m /docs/instal answers 404\r\n\r\n 13 pass\r\n \x1b[31m1 fail\x1b[0m\r\n"}},
		{Seq: 5, Kind: "note", Text: "Not allowed: pushing waits for the user"},
		{Seq: 6, Kind: "plan", Steps: []AgentStep{{Text: "Find the broken link", Done: true}, {Text: "Fix it", Done: true}, {Text: "Run the link test", Done: false}}},
		{Seq: 7, Kind: "tool", Tool: &AgentTool{Title: "bun test links", Kind: "execute", Status: "completed", ExitCode: &zero, Output: "\x1b[1mbun test\x1b[0m v1.2.0\r\n\r\n \x1b[32m14 pass\x1b[0m\r\n 0 fail\r\n"}},
		{Seq: 8, Kind: "said", Text: "Fixed the link to **/docs/install**, and the link test passes."},
	}
}
