package main

import (
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// A chat, after the macOS app's ChatViewController and ChatCells: the transcript, which follows new
// messages while it rests at its end and otherwise holds what the user is reading in place, older
// pages asked for near the top, the composer floating over it, and the button back to the latest.
// Bubbles put the user's on the right in the accent color and a bot's on the left; in a group the
// bot's name sits above its first bubble and its avatar beside the bubble's bottom edge.

const (
	horizontalInset  = 22
	chatAvatarSize   = 26
	avatarGutter     = 10
	groupTopPadding  = 14
	tightTopPadding  = 4
	maxBubbleWidth   = 680
	userLeftGutter   = 72
	bubbleIndent     = horizontalInset + chatAvatarSize + avatarGutter
	separatorGap     = 15 * time.Minute
	composerFallback = 66
)

// chatState is the chat view's own: the transcript's place, the composer's draft, and the notes
// it shows. The view keeps one for the chat on screen; another chat starts afresh.
type chatState struct {
	chatID string
	list   ui.ListState
	rows   []chatRow
	// stoppedNotice is "Chef stopped without replying", after a turn that ended without a word.
	stoppedNotice string
	// stopping is Stop pressed: the composer shows Send until the turn's next word.
	stopping       bool
	scrollToLatest bool
	// flashID is the message a reply's quote brought into view, which pulses from flashAt.
	flashID  string
	flashAt  time.Time
	composer composerState
	// composerHeight is the composer's as the last frame laid it out, which the transcript keeps
	// clear at its end.
	composerHeight float32
	// viewport is the transcript's box as the last frame laid it out; a message's toolbar keeps
	// out from under the header at its top and the composer at its end.
	viewport ui.Rect
	// actionsFor is the message whose toolbar the pointer is over, which keeps it up while the
	// pointer crosses from the bubble to the toolbar, and is on it outside the row.
	actionsFor string
}

func (m *mainWindow) chatStateFor(chatID string) *chatState {
	if m.chat == nil || m.chat.chatID != chatID {
		m.chat = &chatState{chatID: chatID, composerHeight: composerFallback}
		m.chat.list.FollowEnd = true
	}
	return m.chat
}

// chatStoreChanged follows the store for the chat on screen: a new message clears the stopped
// note, and a turn that ended after the user's message says the bots stopped without replying.
func (s *chatState) storeChanged(event model.Event) {
	if s == nil || event.ChatID != s.chatID {
		return
	}
	switch event.Kind {
	case model.EventMessageAdded:
		s.stoppedNotice = ""
	case model.EventRespondingChanged:
		s.stopping = false
		chat := store.Chat(s.chatID)
		if chat != nil && !store.IsResponding(s.chatID) && len(chat.Messages) > 0 && chat.Messages[len(chat.Messages)-1].Author.Kind == model.AuthorYou {
			s.stoppedNotice = L("%@ stopped without replying", store.Title(chat))
		}
	}
}

// MARK: - Rows

type rowKind int

const (
	rowDay rowKind = iota
	rowMessage
	// rowWorking is the bots with a turn running, after the last message.
	rowWorking
	// rowStatus is a one-line note after the last message, such as "Chef stopped without replying".
	rowStatus
)

type chatRow struct {
	kind       rowKind
	key        string
	at         time.Time
	message    *model.Message
	groupStart bool
	// stack is where an output's card sits in a run of one bot's outputs, which join into one list.
	stack  outputStack
	botIDs []string
	text   string
}

// outputStack says whether the cards above and below an output's are in its run.
type outputStack struct{ above, below bool }

func (s outputStack) stacked() bool { return s.above || s.below }

// isOutputCard is a bot's published output, which shows as a card of its own.
func isOutputCard(message *model.Message) bool {
	return message.Output != nil && message.Author.Kind != model.AuthorYou
}

// isImageOutput is a bot's published picture, which shows as the picture, not as a card of a file,
// and stands apart from a run of files.
func isImageOutput(message *model.Message) bool {
	return isOutputCard(message) && message.Output.URL == "" && len(message.Attachments) > 0 && message.Attachments[0].PreviewKind() == model.PreviewImage
}

// buildRows lays the transcript out as rows, after the macOS app's rebuildRows: a day separator
// after fifteen minutes of silence or a new day, the messages (tool calls only as a sent
// message's marker or a command's card while it needs the user), and at the end the working row
// or a status line.
func buildRows(chat *model.Chat, working []string, stoppedNotice string) []chatRow {
	var rows []chatRow
	var previousAuthor *model.Author
	var previousDate time.Time
	for _, message := range chat.Messages {
		if message.Body.Kind == model.BodyTool && !message.Body.Tool.IsShown() {
			continue
		}
		if previousDate.IsZero() || message.CreatedAt.Sub(previousDate) > separatorGap || !model.IsSameDay(previousDate, message.CreatedAt) {
			rows = append(rows, chatRow{kind: rowDay, key: "day:" + message.ID, at: message.CreatedAt})
			previousAuthor = nil
		}
		chrome := message.Body.Kind != model.BodyText
		groupStart := chrome || previousAuthor == nil || !previousAuthor.Same(message.Author)
		rows = append(rows, chatRow{kind: rowMessage, key: message.ID, message: message, groupStart: groupStart})
		if chrome {
			previousAuthor = nil
		} else {
			author := message.Author
			previousAuthor = &author
		}
		previousDate = message.CreatedAt
	}
	// Each run of one bot's file outputs, with nothing between them, joins into one list; a
	// picture is not in one.
	for i := 1; i < len(rows); i++ {
		above, below := &rows[i-1], &rows[i]
		if above.kind == rowMessage && below.kind == rowMessage && isOutputCard(above.message) && isOutputCard(below.message) &&
			!isImageOutput(above.message) && !isImageOutput(below.message) && above.message.Author.Same(below.message.Author) {
			above.stack.below, below.stack.above = true, true
		}
	}
	if len(working) > 0 {
		rows = append(rows, chatRow{kind: rowWorking, key: "working", botIDs: working})
	} else if stoppedNotice != "" {
		rows = append(rows, chatRow{kind: rowStatus, key: "status", text: stoppedNotice})
	}
	return rows
}

// MARK: - The view

func (m *mainWindow) chatView(c *ui.Context, chatID string) {
	p := colors(c)
	chat := store.Chat(chatID)
	if chat == nil {
		return
	}
	s := m.chatStateFor(chatID)
	s.rows = buildRows(chat, store.WorkingBots(chatID), s.stoppedNotice)
	rows := s.rows
	members := store.BotsIn(chat)
	if s.scrollToLatest {
		s.list.ScrollToEnd()
		s.scrollToLatest = false
	}

	view := ui.Box(c).Grow(1).MinHeight(0)
	s.viewport = view.Bounds()
	view.Children(func() {
		s.list.Key = func(i int) any { return rows[i].key }
		bottom := s.composerHeight
		ui.List(c, &s.list, len(rows), func(i int) {
			m.chatRowView(c, chat, rows[i], s)
		}).Fill().Padding(headerHeight+8, 0, bottom+14, 0).ScrollbarInsets(headerHeight, 0, bottom, 0).Label(L("Transcript"))
		// Nearing the first message: ask for the page before it.
		if first, _ := s.list.Visible(); first < 8 && chat.HasMore {
			store.LoadOlderMessages(chatID)
		}
		if len(chat.Messages) == 0 {
			chatEmptyState(c, m, chat, members, s, bottom)
		}
		if !s.list.AtEnd() && len(rows) > 0 {
			// A glass disc, as AppKit's glass bezel draws the Mac's.
			jump := ui.ButtonBase(c.Key("jump")).Absolute().Right(horizontalInset).Bottom(bottom+14).Size(28, 28).Radius(14).Center().
				Material(glassButton{p.GlassButton}).Border(1, p.ComposerBorder).Shadow(0, 1, 1.5, -0.5, ui.RGBA(0, 0, 0, 0.08)).TextColor(p.Label).
				Label(L("Scroll to latest")).Tooltip(L("Scroll to latest (%@)", shortcutText("CmdOrCtrl+J")))
			jump.Children(func() { symbol(c, "arrow.down", 15, 2.1) })
			if jump.Clicked() {
				s.list.ScrollToEnd()
			}
		}
		composer := m.composerView(c, chat, members, s)
		if h := composer.Bounds().H; h > 0 {
			if h != s.composerHeight {
				s.composerHeight = h
				// Bounds is the previous frame's. Build again to apply the new transcript inset.
				c.AnimationFrame()
			}
		} else {
			c.AnimationFrame()
		}
	})
}

// riseIn brings a row that just arrived (made less than messageArrival ago) into the transcript:
// up from 10 DIPs lower as it fades in. A row older than that, or built again as the transcript
// scrolls, shows in place.
func riseIn(c *ui.Context, row ui.Element, made time.Time) {
	if c.Now().Sub(made) >= messageArrival {
		return
	}
	shown := row.Keyframes("arrive", []ui.Keyframe{{Value: 0}, {At: 260 * time.Millisecond, Value: 1, Ease: ui.EaseOut}})
	row.Translate(0, 10*(1-shown)).Opacity(shown)
}

// glassButton is AppKit's glass bezel on a round button: what is behind it frosted under a
// light fill, with no rim of its own; the button draws its even hairline ring and faint shadow.
type glassButton struct{ fill ui.Color }

func (g glassButton) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	glass.Blur{Radius: 4}.PaintMaterial(p, box, radii)
	p.Fill(box, g.fill, radii[0])
}

