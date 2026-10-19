package main

import (
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/ui"
)

// Edits the account's PROFILE.md on one Runner: what every bot there knows about the user. A
// save carries the hash of the text that was opened, so it is refused when a bot changed the
// profile in between; the user then reloads theirs or overwrites knowingly.

type profileState struct {
	runnerID string
	profile  model.Profile
	text     string
	saving   bool
}

// presentProfile edits the user's profile on a Runner; onSaved runs once it is saved.
func (w *appWindow) presentProfile(runnerID, runnerName string, profile model.Profile, onSaved func()) {
	st := &profileState{runnerID: runnerID, profile: profile, text: profile.Text}
	subtitle := L("Every bot on %@ reads this: who you are, how you like replies, and facts that hold in every chat. Bots add to it when you tell them something about yourself; you can change anything here.", runnerName)
	w.present(func(c *ui.Context, s *sheet) {
		p := colors(c)
		title := L("About you")
		result := sheetFrame(c, sheetOptions{
			Title:           title,
			Subtitle:        subtitle,
			Width:           560,
			Confirm:         L("Save"),
			ConfirmDisabled: st.saving || st.text == st.profile.Text,
			ReturnInContent: true,
		}, func() {
			textArea(c, &st.text, 0, fieldOptions{Mono: true, AutoFocus: true, Label: title, Placeholder: L("- Prefers short replies\n- Works in Shanghai (UTC+8)")}).Height(300).LineHeight(1.45)
			gauge, tone := profileGauge(st.text, st.profile)
			ui.Text(c, gauge).FontSize(textCaption).LineHeight(1.4).TextColor(p.tone(tone))
		})
		switch {
		case result.Confirmed:
			st.save(w, s, st.profile.Hash, onSaved)
		case result.Cancelled:
			s.dismiss()
		}
	}, nil)
}

// save writes the draft; with an expected hash it is refused when a bot wrote in between.
func (st *profileState) save(w *appWindow, s *sheet, expectedHash string, onSaved func()) {
	st.saving = true
	store.WriteProfile(st.runnerID, st.text, expectedHash, func(_ string, err error) {
		st.saving = false
		if err == nil {
			if onSaved != nil {
				onSaved()
			}
			s.dismiss()
			return
		}
		if !strings.Contains(model.ErrorText(err), "changed since") {
			w.showAlert(alertOptions{Message: L("Couldn't save your profile"), Informative: model.ErrorText(err)}, nil)
			return
		}
		w.showAlert(alertOptions{
			Message:     L("A bot changed your profile while you were editing"),
			Informative: L("Reload shows the latest version and discards your edits. Overwrite saves yours over it."),
			Buttons:     []alertButton{{Title: L("Reload")}, {Title: L("Overwrite with mine")}, {Title: L("Cancel")}},
		}, func(index int) {
			switch index {
			case 0:
				store.ReadProfile(st.runnerID, func(profile model.Profile, err error) {
					if err != nil {
						w.showAlert(alertOptions{Message: L("Couldn't reload your profile"), Informative: model.ErrorText(err)}, nil)
						return
					}
					st.profile, st.text = profile, profile.Text
				})
			case 1:
				st.save(w, s, "", onSaved)
			}
		})
	})
}

// profileGauge is lines and bytes against the budget, orange from 80% and red once anything
// stops loading. Without a budget from the Runner it shows the size alone.
func profileGauge(text string, profile model.Profile) (string, model.Tone) {
	lines := 0
	if text != "" {
		lines = strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	}
	bytes := len(text)
	if profile.MaxLines <= 0 || profile.MaxBytes <= 0 {
		return L("%d lines · %@", lines, model.Kilobytes(bytes)), model.ToneSecondary
	}
	status := L("%d of %d lines · %@ of %@", lines, profile.MaxLines, model.Kilobytes(bytes), model.Kilobytes(profile.MaxBytes))
	if lines > profile.MaxLines || bytes > profile.MaxBytes {
		return status + " · " + L("the end will not load"), model.ToneRed
	}
	if float64(lines) >= float64(profile.MaxLines)*0.8 || float64(bytes) >= float64(profile.MaxBytes)*0.8 {
		return status, model.ToneOrange
	}
	return status, model.ToneSecondary
}
