package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// openTestSheet puts a small sheet up over the main window and answers its tester.
func openTestSheet(t *testing.T, m *mainWindow, closed *int) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(m.frame(m.view), 1180, 820)
	settle(tt)
	m.present(func(c *ui.Context, s *sheet) {
		result := sheetFrame(c, sheetOptions{Title: "Centered sheet", Confirm: "Done"}, func() {
			ui.Text(c, "Sheet body")
		})
		if result.Cancelled || result.Confirmed {
			*closed++
			s.dismiss()
		}
	}, nil)
	settleTransitions(tt)
	return tt
}

func TestSheetHangsNearTheTop(t *testing.T) {
	m := demoWindow(t)
	var closed int
	tt := openTestSheet(t, m, &closed)

	title, ok := tt.Find("Centered sheet")
	if !ok {
		t.Fatalf("no sheet title in %q", tt.Texts())
	}
	done, ok := tt.Find("Done")
	if !ok {
		t.Fatalf("no Done button in %q", tt.Texts())
	}
	// The panel starts after the layer's 44 above it, not at the window's middle.
	top, bottom := title.Y-space5, done.Y+done.H+space5
	if got, want := top, float32(44); math.Abs(float64(got-want)) > 6 {
		t.Errorf("panel's top is at %v, want the window's %v", got, want)
	}
	if bottom > 820/2 {
		t.Errorf("panel's bottom is at %v, down the window as a centered sheet would be", bottom)
	}
	if got, want := title.X+title.W/2, float32(1180)/2; math.Abs(float64(got-want)) > 200 {
		t.Errorf("title is at %v, nowhere near the window's middle %v", got, want)
	}
}

func TestSheetClosesOnPressOutside(t *testing.T) {
	m := demoWindow(t)
	var closed int
	tt := openTestSheet(t, m, &closed)

	body, _ := tt.Find("Sheet body")
	tt.ClickAt(body.X+body.W/2, body.Y+body.H/2)
	settle(tt)
	if !m.hasSheet() || closed != 0 {
		t.Fatalf("a press inside the panel closed the sheet (sheet %v, closed %d)", m.hasSheet(), closed)
	}

	tt.ClickAt(30, 400)
	settle(tt)
	if m.hasSheet() || closed != 1 {
		t.Errorf("a press outside left the sheet up (sheet %v, closed %d)", m.hasSheet(), closed)
	}
}

func TestAlertStaysOnPressOutside(t *testing.T) {
	m := demoWindow(t)
	tt := ui.NewTester(m.frame(m.view), 1180, 820)
	settle(tt)
	answered := -2
	m.showAlert(alertOptions{Message: "Delete the bot?", Buttons: []alertButton{{Title: "Delete", Destructive: true}, {Title: "Cancel"}}}, func(i int) { answered = i })
	settleTransitions(tt)

	tt.ClickAt(30, 400)
	settle(tt)
	if !m.hasSheet() || answered != -2 {
		t.Fatalf("a press outside closed the alert (sheet %v, answered %d)", m.hasSheet(), answered)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if m.hasSheet() || answered != 1 {
		t.Errorf("Cancel left the alert up or answered %d, want 1", answered)
	}
}