// chatPlaceholder is what the empty composer says: whom the message goes to.
func chatPlaceholder(chat *model.Chat, members []*model.Bot) string {
	if chat.IsDM() && len(members) > 0 {
		return L("Message %@", members[0].Name)
	}
	title := store.Title(chat)
	if len(members) > 1 {
		return L("Message %@ — @ to address one bot", title)
	}
	return L("Message %@", title)
}

func (m *mainWindow) chatRowView(c *ui.Context, chat *model.Chat, row chatRow, s *chatState) {
	switch row.kind {
	case rowDay:
		dayCell(c, row.at)
	case rowStatus:
		statusCell(c, row.text)
	case rowWorking:
		var bots []*model.Bot
		for _, id := range row.botIDs {
			if bot := store.Bot(id); bot != nil {
				bots = append(bots, bot)
			}
		}
		workingCell(c, bots, workingActivity(row.botIDs, chat), chat.IsGroup())
	case rowMessage:
		message := row.message
		showsAvatar := chat.ShowsSpeakers() && (message.Author.Kind == model.AuthorBot || message.Author.Kind == model.AuthorContact)
		var cardAvatar *avatarContent
		if showsAvatar {
			content := authorAvatar(message.Author)
			cardAvatar = &content
		}
		switch body := message.Body; body.Kind {
		case model.BodyText:
			m.messageCell(c, chat, message, row.groupStart, row.stack, showsAvatar, s)
		case model.BodyTool:
			if body.Tool.Agent != nil {
				m.agentCard(c, chat, message, cardAvatar, row.groupStart)
			} else if body.Tool.Run != nil {
				m.commandCard(c, chat, message, cardAvatar, row.groupStart)
			} else {
				var to *model.Bot
				if body.Tool.TargetBotID != "" {
					to = store.Bot(body.Tool.TargetBotID)
				}
				handoffCell(c, handoffMode{kind: handoffOutgoing, to: to}, body.Tool.Detail, row.groupStart)
			}
		case model.BodyHandoff:
			h := body.Handoff
			mode := handoffMode{kind: handoffBetween, from: store.Bot(h.From), to: store.Bot(h.To)}
			// From a bot outside the chat, as a DM's request or a handoff's report: a message.
			if slices.Contains(chat.BotIDs, h.To) && !slices.Contains(chat.BotIDs, h.From) {
				mode = handoffMode{kind: handoffIncoming, from: store.Bot(h.From)}
			}
			// To a bot outside the chat, as the request its bot sent: a message it sent.
			if slices.Contains(chat.BotIDs, h.From) && !slices.Contains(chat.BotIDs, h.To) {
				mode = handoffMode{kind: handoffOutgoing, to: store.Bot(h.To)}
			}
			handoffCell(c, mode, h.Reason, row.groupStart)
		case model.BodyNotice:
			noticeCell(c, body.Text, row.groupStart)
		case model.BodyPermission:
			if body.Request.IsSecret() {
				m.secretCard(c, chat, message, cardAvatar, row.groupStart)
			} else if body.Request.IsAsk() {
				m.askCard(c, chat, message, cardAvatar, row.groupStart)
			} else {
				m.permissionCard(c, chat, message, cardAvatar, row.groupStart)
			}
		case model.BodyDraft:
			m.draftCard(c, chat, message, cardAvatar, row.groupStart)
		}
	}
}

// botName is who wrote a message, for the cards.
func botName(message *model.Message) string {
	if message.Author.Kind == model.AuthorBot {
		if bot := store.Bot(message.Author.BotID); bot != nil {
			return bot.Name
		}
	}
	return L("The bot")
}

// MARK: - Messages

// quoteAuthorName is who wrote a quoted message, as a reply's quote names them.
func quoteAuthorName(author model.Author) string {
	switch author.Kind {
	case model.AuthorYou:
		return L("You")
	case model.AuthorBot:
		if bot := store.Bot(author.BotID); bot != nil {
			return bot.Name
		}
		return L("Bot")
	case model.AuthorContact:
		return author.Name
	}
	return "Lorca"
}

