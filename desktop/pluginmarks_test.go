package main

import "testing"

// Every plugin mark is an SVG the toolkit reads, and a plugin without one has none.
func TestPluginMarksParse(t *testing.T) {
	for id := range pluginMarks {
		if pluginMark(id) == nil {
			t.Errorf("the mark of %s does not parse", id)
		}
	}
	if len(pluginMarks) == 0 || pluginMark("no-such-plugin") != nil {
		t.Errorf("%d marks", len(pluginMarks))
	}
}
