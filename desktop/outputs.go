package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Outputs, after the macOS app: what a chat's bots published shows in the inspector, the latest
// three with View all for the rest. A row opens the output: a file in the viewer over the whole
// window (viewer.go), a link in a sheet, each with what the bot checked and its versions. Open hands the file to its app
// or the link to the browser; Save As… copies the file. The files come from the local CLI.

// outputSaveDestination asks where to save a copy; tests put their own in its place.
var outputSaveDestination = func(parent *mygo.Window, name string) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: L("Save As…"), ButtonLabel: L("Save"), DefaultPath: filepath.Base(name)})
}

// outputOpenPath and outputOpenURL hand a file or a link to the system; tests replace them.
var outputOpenPath = openFile
var outputOpenURL = func(link string) { _ = openExternal(link) }

// inspectorOutputs is the inspector's Outputs section, hidden while the chat has none.
func (m *mainWindow) inspectorOutputs(c *ui.Context, chat *model.Chat) {
	all := store.Outputs(chat.ID)
	if len(all) == 0 {
		return
	}
	shown := all[:min(3, len(all))]
	var accessory func()
	if len(all) > len(shown) {
		accessory = func() {
			// On the rows' trailing text edge, as a row's state is.
			b := ui.ButtonBase(c.Key("outputs-all")).Margin(0, 12, 0, 0).Label(L("View all")).Cursor(ui.CursorPointer)
			b.Children(func() { ui.Text(c, L("View all")).FontSize(11).TextColor(colors(c).Label2) })
			if b.Clicked() {
				m.presentAllOutputs(chat.ID)
			}
		}
	}
	section(c, L("Outputs"), sectionCaption, accessory, func(k *card) {
		for _, series := range shown {
			m.outputRow(c, k, chat, series)
		}
	})
}

// outputRow is an output's row: what it is, its name, when it was published (and by whom, in a
// group), and its size. A click opens the output; its menu has the file actions.
func (w *appWindow) outputRow(c *ui.Context, k *card, chat *model.Chat, series model.OutputSeries) {
	output := series.Output()
	subtitle := model.Stamp(series.Latest().CreatedAt)
	if chat.IsGroup() {
		if bot := store.Bot(output.BotID); bot != nil {
			subtitle = bot.Name + " · " + subtitle
		}
	}
	o := statusRowOptions{Symbol: output.Symbol(), Title: output.Name, Subtitle: subtitle, Clickable: true, Tooltip: output.Name, State: output.TypeName()}
	if latest := series.Latest(); len(latest.Attachments) > 0 {
		o.FileIcon = [2]string{output.Name, output.Mime}
		if size := latest.Attachments[0].Size; size > 0 {
			o.State = model.SizeText(size)
		}
	}
	var row statusRowResult
	var element ui.Element
	ui.Box(c.Key("output:" + series.ID())).Children(func() {
		element, row = statusRow(c, k, o)
	})
	latest := series.Latest()
	element.ContextMenu(func(menu *ui.Menu) { w.outputMenu(menu, latest) })
	if row.Clicked {
		w.presentOutput(chat.ID, series.ID())
	}
}

// outputMenu is Open and Save As…, or for a link Open Link and Copy Link.
func (w *appWindow) outputMenu(menu *ui.Menu, message *model.Message) {
	if link := message.Output.DocumentURL(); link != "" {
		if menu.Item(L("Open Link")).Chosen() {
			outputOpenURL(link)
		}
		if menu.Item(L("Copy Link")).Chosen() {
			mygo.Clipboard.WriteText(link)
		}
		return
	}
	if len(message.Attachments) == 0 {
		return
	}
	attachment := message.Attachments[0]
	if menu.Item(L("Open")).Chosen() {
		w.openAttachment(attachment)
	}
	if menu.Item(L("Save As…")).Chosen() {
		w.saveAttachment(attachment)
	}
}

// openOutput opens the version's file in its app, or its link in the browser.
func (w *appWindow) openOutput(message *model.Message) {
	if link := message.Output.DocumentURL(); link != "" {
		outputOpenURL(link)
	} else if len(message.Attachments) > 0 {
		w.openAttachment(message.Attachments[0])
	}
}

// openAttachment opens a file in its app, under its own name so the system knows what it is.
func (w *appWindow) openAttachment(attachment model.Attachment) {
	store.OpenableFile(attachment, func(path string, err error) {
		if err != nil {
			w.fileFailed(err)
			return
		}
		outputOpenPath(path)
	})
}

// saveAttachment asks where to put a copy of the file and writes it there.
func (w *appWindow) saveAttachment(attachment model.Attachment) {
	store.OpenableFile(attachment, func(source string, err error) {
		if err != nil {
			w.fileFailed(err)
			return
		}
		parent := w.win
		model.Async(store, func() (struct{}, error) {
			target, err := outputSaveDestination(parent, attachment.Name)
			if err != nil || target == "" {
				return struct{}{}, err
			}
			return struct{}{}, copyFile(source, target)
		}, func(_ struct{}, err error) {
			if err != nil {
				w.fileFailed(err)
			}
		})
	})
}