func (m *mainWindow) messageCell(c *ui.Context, chat *model.Chat, message *model.Message, groupStart bool, stack outputStack, showsAvatar bool, s *chatState) {
	p := colors(c)
	isUser := message.Author.Kind == model.AuthorYou
	text := message.Body.Text
	showsName := showsAvatar && groupStart
	top := float32(groupTopPadding)
	switch {
	case stack.above:
		// The cards of a run stand a few points apart.
		top = tightTopPadding
	case !groupStart:
		top = tightTopPadding
	}
	left := float32(horizontalInset)
	switch {
	case isUser:
		left = horizontalInset + userLeftGutter
	case showsAvatar:
		left = bubbleIndent
	}
	// The bubble as the last frame laid it out, which the floating toolbar is placed beside.
	var bubbleBox ui.Rect
	row := ui.Row(c).Padding(top, horizontalInset, 0, left)
	if isUser {
		row.Justify(ui.End)
	}
	riseIn(c, row, message.CreatedAt)
	row.Children(func() {
		column := ui.Column(c).MinWidth(0).MaxWidth(maxBubbleWidth).AlignItems(ui.Start)
		if isUser {
			column.AlignItems(ui.End)
		}
		column.Children(func() {
			if showsName {
				bot := store.Bot(message.Author.BotID)
				name, tint := L("Bot"), p.Label2
				if bot != nil {
					name, tint = bot.Name, p.accentColor(bot.Accent)
				}
				if message.Author.Kind == model.AuthorContact {
					name = message.Author.Name
				}
				ui.Text(c, name).Padding(0, 0, 0, 4).Margin(0, 0, 2, 0).FontSize(11).FontWeight(600).FixedLineHeight(16).TextColor(tint).SingleLine()
			}
			if quote := message.ReplyTo; quote != nil {
				words := L("In reply to %@: %@", quoteAuthorName(quote.Author), quote.Text)
				b := ui.ButtonBase(c).Height(18).Gap(4).MaxWidthPercent(100).MinWidth(0).Margin(0, 0, 2, 0).FontSize(11.5).TextColor(p.Label2).Cursor(ui.CursorPointer).Label(words).Tooltip(words).FocusRing(false)
				b.Children(func() {
					symbol(c, "arrowshape.turn.up.left.fill", 11, 2.4).TextColor(p.Label3)
					ui.RichText(c, ui.Span{Text: quoteAuthorName(quote.Author) + ":", Weight: 600}, ui.Span{Text: " " + quote.Text}).Shrink(1).MinWidth(0).SingleLine()
				})
				if b.Clicked() {
					m.revealMessage(s, quote.MessageID)
				}
			}
			if message.Output != nil && !isUser {
				m.outputCard(c, chat, message, stack, s)
				return
			}
			bubble := ui.Column(c).MaxWidthPercent(100).MinWidth(0).Padding(12, 18).Radius(radiusSheet).FontSize(textMessage)
			bubbleBox = bubble.Bounds()
			if isUser {
				// A selection is the ink's text color, faint, where the accent's own highlight would vanish.
				bubble.Background(p.Ink).TextColor(p.InkText).SelectionColor(p.InkText.Alpha(0.28))
			} else {
				bubble.Background(p.BotBubble)
			}
			if message.Queued {
				bubble.Opacity(0.55)
			}
			if s.flashID == message.ID {
				// Pulses twice over 1.1 seconds: down to 35% and back, twice.
				if elapsed := c.Now().Sub(s.flashAt); elapsed < 1100*time.Millisecond {
					phase := float64(elapsed) / float64(1100*time.Millisecond)
					bubble.Opacity(float32(1 - 0.65*math.Abs(math.Sin(phase*2*math.Pi))))
					c.AnimationFrame()
				} else {
					s.flashID = ""
				}
			}
			// Reply, from the bubble, and above Copy from its text, as on the Mac.
			var replyMenu func(menu *ui.Menu)
			if message.CanBeQuoted() {
				replyMenu = func(menu *ui.Menu) {
					if menu.Item(L("Reply")).Chosen() {
						m.startReply(s, message)
					}
					if message.Author.BotID != "" && menu.Item(L("Give Feedback…")).Chosen() {
						m.presentFeedback(chat.ID, message)
					}
					if strings.TrimSpace(message.Body.Text) != "" {
						title := L("Save as Skill…")
						if message.Author.Kind == model.AuthorYou {
							title = L("Save as Standing Instruction…")
						}
						if menu.Item(title).Chosen() {
							m.presentPlaybookCapture(chat.ID, message)
						}
					}
				}
				bubble.ContextMenu(replyMenu)
			}
			bubble.Children(func() {
				if len(message.Attachments) > 0 {
					below := float32(0)
					if text != "" {
						below = 8
					}
					attachmentTiles(c, chat.ID, message, isUser).Margin(0, 0, below, 0)
				}
				if text != "" {
					markdownView(c, text, markdownOptions{OnUserBubble: isUser, Menu: replyMenu, Mentionable: mentionable(chat, message)}).Shrink(1)
				}
			})
			if !message.EditedAt.IsZero() {
				ui.Text(c, L("Edited")).Margin(2, 4, 0, 4).FontSize(textCaption).TextColor(p.Label3).Tooltip(model.Clock(message.EditedAt))
			}
			if message.Queued {
				b := ui.ButtonBase(c).Margin(3, 0, 0, 0).Padding(0, 2).FontSize(11.5).FontWeight(500).FixedLineHeight(18).TextColor(p.Accent).Cursor(ui.CursorPointer).
					Tooltip(L("Have the bot read this now. A command it is running moves to the background.")).Label(L("Send now"))
				b.Children(func() { ui.Text(c, L("Send now")) })
				if b.Clicked() {
					store.SendNow(message.ID, chat.ID)
				}
			}
		})
		if showsName {
			ui.Box(c).Absolute().Left(horizontalInset).Bottom(0).Children(func() {
				avatar(c, authorAvatar(message.Author), chatAvatarSize, false)
			})
		}
		// The toolbar floats beside the bubble while the pointer is over its row or the toolbar,
		// laid out of the flow so it moves nothing; placeActions keeps it in view.
		// The row claims the toolbar while the pointer is over it and keeps it while the pointer is
		// on the toolbar, which may stand outside the row; it lets go when the pointer is on neither.
		if row.Hovered() {
			s.actionsFor = message.ID
		}
		if s.actionsFor != message.ID || message.Queued || message.Output != nil || message.State.Kind != model.StateComplete || bubbleBox.W == 0 {
			return
		}
		actions := m.messageActions(c, chat.ID, message, s, text)
		place := placeActions(row.Bounds(), bubbleBox, actionsSize(actions), isUser, s.safeArea())
		actions.Absolute().Left(place.X).Top(place.Y)
		if !row.Hovered() && !actions.Hovered() {
			s.actionsFor = ""
		}
	})
}

