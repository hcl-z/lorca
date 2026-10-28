package model

import "testing"

func TestMergeAgentEntriesPutsAnOlderPageInFrontAndDropsWhatTheRunnerDropped(t *testing.T) {
	said := func(seq uint64) AgentEntry { return AgentEntry{Seq: seq, Rev: seq, Kind: "said", Text: "line"} }
	have, changed := MergeAgentEntries(nil, AgentTranscriptPage{Entries: []AgentEntry{said(5), said(6)}})
	if !changed || len(have) != 2 || have[0].Seq != 5 || have[1].Seq != 6 {
		t.Fatalf("newest page: %v, changed %v", have, changed)
	}
	have, _ = MergeAgentEntries(have, AgentTranscriptPage{Entries: []AgentEntry{said(3), said(4)}})
	if want := []uint64{3, 4, 5, 6}; len(have) != len(want) || have[0].Seq != want[0] || have[3].Seq != want[3] {
		t.Fatalf("older page in front: %v", have)
	}
	// The Runner moved past what the app had: those entries go.
	have, changed = MergeAgentEntries(have, AgentTranscriptPage{First: 5})
	if !changed || len(have) != 2 || have[0].Seq != 5 || have[1].Seq != 6 {
		t.Fatalf("bounded by first: %v, changed %v", have, changed)
	}
}