func (w *appWindow) fileFailed(err error) {
	w.showAlert(alertOptions{Message: L("Couldn't get this file"), Informative: model.ErrorText(err), Escape: -1}, func(int) {})
}

// copyFile writes a copy of source at target through a temporary file beside it, so a failed copy
// leaves what was there.
func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if info, err := os.Stat(target); err == nil {
		if sourceInfo, err := input.Stat(); err == nil && os.SameFile(sourceInfo, info) {
			return nil
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".lorca-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	_, err = io.Copy(temporary, input)
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary.Name(), target)
}

// outputSheet is one output's sheet, or its viewer for a file; picked is the version on screen,
// empty for the latest. shown is the output as last listed, kept while a resync lists the chat
// again. details is whether the viewer's side pane is open, and detailsSet whether it has been
// opened or closed yet.
type outputSheet struct {
	chatID, outputID    string
	picked              string
	viewer              *fileViewer
	shown               model.OutputSeries
	details, detailsSet bool
}

// presentOutput puts up one output: a file in the viewer over the whole window, a link in a sheet.
func (w *appWindow) presentOutput(chatID, outputID string) {
	state := &outputSheet{chatID: chatID, outputID: outputID, viewer: newFileViewer()}
	s := w.present(func(c *ui.Context, s *sheet) { state.view(c, s) }, state.viewer.close)
	if series, ok := state.series(); ok {
		s.fullWindow = len(series.Latest().Attachments) > 0
	}
}

func (o *outputSheet) series() (model.OutputSeries, bool) {
	for _, series := range store.Outputs(o.chatID) {
		if series.ID() == o.outputID {
			o.shown = series
		}
	}
	return o.shown, len(o.shown.Versions) > 0
}

func (o *outputSheet) view(c *ui.Context, s *sheet) {
	p := colors(c)
	series, ok := o.series()
	if !ok || store.Chat(o.chatID) == nil {
		s.dismiss()
		return
	}
	message := series.Latest()
	for _, version := range series.Versions {
		if version.ID == o.picked {
			message = version
		}
	}
	output := message.Output
	bot := L("A bot")
	if b := store.Bot(output.BotID); b != nil {
		bot = b.Name
	}
	subtitle := bot + " · " + model.DaySeparator(message.CreatedAt)
	if s.fullWindow = len(message.Attachments) > 0; s.fullWindow {
		o.fileView(c, s, series, message, bot, subtitle)
		return
	}
	link := output.DocumentURL()
	result := sheetFrame(c, sheetOptions{Title: output.Name, Subtitle: subtitle, Width: 480, Confirm: L("Open"), Cancel: L("Done")}, func() {
		if link != "" {
			section(c, L("Link"), sectionCaption, nil, func(k *card) {
				host := link
				if parsed := strings.TrimPrefix(link, "https://"); parsed != "" {
					host, _, _ = strings.Cut(parsed, "/")
				}
				if _, row := statusRow(c, k, statusRowOptions{Symbol: "link", Title: host, Subtitle: link, Clickable: true, Tooltip: link}); row.Clicked {
					outputOpenURL(link)
				}
			})
		}
		o.versions(c, p, series, message)
	})
	switch {
	case result.Cancelled:
		s.dismiss()
	case result.Confirmed:
		s.window.openOutput(message)
		s.dismiss()
	}
}

// fileView is the output's file in the viewer, with its versions and the file's facts in the info
// pane at its side.
func (o *outputSheet) fileView(c *ui.Context, s *sheet, series model.OutputSeries, message *model.Message, bot, subtitle string) {
	p := colors(c)
	output := message.Output
	attachment := message.Attachments[0]
	path := store.LocalFile(attachment, o.chatID, message.ID)
	f := o.viewer.file(c, s.window, attachment, message.ID, path, store.AttachmentError(attachment.ID))
	if attachment.Size > 0 {
		subtitle += " · " + model.SizeText(attachment.Size)
	}
	var clicked viewerActions
	options := viewerFrameOptions{
		Title: output.Name, Subtitle: subtitle, Name: output.Name, Mime: output.Mime,
		Center: o.viewer.switcher(c, f), Actions: func() { clicked = o.viewer.actions(c, f) },
	}
	// The info pane opens to start with when there is more than one version to pick from.
	if !o.detailsSet {
		o.details, o.detailsSet = len(series.Versions) > 1, true
	}
	options.Details = func() {
		o.versions(c, p, series, message)
		fileFacts(c, p, attachment, bot, message)
	}
	options.DetailsShown = &o.details
	closed := viewerFrame(c, options, func() {
		o.viewer.body(c, f, func() { store.RetryAttachment(attachment, o.chatID, message.ID) }, func() { clicked.open = true })
	})
	switch {
	case clicked.save:
		s.window.saveAttachment(attachment)
	case clicked.open:
		s.window.openOutput(message)
		s.dismiss()
	case closed || o.viewer.closing:
		s.dismiss()
	}
}