// mentionable are the bots whose @Name reads as a chip in a message: the other members of a group,
// in a bot's message there, which is how its owner hands each member a part.
func mentionable(chat *model.Chat, message *model.Message) []*model.Bot {
	if !chat.IsGroup() || message.Author.Kind != model.AuthorBot {
		return nil
	}
	var bots []*model.Bot
	for _, id := range chat.BotIDs {
		if bot := store.Bot(id); bot != nil && id != message.Author.BotID {
			bots = append(bots, bot)
		}
	}
	return bots
}

// startReply makes the draft a reply to a message, from the bubble's Reply.
func (m *mainWindow) startReply(s *chatState, message *model.Message) {
	quote := model.QuoteOf(message)
	if quote == nil {
		return
	}
	s.composer.reply = &composerReply{messageID: quote.MessageID, name: quoteAuthorName(quote.Author), text: quote.Text}
	s.composer.focus = true
	m.invalidate()
}

// revealMessage brings a quoted message into view and pulses its bubble. One on a page not loaded
// yet stays where it is.
func (m *mainWindow) revealMessage(s *chatState, messageID string) {
	for i, row := range s.rows {
		if row.kind == rowMessage && row.message.ID == messageID {
			s.list.ScrollTo(i, ui.Center)
			s.flashID, s.flashAt = messageID, time.Now()
			m.invalidate()
			return
		}
	}
}

// MARK: - Outputs

const (
	outputCardWidth = 320
	// outputIconTile is the tinted tile an output's icon sits on.
	outputIconTile = 40
	// outputImageMax and outputImageMin bound a published picture's longest and shortest side.
	outputImageMax = 360
	outputImageMin = 96
)

// outputTint is the color of an output's icon, by what it is.
func outputTint(p *palette, kind model.OutputKind) ui.Color {
	switch kind {
	case model.OutputLink, model.OutputImage:
		return p.Blue
	case model.OutputVideo:
		return p.Purple
	case model.OutputAudio:
		return p.Pink
	case model.OutputPDF:
		return p.Red
	case model.OutputCode:
		return p.Orange
	case model.OutputData:
		return p.Teal
	case model.OutputTable:
		return p.Green
	case model.OutputText:
		return p.Indigo
	}
	return p.Label2
}

// outputCard is a published file or link in the transcript, standing without a bubble: a card with
// the file's icon on a tinted tile, its name, and its type and size, and under them what the bot
// checked. It shows no time: the stamps belong to the bubbles. A click opens the output's viewer;
// the menu opens or saves the file, and replies to it. A run of one bot's outputs is a column of
// such cards a few points apart, each with the check's symbol at its end in place of its words,
// which are in the output's viewer. A picture is not a card: see imageOutput.
func (m *mainWindow) outputCard(c *ui.Context, chat *model.Chat, message *model.Message, stack outputStack, s *chatState) {
	if isImageOutput(message) {
		m.imageOutput(c, chat, message, s)
		return
	}
	p := colors(c)
	output := message.Output
	kind := output.Kind()
	tint := outputTint(p, kind)
	if output.URL == "" {
		tint = fileIconFor(output.Name, output.Mime).tint
	}
	var attachment *model.Attachment
	fileError := ""
	if len(message.Attachments) > 0 {
		attachment = &message.Attachments[0]
		// Asking for the file starts its fetch; the card shows only whether that failed.
		store.LocalFile(*attachment, chat.ID, message.ID)
		fileError = store.AttachmentError(attachment.ID)
	}
	detail := output.TypeName()
	if attachment != nil {
		size := model.SizeText(attachment.Size)
		if detail != "" {
			detail += " · " + size
		} else {
			detail = size
		}
	}
	ui.Row(c).AlignItems(ui.End).Gap(8).MaxWidthPercent(100).MinWidth(0).Children(func() {
		card := ui.ButtonBase(c.Key("output:"+message.ID)).Width(outputCardWidth).MaxWidthPercent(100).Shrink(1).Radius(radiusXL).Clip().
			Background(p.OutputCard).Border(1, p.Separator).Cursor(ui.CursorPointer).Label(output.Name).Tooltip(output.Name).Transition(controlFade)
		// A card lifts a little under the pointer: a firmer edge and a soft shadow.
		if card.Hovered() {
			card.Border(1, p.ButtonBorder).Shadow(0, 2, 8, 0, p.Shadow.Alpha(0.08))
		}
		sinkWhenPressed(card, card.Pressed(), 0.99)
		if s.flashID == message.ID {
			if elapsed := c.Now().Sub(s.flashAt); elapsed < 1100*time.Millisecond {
				phase := float64(elapsed) / float64(1100*time.Millisecond)
				card.Opacity(float32(1 - 0.65*math.Abs(math.Sin(phase*2*math.Pi))))
				c.AnimationFrame()
			} else {
				s.flashID = ""
			}
		}
		// A lone card shows the start of its file; a run's cards stay one row each.
		var peek *filePeek
		if attachment != nil && fileError == "" && !stack.stacked() {
			peek = loadPeek(store.LocalFile(*attachment, chat.ID, message.ID), attachment.Name, attachment.PreviewKind())
		}
		card.Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				if peek != nil && !peek.empty() {
					outputPeek(c, p, tint, attachment.PreviewKind(), output.Name, peek)
				}
				ui.Row(c).FillWidth().Padding(10, 12, 10, 10).Gap(11).AlignItems(ui.Center).Children(func() {
					icon := ui.Box(c).Size(outputIconTile, outputIconTile).Shrink(0).Center()
					if output.URL != "" {
						// A link has no file: its symbol sits on a tinted tile.
						icon.Radius(radiusLG).Background(tint.Alpha(0.12)).TextColor(tint).Children(func() { symbol(c, output.Symbol(), 18, 1.7) })
					} else {
						icon.Children(func() { fileIconView(c, output.Name, output.Mime, outputIconTile) })
					}
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, output.Name).FontSize(13).FontWeight(600).TextColor(p.Label).SingleLine()
						switch {
						case fileError != "":
							ui.Text(c, L("Couldn't download · Retry")).FontSize(11.5).TextColor(p.Red).SingleLine()
						case detail != "":
							ui.Text(c, detail).FontSize(11.5).TextColor(p.Label2).FontFeatures("tnum").SingleLine()
						}
					})
				})
			})
		})
		card.ContextMenu(func(menu *ui.Menu) {
			if fileError != "" && attachment != nil {
				if menu.Item(L("Try Again")).Chosen() {
					store.RetryAttachment(*attachment, chat.ID, message.ID)
				}
			} else {
				m.outputMenu(menu, message)
			}
			if message.CanBeQuoted() {
				menu.Separator()
				if menu.Item(L("Reply")).Chosen() {
					m.startReply(s, message)
				}
				if menu.Item(L("Give Feedback…")).Chosen() {
					m.presentFeedback(chat.ID, message)
				}
			}
		})
		if card.Clicked() {
			if fileError != "" && attachment != nil {
				store.RetryAttachment(*attachment, chat.ID, message.ID)
			} else {
				m.presentOutput(chat.ID, output.ID)
			}
		}
	})
}

