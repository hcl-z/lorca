package main

import (
	"testing"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// TestTheAgentWindowFloatsMovesAndFillsTheWindow draws the developer's chat with its agent at work:
// the agent's window floats over it, moves when its title is dragged, fills the window when its
// body is clicked, goes back on Escape, and is gone once the agent is done.
func TestTheAgentWindowFloatsMovesAndFillsTheWindow(t *testing.T) {
	m := demoWindow(t)
	m.pips.off = false
	m.selectChat("chat-patch")
	chat := store.Chat("chat-patch")
	var rowID string
	for _, message := range chat.Messages {
		if model.AgentOf(message) != nil {
			rowID = message.ID
		}
	}
	if rowID == "" {
		t.Fatal("the demo has no agent card")
	}
	setSettings := func(agent *model.AgentRun) {
		agent.Settings = []model.AgentSetting{{Name: "Model", Value: "Opus"}, {Name: "Effort", Value: "High"}, {Name: "Context", Value: "34k / 200k"}}
	}
	store.Update(rowID, "chat-patch", func(message *model.Message) {
		tool := *message.Body.Tool
		agent := *tool.Agent
		setSettings(&agent)
		tool.Agent = &agent
		message.Body.Tool = &tool
	})
	tt := ui.NewTester(m.frame(m.view), 1180, 760)
	settle(tt)

	st := m.pips.byID[rowID]
	if st == nil || !st.placed || st.full {
		t.Fatalf("no floating window for the agent at work: %+v", st)
	}
	renderBoth(t, tt, "agent-window")
	// It sits whole inside the window, however tall its content is.
	if st.height == 0 || st.y < 0 || st.y+st.height > 760 || st.x+pipWidth > 1180 {
		t.Errorf("the window is not inside the app window: at (%v, %v), %v tall", st.x, st.y, st.height)
	}

	// Dragging its title moves it, and it stays in the window.
	x, y := st.x, st.y
	tt.Press(x+60, y+pipBarHeight/2)
	for step := 1; step <= 4; step++ {
		tt.Move(x+60-float32(step)*60, y+pipBarHeight/2-float32(step)*40)
		settle(tt)
	}
	tt.Release(x+60-240, y+pipBarHeight/2-160)
	settle(tt)
	if st.x >= x-100 || st.y >= y-60 {
		t.Errorf("the window did not follow the drag: from (%v, %v) to (%v, %v)", x, y, st.x, st.y)
	}
	if st.x < 0 || st.y < headerHeight {
		t.Errorf("the window left the window: (%v, %v)", st.x, st.y)
	}
	if st.full {
		t.Error("a drag is not a click")
	}
	renderBoth(t, tt, "agent-window-moved")

	// Dragged past the bottom and the right edge, it stops at them.
	startX, startY := st.x+60, st.y+pipBarHeight/2
	tt.Press(startX, startY)
	for step := 1; step <= 6; step++ {
		tt.Move(startX+float32(step)*150, startY+float32(step)*150)
		settle(tt)
	}
	tt.Release(startX+900, startY+900)
	settle(tt)
	if st.y+st.height > 760 || st.x+pipWidth > 1180 {
		t.Errorf("the window left the app window: at (%v, %v), %v tall", st.x, st.y, st.height)
	}
	if st.full {
		t.Error("a drag is not a click")
	}

	// A click on its body fills the window with the agent.
	tt.ClickAt(st.x+pipWidth/2, st.y+pipBarHeight+14)
	settle(tt)
	if !st.full {
		t.Fatal("a click on its body did not open it full screen")
	}
	renderBoth(t, tt, "agent-full")
	tt.Key(0, ui.KeyEscape)
	settle(tt)
	if st.full {
		t.Error("Escape did not bring the window back")
	}

	// A question shows in the window, so it can be answered from any chat.
	store.Update(rowID, "chat-patch", func(message *model.Message) {
		tool := *message.Body.Tool
		agent := *tool.Agent
		agent.State = model.AgentAsking
		agent.Question = &model.AgentQuestion{Kind: "command", Command: "git push origin fix-docs-link", Reason: "Pushing sends the branch to GitHub.", Rule: "push branches to GitHub", HasRule: true}
		tool.Agent = &agent
		message.Body.Tool = &tool
	})
	settle(tt)
	renderBoth(t, tt, "agent-window-asking")
	if got := countLines(tt, L("Allow once")); got < 2 {
		t.Errorf("Allow once is on the card and in the window, found %d", got)
	}

	// Done, the agent gives its window up.
	store.Update(rowID, "chat-patch", func(message *model.Message) {
		tool := *message.Body.Tool
		agent := *tool.Agent
		agent.State, agent.Question = model.AgentIdle, nil
		tool.Agent = &agent
		message.Body.Tool = &tool
	})
	settle(tt)
	if m.pips.byID[rowID] != nil {
		t.Error("an agent that is done keeps its window")
	}
}