// versions are an output's versions, when it has more than one, as a line of stops, each of which
// puts that version on screen.
func (o *outputSheet) versions(c *ui.Context, p *palette, series model.OutputSeries, message *model.Message) {
	if len(series.Versions) < 2 {
		return
	}
	infoGroup(c, L("Versions"), func() {
		latest := series.Latest().ID
		// Newest first, as the inspector lists outputs.
		for i := len(series.Versions) - 1; i >= 0; i-- {
			version := series.Versions[i]
			if o.versionRow(c, p, version, version.ID == message.ID, i > 0, i < len(series.Versions)-1) {
				o.picked = version.ID
				if version.ID == latest {
					o.picked = ""
				}
			}
		}
	})
}

// infoGroup is a group of the info pane under its caption.
func infoGroup(c *ui.Context, caption string, rows func()) {
	ui.Column(c).FillWidth().Gap(8).Label(caption).Children(func() {
		ui.Text(c, caption).FontSize(11).FontWeight(600).TextColor(colors(c).Label2).Padding(0, 2)
		ui.Column(c).FillWidth().Gap(0).Children(rows)
	})
}

// versionRow is one version on the line of stops: a dot, filled for the version on screen, its
// number, when, and its file's size. It answers a click. above and below draw the line on to
// the stops before and after it.
func (o *outputSheet) versionRow(c *ui.Context, p *palette, version *model.Message, current, below, above bool) bool {
	row := ui.ButtonBase(c.Key("version:"+version.ID)).FillWidth().Height(44).Padding(0, 10, 0, 6).Gap(10).Radius(radiusLG).
		AlignItems(ui.Center).Label(L("Version %d", int(version.Output.Version))).Transition(controlFade)
	switch {
	case current:
		row.Background(p.Selection.Alpha(0.6))
	case row.Pressed():
		row.Background(p.Pressed)
	case row.Hovered():
		row.Background(p.Hover)
	}
	row.Children(func() {
		ui.Column(c).Width(16).FillHeight().AlignItems(ui.Center).Children(func() {
			line := func(show bool) {
				fill := p.Separator
				if !show {
					fill = ui.Transparent
				}
				ui.Box(c).Width(1.5).Grow(1).Background(fill)
			}
			line(above)
			dot := ui.Box(c).Size(10, 10).Radius(5).Shrink(0)
			if current {
				dot.Background(p.Ink)
			} else {
				dot.Background(p.Content).Border(1.5, p.Label3)
			}
			line(below)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			weight := 500
			if current {
				weight = 600
			}
			ui.Text(c, L("Version %d", int(version.Output.Version))).FontSize(12.5).FontWeight(weight).TextColor(p.Label).SingleLine()
			ui.Text(c, model.DaySeparator(version.CreatedAt)).FontSize(11).TextColor(p.Label2).SingleLine()
		})
		if len(version.Attachments) > 0 && version.Attachments[0].Size > 0 {
			ui.Text(c, model.SizeText(version.Attachments[0].Size)).FontSize(11).TextColor(p.Label3).FontFeatures("tnum").SingleLine()
		}
	})
	return row.Clicked()
}

// fileFacts is the file's facts: what it is, its size, a picture's dimensions, and who published
// it when.
func fileFacts(c *ui.Context, p *palette, attachment model.Attachment, bot string, message *model.Message) {
	infoGroup(c, L("File"), func() {
		fact := func(key, value string) {
			if value == "" {
				return
			}
			ui.Row(c).FillWidth().Padding(5, 2).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, key).Width(72).Shrink(0).FontSize(12).TextColor(p.Label2).SingleLine()
				ui.Text(c, value).Grow(1).Shrink(1).MinWidth(0).FontSize(12).TextColor(p.Label).FontFeatures("tnum").Selectable()
			})
		}
		fact(L("Kind"), message.Output.TypeName())
		if attachment.Size > 0 {
			fact(L("Size"), model.SizeText(attachment.Size))
		}
		if attachment.Width > 0 && attachment.Height > 0 {
			fact(L("Dimensions"), fmt.Sprintf("%d × %d", attachment.Width, attachment.Height))
		}
		fact(L("From"), bot)
		fact(L("Published"), model.DaySeparator(message.CreatedAt))
	})
}

// presentAllOutputs lists every output of the chat, for the inspector's View all.
func (m *mainWindow) presentAllOutputs(chatID string) {
	m.present(func(c *ui.Context, s *sheet) {
		chat := store.Chat(chatID)
		if chat == nil {
			s.dismiss()
			return
		}
		result := sheetFrame(c, sheetOptions{Title: L("Outputs"), Width: 440, Confirm: L("Done"), NoCancel: true}, func() {
			ui.Scroll(c).MaxHeight(440).Children(func() {
				section(c, "", sectionCaption, nil, func(k *card) {
					for _, series := range store.Outputs(chatID) {
						s.window.outputRow(c, k, chat, series)
					}
				})
			})
		})
		if result.Confirmed || result.Cancelled {
			s.dismiss()
		}
	}, nil)
}