// imageOutput is a published picture in the transcript: the picture itself, whole, with the bot's
// check under it when there is one, and no name or size. A click opens it in the viewer, or
// fetches it again when it could not be fetched; the menu opens or saves the file, and replies to
// it. The output's check and versions are in the viewer its row in the inspector opens.
func (m *mainWindow) imageOutput(c *ui.Context, chat *model.Chat, message *model.Message, s *chatState) {
	p := colors(c)
	output := message.Output
	attachment := message.Attachments[0]
	path := store.LocalFile(attachment, chat.ID, message.ID)
	fileError := store.AttachmentError(attachment.ID)
	width, height := fitImage(attachment, outputImageMax, outputImageMin)
	ui.Row(c).MaxWidthPercent(100).MinWidth(0).Children(func() {
		card := ui.ButtonBase(c.Key("output:"+message.ID)).Width(width).MaxWidthPercent(100).Shrink(1).Radius(radiusXL).Clip().Background(p.Code).
			Border(1, p.Separator).Cursor(ui.CursorPointer).Label(output.Name).Tooltip(output.Name).Transition(controlFade)
		if card.Hovered() {
			card.Shadow(0, 2, 10, 0, p.Shadow.Alpha(0.1))
		}
		sinkWhenPressed(card, card.Pressed(), 0.99)
		if s.flashID == message.ID {
			if elapsed := c.Now().Sub(s.flashAt); elapsed < 1100*time.Millisecond {
				phase := float64(elapsed) / float64(1100*time.Millisecond)
				card.Opacity(float32(1 - 0.65*math.Abs(math.Sin(phase*2*math.Pi))))
				c.AnimationFrame()
			} else {
				s.flashID = ""
			}
		}
		card.Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				switch bitmap := loadBitmap(path); {
				case fileError != "":
					ui.Row(c).FillWidth().Height(height).Padding(0, 14).AlignItems(ui.Center).Justify(ui.Center).TextColor(p.Red).Children(func() {
						ui.Text(c, L("Couldn't download · Retry")).FontSize(12).TextAlign(ui.Center)
					})
				case bitmap != nil:
					ui.Image(c, bitmap).FillWidth().Height(height).Fit(ui.Cover)
				default:
					ui.Row(c).FillWidth().Height(height).AlignItems(ui.Center).Justify(ui.Center).Children(func() { spinner(c, 18) })
				}
			})
		})
		card.ContextMenu(func(menu *ui.Menu) {
			if fileError != "" {
				if menu.Item(L("Try Again")).Chosen() {
					store.RetryAttachment(attachment, chat.ID, message.ID)
				}
			} else {
				m.outputMenu(menu, message)
			}
			if message.CanBeQuoted() {
				menu.Separator()
				if menu.Item(L("Reply")).Chosen() {
					m.startReply(s, message)
				}
				if menu.Item(L("Give Feedback…")).Chosen() {
					m.presentFeedback(chat.ID, message)
				}
			}
		})
		if card.Clicked() {
			if fileError != "" {
				store.RetryAttachment(attachment, chat.ID, message.ID)
			} else if path != "" {
				m.presentFile(attachment, chat.ID, message.ID)
			}
		}
	})
}

const outputPeekHeight = 96

// outputPeek is the start of a file on its card: a page on a ground tinted for the kind of file,
// rising from below the card's row and fading out before it. A Markdown file shows its first
// heading and paragraph, an HTML page its title in a small browser window, other text its first
// lines.
func outputPeek(c *ui.Context, p *palette, tint ui.Color, kind model.PreviewKind, name string, peek *filePeek) {
	ground := tint.Alpha(0.07).Over(p.OutputCard)
	ui.Box(c).FillWidth().Height(outputPeekHeight).Clip().Background(ground).Children(func() {
		paper := ui.Column(c).Absolute().Left(18).Right(18).Top(14).Height(outputPeekHeight).Radius(8).Clip().Background(p.Content).
			Border(1, p.Separator).Shadow(0, 1, 4, 0, p.Shadow.Alpha(0.08))
		paper.Children(func() {
			switch kind {
			case model.PreviewHTML:
				ui.Row(c).FillWidth().Height(18).Padding(0, 8).Gap(4).AlignItems(ui.Center).Background(p.Window).Children(func() {
					for _, dot := range []ui.Color{p.Red, p.Orange, p.Green} {
						ui.Box(c).Size(6, 6).Radius(3).Background(dot.Alpha(0.75))
					}
					ui.Box(c).Grow(1).Margin(0, 0, 0, 8).Height(10).Radius(5).Background(p.Separator.Alpha(0.6))
				})
				ui.Column(c).FillWidth().Padding(9, 12).Gap(4).Children(func() {
					ui.Text(c, firstNonEmpty(peek.title, name)).FontSize(12).FontWeight(650).TextColor(p.Label).SingleLine()
					if peek.body != "" {
						ui.Text(c, peek.body).FontSize(10.5).TextColor(p.Label2).SingleLine()
					} else {
						for _, width := range []float32{100, 82, 64} {
							ui.Box(c).WidthPercent(width).Height(5).Radius(2.5).Background(p.Separator.Alpha(0.7))
						}
					}
				})
			case model.PreviewTable:
				if len(peek.rows) == 0 {
					break
				}
				ui.Column(c).FillWidth().Children(func() {
					for i, row := range peek.rows {
						line := ui.Row(c.Key(i)).FillWidth().Height(15).Padding(0, 8).AlignItems(ui.Center)
						if i == 0 {
							line.Background(p.Window)
						}
						line.Children(func() {
							for _, cell := range row {
								text := ui.Text(c, cell).Grow(1).Basis(0).MinWidth(0).FontSize(9).TextColor(p.Label2).FontFeatures("tnum").SingleLine()
								if i == 0 {
									text.FontWeight(650).TextColor(p.Label)
								}
							}
						})
						ui.Box(c).FillWidth().Height(1).Background(p.Separator.Alpha(0.6))
					}
				})
			case model.PreviewMarkdown:
				ui.Column(c).FillWidth().Padding(10, 14).Gap(4).Children(func() {
					if peek.title != "" {
						ui.Text(c, peek.title).FontSize(12.5).FontWeight(650).TextColor(p.Label).SingleLine()
					}
					if peek.body != "" {
						ui.Text(c, peek.body).FontSize(10.5).LineHeight(1.45).TextColor(p.Label2).MaxLines(3)
					}
				})
			default:
				ui.Text(c, strings.Join(peek.lines, "\n")).Font(monoFont).FontSize(9.5).LineHeight(1.5).TextColor(p.Label2).NoWrap().Padding(9, 12)
			}
		})
		// The page fades into the ground before the card's row.
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(34).PassThrough().Gradient(ground.Alpha(0), ground, 180)
	})
	ui.Box(c).FillWidth().Height(1).Background(p.Separator)
}

