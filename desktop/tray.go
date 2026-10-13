package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
)

// The tray icon: the menu bar's on macOS, the notification area's on Windows and Linux. Its menu
// lists the chats with unread messages, and the unread total is the number beside the icon on
// macOS and in the tooltip elsewhere, as the Dock or launcher badge carries it.

// trayChatLimit is how many unread chats the menu lists before "more".
const trayChatLimit = 8

type trayChat struct {
	id     string
	title  string
	unread int
}

// trayState is what the tray shows of the store: it is comparable by its key, so the menu is
// rebuilt only when something it shows changes.
type trayState struct {
	unread    int
	chats     []trayChat
	more      int
	attention int
	// signedIn is whether the account exists: without it the menu offers only the app.
	signedIn bool
}

func (s trayState) key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v|%d|%d|%d", s.signedIn, s.unread, s.more, s.attention)
	for _, chat := range s.chats {
		fmt.Fprintf(&b, "|%s:%s:%d", chat.id, chat.title, chat.unread)
	}
	return b.String()
}

// unreadChats are the chats with unread messages the user has not muted, in the sidebar's order.
func unreadChats() []*model.Chat {
	if store == nil || (store.HasIdentity != nil && !*store.HasIdentity) {
		return nil
	}
	now := time.Now()
	var out []*model.Chat
	for _, chat := range store.Chats {
		if chat.UnreadCount > 0 && !chat.IsMuted(now) {
			out = append(out, chat)
		}
	}
	return out
}

// unreadTotal is the sum of the unread messages of the chats that are not muted: the number on
// the Dock icon and beside the tray icon.
func unreadTotal() int {
	total := 0
	for _, chat := range unreadChats() {
		total += chat.UnreadCount
	}
	return total
}

func currentTrayState() trayState {
	state := trayState{signedIn: store != nil && (store.HasIdentity == nil || *store.HasIdentity)}
	if !state.signedIn {
		return state
	}
	chats := unreadChats()
	for _, chat := range chats {
		state.unread += chat.UnreadCount
	}
	for i, chat := range chats {
		if i == trayChatLimit {
			state.more = len(chats) - trayChatLimit
			break
		}
		state.chats = append(state.chats, trayChat{id: chat.ID, title: store.Title(chat), unread: chat.UnreadCount})
	}
	state.attention = len(store.Attention.Items)
	return state
}

// trayMenu is the tray's menu for a state: the unread chats on top, what waits on the user, then
// the app's own commands.
func (a *appDelegate) trayMenu(s trayState) *mygo.Menu {
	var items []*mygo.MenuItem
	item := func(label string, click func()) {
		items = append(items, &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) { click() }})
	}
	if s.unread > 0 {
		items = append(items, &mygo.MenuItem{Label: L("%d unread", s.unread), Disabled: true})
		for _, chat := range s.chats {
			id := chat.id
			item(fmt.Sprintf("%s · %d", chat.title, chat.unread), func() { a.openChat(id) })
		}
		if s.more > 0 {
			item(L("%d more chats…", s.more), a.reopen)
		}
		items = append(items, mygo.Separator())
	}
	if s.attention > 0 {
		item(L("Attention (%d)", s.attention), func() { runCommand("attention") })
		items = append(items, mygo.Separator())
	}
	item(L("Open %@", appName()), a.reopen)
	if s.signedIn {
		item(L("New Bot…"), func() { runCommand("newBot") })
	}
	item(L("Settings…"), func() { runCommand("settings") })
	items = append(items, mygo.Separator())
	item(L("Quit %@", appName()), mygo.App.Quit)
	return mygo.NewMenu(items)
}

// installTray puts the app in the menu bar or notification area. On Windows and Linux closing its
// windows would otherwise leave nothing to bring it back by, and bots here stop with the app.
func (a *appDelegate) installTray() {
	if a.tray != nil || !prefs.get().ShowsTrayIcon {
		return
	}
	// The app's own icon, in color, as in the Dock; Lorca Dev wears its own, so the two are told apart.
	icon := trayIcon
	if isDevelopment() {
		icon = trayIconDev
	}
	state := currentTrayState()
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    icon,
		ToolTip: appName(),
		Menu:    a.trayMenu(state),
	})
	if err != nil {
		return
	}
	// On macOS a click on the icon shows the menu. Elsewhere it brings the app back, as a click on
	// its Dock icon does; the menu is on the right button on Windows, and all a click shows on
	// Linux.
	if runtime.GOOS != "darwin" {
		tray.OnClick(func() { post(a.reopen) })
	}
	a.tray = tray
	a.trayKey = state.key()
	a.showTrayCount(shownCount(state.unread))
}

// refreshTray brings the tray's menu and count up to date with the store.
func (a *appDelegate) refreshTray() {
	if a.tray == nil {
		return
	}
	state := currentTrayState()
	a.showTrayCount(shownCount(state.unread))
	if key := state.key(); key != a.trayKey {
		a.trayKey = key
		a.tray.SetMenu(a.trayMenu(state))
	}
}

// showTrayCount puts the unread total beside the icon on macOS and in the tooltip elsewhere; zero
// clears it.
func (a *appDelegate) showTrayCount(count int) {
	if runtime.GOOS == "darwin" {
		title := ""
		if count > 0 {
			title = strconv.Itoa(count)
		}
		a.tray.SetTitle(title)
		return
	}
	tip := appName()
	if count > 0 {
		tip += " · " + strconv.Itoa(count)
	}
	a.tray.SetToolTip(tip)
}

// trayPreferenceChanged puts the icon in or takes it out as the user chose. Without it, closing
// the last window quits the app on Windows and Linux, where nothing else brings it back.
func (a *appDelegate) trayPreferenceChanged() {
	if prefs.get().ShowsTrayIcon {
		a.installTray()
		return
	}
	if a.tray != nil {
		a.tray.Destroy()
		a.tray = nil
		a.trayKey = ""
	}
}