// MARK: - Attachments

const (
	imageMax = 220
	imageMin = 72
)

// imageSize is an image's box, from the width and height the message carries, before the bytes
// are here.
func imageSize(attachment model.Attachment) (float32, float32) {
	return fitImage(attachment, imageMax, imageMin)
}

// fitImage is the box of a picture shown no larger than its own size nor than `limit` on a side,
// with the proportions the message carries and no side shorter than `least`.
func fitImage(attachment model.Attachment, limit, least float64) (float32, float32) {
	width, height := float64(max(attachment.Width, 1)), float64(max(attachment.Height, 1))
	if attachment.Width == 0 || attachment.Height == 0 {
		width, height = 4, 3
	}
	scale := math.Min(math.Min(limit/width, limit/height), 1)
	return float32(math.Max(least, math.Ceil(width*scale))), float32(math.Max(least, math.Ceil(height*scale)))
}

// attachmentTiles are thumbnails and file cards inside a bubble, above the text: images side by
// side, wrapping at the bubble's edge, and each file card on a row of its own. A click opens the
// file in the viewer, or retries a fetch that failed; the menu also opens it in its app and saves
// a copy.
func attachmentTiles(c *ui.Context, chatID string, message *model.Message, onUser bool) ui.Element {
	p := colors(c)
	var rows [][]model.Attachment
	for _, attachment := range message.Attachments {
		if n := len(rows); n > 0 && attachment.IsImage() && rows[n-1][0].IsImage() {
			rows[n-1] = append(rows[n-1], attachment)
		} else {
			rows = append(rows, []model.Attachment{attachment})
		}
	}
	tiles := ui.Column(c).Gap(6).MinWidth(0)
	tiles.Children(func() {
		for _, row := range rows {
			ui.Row(c).Wrap().Gap(6).MinWidth(0).Children(func() {
				for _, attachment := range row {
					// The bytes of a file sent from another Device land later; the message's
					// change brings them in.
					path := store.LocalFile(attachment, chatID, message.ID)
					fileError := store.AttachmentError(attachment.ID)
					tooltip := attachment.Name
					if fileError != "" {
						tooltip = attachment.Name + ": " + fileError
					} else if path == "" {
						tooltip = L("%@ · fetching…", attachment.Name)
					}
					// A card with the file's name and its size, or, when the fetch failed, what
					// happened; an image that failed shows the same card in its box.
					card := func() {
						fileIconView(c, attachment.Name, attachment.Mime, 28)
						ui.Column(c).MinWidth(0).Shrink(1).Children(func() {
							ui.Text(c, attachment.Name).FontSize(12.5).FontWeight(500).SingleLine()
							detail := model.SizeText(attachment.Size)
							if fileError != "" {
								detail = L("Couldn't download · Retry")
							}
							ui.Text(c, detail).FontSize(11).Opacity(0.7)
						})
					}
					var tile ui.Element
					if attachment.IsImage() {
						w, h := imageSize(attachment)
						tile = ui.ButtonBase(c.Key(attachment.ID)).Size(w, h).Radius(radiusLG).Clip().Background(p.Code)
						tile.Children(func() {
							if fileError != "" {
								ui.Row(c).FillWidth().FillHeight().Padding(0, 10).Gap(8).Children(func() { card() })
							} else if bitmap := loadBitmap(path); bitmap != nil {
								ui.Image(c, bitmap).Size(w, h).Fit(ui.Cover)
							}
						})
					} else {
						tile = ui.ButtonBase(c.Key(attachment.ID)).Width(230).MaxWidthPercent(100).Height(46).Padding(0, 10).Gap(8).Radius(radiusLG).Background(p.Code)
						if onUser {
							tile.Background(p.InkText.Alpha(0.18))
						}
						tile.Children(func() { card() })
					}
					tile.Label(attachment.Name).Tooltip(tooltip)
					messageID := message.ID
					tile.ContextMenu(func(menu *ui.Menu) {
						switch {
						case fileError != "":
							if menu.Item(L("Try Again")).Chosen() {
								store.RetryAttachment(attachment, chatID, messageID)
							}
						case path != "" && app.main != nil:
							if menu.Item(L("Open")).Chosen() {
								app.main.openAttachment(attachment)
							}
							if menu.Item(L("Save As…")).Chosen() {
								app.main.saveAttachment(attachment)
							}
						}
					})
					if tile.Clicked() {
						if fileError != "" {
							store.RetryAttachment(attachment, chatID, messageID)
						} else if path != "" && app.main != nil {
							app.main.presentFile(attachment, chatID, messageID)
						}
					}
				}
			})
		}
	})
	return tiles
}

// MARK: - The working row

// toolActivity is what a tool row means while it runs, in the words of the status line.
// `pluginName` is the plugin behind a `<plugin>__<tool>` row, so the line reads "Using GitHub"
// between two calls as well as during one.
func toolActivity(tool *model.ToolInvocation, targetName, pluginName string) string {
	switch tool.Name {
	case "read":
		return L("Reading a file")
	case "write", "edit":
		return L("Drafting a file")
	case "bash":
		if tool.Description != "" {
			return L("Running command: %@", tool.Description)
		}
		return L("Running commands")
	case "web_search":
		return L("Searching the web")
	case "web_fetch":
		return L("Reading the web")
	case "grep", "find", "ls":
		return L("Searching files")
	case "message_bot":
		if targetName != "" {
			return L("Messaging %@", targetName)
		}
		return L("Messaging another bot")
	case "list_teammates":
		return L("Checking the team")
	case "create_bot":
		return L("Creating a bot")
	case "edit_bot":
		return L("Updating a bot")
	case "create_group":
		return L("Creating a group")
	case "edit_group":
		return L("Updating a group")
	case "memory_update", "memory_log":
		return L("Taking a note")
	case "search_plugins":
		return L("Searching plugins")
	case "install_plugin":
		return L("Installing a plugin")
	case "bash_input":
		return L("Answering a command")
	case "bash_output":
		return L("Waiting on a command")
	case "codemode":
		// A codemode script: its latest command, else the plugin of its latest plugin call, which
		// the CLI names.
		if tool.ScriptCommand != "" {
			return L("Running command: %@", tool.ScriptCommand)
		}
		if tool.Description != "" {
			return L("Using %@", tool.Description)
		}
		return L("Working")
	}
	if pluginName != "" {
		return L("Using %@", pluginName)
	}
	if strings.Contains(tool.Name, "__") && strings.HasPrefix(tool.Summary, "Using ") {
		return strings.TrimSuffix(tool.Summary, "…")
	}
	return L("Working")
}

// pluginNameOf is the plugin behind a `<plugin>__<tool>` row, by name, from the bot's Runner.
func pluginNameOf(toolName, botID string) string {
	pluginID, _, ok := strings.Cut(toolName, "__")
	bot := store.Bot(botID)
	if !ok || bot == nil {
		return ""
	}
	if device := store.Device(bot.RunnerID); device != nil {
		for _, plugin := range device.Plugins {
			if plugin.ID == pluginID {
				return plugin.Name
			}
		}
	}
	if pluginID == "" {
		return ""
	}
	runes := []rune(pluginID)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// workingActivity is what the one working bot is doing: its latest tool, running or just
// finished (so the line does not flash back to its name between two commands), thinking, or a
// model call waiting to be asked again, which outranks both.
func workingActivity(botIDs []string, chat *model.Chat) string {
	words := ""
	only := ""
	if len(botIDs) == 1 {
		only = botIDs[0]
	}
	if n := len(chat.Messages); only != "" && n > 0 {
		last := chat.Messages[n-1]
		if last.Author.Kind == model.AuthorBot && last.Author.BotID == only && last.Body.Kind == model.BodyTool && !last.Body.Tool.IsSentMessage() {
			tool := last.Body.Tool
			target := ""
			if tool.TargetBotID != "" {
				if bot := store.Bot(tool.TargetBotID); bot != nil {
					target = bot.Name
				}
			}
			words = toolActivity(tool, target, pluginNameOf(tool.Name, only))
		}
	}
	if only != "" && store.IsThinking(only, chat.ID) {
		words = Lc("Thinking", "status")
	}
	if note := store.RetryNote(chat.ID); note != "" {
		words = note
	}
	return words
}

func workingText(names []string, activity string, showsName bool) string {
	switch {
	case len(names) > 1:
		return L("%@ and %@ are working…", strings.Join(names[:len(names)-1], L(", ")), names[len(names)-1])
	case activity != "":
		return activity + "…"
	case showsName && len(names) > 0:
		return L("%@ is working…", names[0])
	}
	return L("Working") + "…"
}

// workingCell is "Chef is working…" after the last message, its words shimmering while a turn
// runs. A DM reads "Working…"; with one bot at work the line says what it is doing.
func workingCell(c *ui.Context, bots []*model.Bot, activity string, showsName bool) {
	p := colors(c)
	names := make([]string, 0, len(bots))
	for _, bot := range bots {
		names = append(names, bot.Name)
	}
	text := workingText(names, activity, showsName)
	label := text
	if len(bots) == 1 {
		label = L("%@ is working", bots[0].Name)
	}
	ui.Row(c).AlignItems(ui.Start).Gap(10).Height(44).Padding(14, horizontalInset, 0, horizontalInset).Label(label).Children(func() {
		ui.Box(c).Size(chatAvatarSize, chatAvatarSize).Children(func() {
			if len(bots) > 0 {
				avatar(c, botAvatar(bots[0]), chatAvatarSize, false)
			}
		})
		shimmerText(c, text, 12.5, p.Label).Margin(5, 0, 0, 0)
	})
}

// shimmerText is words dimmed to 40%, with a bright band 30% of their width sweeping from the
// leading edge to the trailing one in 1.5 s, resting a quarter second between sweeps.
func shimmerText(c *ui.Context, text string, size float32, color ui.Color) ui.Element {
	t := ui.Text(c, text).FontSize(size).FixedLineHeight(16).TextColor(color.Alpha(0.4)).SingleLine().Shrink(1).MinWidth(0)
	t.DrawOver(func(painter *ui.Painter, r ui.Rect) {
		painter.AnimationFrame()
		const period = 1750 * time.Millisecond
		at := float32(painter.Now().UnixNano()%int64(period)) / float32(period)
		sweep := min(at/0.857, 1)
		w, _ := painter.MeasureText(0, ui.Span{Text: text, Size: size})
		band := w * 0.3
		center := r.X - band + (w+2*band)*sweep
		// The band brightens toward its middle, in strips.
		const strips = 8
		for i := range strips {
			edge := float32(i) / strips
			alpha := 1 - float32(math.Abs(float64(edge*2-1+1.0/strips)))
			x := center - band/2 + band*edge
			painter.Clip(ui.Rect{X: x, Y: r.Y, W: band / strips, H: r.H}, 0, func() {
				painter.RichText(r.X, r.Y+(r.H-16)/2, 0, ui.Span{Text: text, Size: size, Color: color.Alpha(alpha)})
			})
		}
	})
	return t
}

// MARK: - Day, status, and notice rows

func dayCell(c *ui.Context, at time.Time) {
	p := colors(c)
	ui.Row(c).Height(42).Justify(ui.Center).Children(func() {
		ui.Text(c, model.DaySeparator(at)).Padding(3, 9).Radius(8).Background(p.Chip).FontSize(textCaption).FontWeight(500).TextColor(p.Label2)
	})
}

// statusCell is a quiet centered line, such as "Chef stopped without replying".
func statusCell(c *ui.Context, text string) {
	p := colors(c)
	ui.Row(c).MinHeight(30).Padding(0, horizontalInset).Justify(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(11.5).TextColor(p.Label3).TextAlign(ui.Center)
	})
}

func noticeCell(c *ui.Context, text string, groupStart bool) {
	p := colors(c)
	top := float32(groupTopPadding)
	if !groupStart {
		top = tightTopPadding
	}
	ui.Row(c).Justify(ui.Center).Padding(top, horizontalInset, 0, horizontalInset).Children(func() {
		ui.Row(c).AlignItems(ui.Start).Gap(8).MaxWidth(460).Padding(8, 10).Radius(9).Background(p.Chip).Children(func() {
			symbol(c, "clock.badge.questionmark", 13, 2).TextColor(p.Label3).Margin(1, 0, 0, 0)
			ui.Text(c, text).FontSize(11.5).LineHeight(1.35).TextColor(p.Label2).Shrink(1).Selectable()
		})
	})
}

// MARK: - Bot-to-bot markers

type handoffKind int

const (
	handoffIncoming handoffKind = iota
	handoffOutgoing
	handoffBetween
)

type handoffMode struct {
	kind     handoffKind
	from, to *model.Bot
}

func nameOf(bot *model.Bot) string {
	if bot == nil {
		return "?"
	}
	return bot.Name
}

func botAvatarOrSystem(bot *model.Bot) avatarContent {
	if bot == nil {
		return avatarContent{Kind: avatarSystem}
	}
	return botAvatar(bot)
}

func handoffSpokenText(mode handoffMode, reason string) string {
	text := strings.TrimSpace(reason)
	switch mode.kind {
	case handoffIncoming:
		return L("Message from") + " " + nameOf(mode.from) + ": " + text
	case handoffOutgoing:
		return L("Messaged") + " " + nameOf(mode.to) + ": " + text
	}
	return L("%@ handed off to %@", nameOf(mode.from), nameOf(mode.to)) + ": " + text
}

// handoffCell shows bot-to-bot messages as markers: "Message from ◉ Name · first line" where one
// arrived, "Messaged ◉ Name · first line" where one was sent, centered; a handoff between two bots
// in the same chat keeps both avatars on one line. A click opens the whole message.
func handoffCell(c *ui.Context, mode handoffMode, reason string, groupStart bool) {
	p := colors(c)
	top := float32(groupTopPadding)
	if !groupStart {
		top = tightTopPadding
	}
	full := strings.TrimSpace(reason)
	first := firstLineOf(full)
	row := ui.Row(c).Padding(top, horizontalInset, 0, horizontalInset).Justify(ui.Center)
	if mode.kind == handoffBetween {
		row.Justify(ui.Start).Padding(top, horizontalInset, 0, 58)
	}
	row.Children(func() {
		marker := ui.Row(c).Gap(5).MaxWidthPercent(100).MinWidth(0).Padding(3, 9).Radius(12).FontSize(11.5).TextColor(p.Label2).
			Label(handoffSpokenText(mode, reason)).Role(ui.RoleButton)
		if full != "" {
			marker.Cursor(ui.CursorPointer)
			textPopover(c, marker, full)
		}
		marker.Children(func() {
			if mode.kind == handoffBetween {
				avatar(c, botAvatarOrSystem(mode.from), 18, false)
				symbol(c, "arrow.right", 11, 2.4).TextColor(p.Label3)
				avatar(c, botAvatarOrSystem(mode.to), 18, false)
				ui.Text(c, L("%@ handed off to %@", nameOf(mode.from), nameOf(mode.to))).SingleLine()
			} else {
				lead, bot := L("Messaged"), mode.to
				if mode.kind == handoffIncoming {
					lead, bot = L("Message from"), mode.from
				}
				ui.Text(c, lead).NoWrap()
				avatar(c, botAvatarOrSystem(bot), 16, false)
				ui.Text(c, nameOf(bot)).FontWeight(500).TextColor(p.Label).NoWrap()
			}
			if first != "" {
				ui.Text(c, "· "+first).MaxWidth(260).Shrink(1).MinWidth(0).SingleLine()
			}
		})
	})
}

// MARK: - The empty chat

// emptyPrompts are two prompts to start a chat with.
func emptyPrompts(bots []*model.Bot) []string {
	if len(bots) == 0 {
		return nil
	}
	first := bots[0]
	if len(bots) > 1 {
		return []string{L("@%@ break this down and hand off what you can", first.Name), L("@everyone what would you check first?")}
	}
	switch first.ID {
	case "bot-patch":
		return []string{L("Write the smallest version that works"), L("What would you delete first?")}
	case "bot-scout":
		return []string{L("Find the prior art and cite it"), L("What do we not know yet?")}
	case "bot-quill":
		return []string{L("Rewrite this without adjectives"), L("One paragraph, no hype")}
	case "bot-ember":
		return []string{L("What is the blast radius?"), L("Status on the last deploy")}
	}
	return []string{L("What should I work on next?"), L("Plan this and delegate the parts")}
}

// chatEmptyState is a chat with no messages yet, after the macOS app's ChatEmptyStateView: its
// bots, its title, where it runs, and two prompts to start with, a little above the middle of the
// room over the composer.
func chatEmptyState(c *ui.Context, m *mainWindow, chat *model.Chat, bots []*model.Bot, s *chatState, bottom float32) {
	p := colors(c)
	subtitle := L("No bots in this group yet.")
	switch {
	case chat.IsDM() && len(bots) > 0:
		host := L("an unassigned Runner")
		if device := store.Device(bots[0].RunnerID); device != nil {
			host = device.Name
		}
		subtitle = L("Runs on %@ with %@", host, model.ProviderName(bots[0].Provider, store.Providers))
	case len(bots) > 1:
		names := make([]string, 0, len(bots))
		for _, bot := range bots {
			names = append(names, bot.Name)
		}
		subtitle = strings.Join(names, L(", ")) + "\n" + L("Address one with @, or say @everyone to hear from all of them.")
	case len(bots) > 0:
		subtitle = L("A group of one for now. Add bots from the inspector.")
	}
	ui.Column(c).Absolute().Left(0).Right(0).Top(0).Bottom(bottom).Center().PassThrough().Children(func() {
		ui.Column(c).Width(440).MaxWidthPercent(100).Padding(0, 16).Gap(12).AlignItems(ui.Center).Margin(-40, 0, 0, 0).Children(func() {
			avatarStack(c, bots, 52, 16, p.Content).Margin(0, 0, 4, 0)
			ui.Text(c, store.Title(chat)).FontSize(19).FontWeight(600).TextAlign(ui.Center)
			ui.Text(c, subtitle).MaxWidth(400).FontSize(12.5).LineHeight(1.4).TextColor(p.Label2).TextAlign(ui.Center).MaxLines(2)
			ui.Column(c).Gap(8).AlignItems(ui.Center).Margin(6, 0, 0, 0).Children(func() {
				for _, prompt := range emptyPrompts(bots) {
					chip := ui.ButtonBase(c.Key(prompt)).Padding(6, space3).Radius(radiusCapsule).Border(1, p.ButtonBorder).Background(p.ButtonBG).TextColor(p.Label2).FontSize(12).Cursor(ui.CursorPointer)
					if chip.Hovered() {
						chip.BorderColor(p.FieldBorder).Background(p.ButtonHover).TextColor(p.Label)
					}
					chip.Children(func() { ui.Text(c, prompt) })
					if chip.Clicked() {
						s.composer.draft = prompt
						s.composer.caret = len([]rune(prompt))
						s.composer.focus = true
					}
				}
			})
		})
	})
}
