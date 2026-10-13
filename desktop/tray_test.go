package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/lorca/desktop/model"
	"github.com/egoist/mygo"
)

func trayLabels(menu *mygo.Menu) []string {
	var labels []string
	for _, item := range menu.Items() {
		if item.Type == mygo.MenuItemSeparator {
			labels = append(labels, "-")
		} else {
			labels = append(labels, item.Label)
		}
	}
	return labels
}

// The tray's menu lists the chats with unread messages under their total, and the number beside
// the icon is the same total the Dock badge shows, muted chats left out.
func TestTrayListsUnreadChats(t *testing.T) {
	demoWindow(t)
	runPosts()
	state := currentTrayState()
	if state.unread != unreadTotal() || state.unread == 0 {
		t.Fatalf("tray count %d, badge count %d", state.unread, unreadTotal())
	}
	sum := 0
	for _, chat := range state.chats {
		sum += chat.unread
	}
	if len(state.chats) == 0 || sum != state.unread || state.more != 0 {
		t.Fatalf("chats %+v, more %d, total %d", state.chats, state.more, state.unread)
	}
	labels := trayLabels(app.trayMenu(state))
	if labels[0] != L("%d unread", state.unread) {
		t.Errorf("menu opens on %q: %q", labels[0], labels)
	}
	for _, chat := range state.chats {
		if !slices.Contains(labels, fmt.Sprintf("%s · %d", chat.title, chat.unread)) {
			t.Errorf("no row for %q: %q", chat.title, labels)
		}
	}
	for _, want := range []string{L("Open %@", appName()), L("New Bot…"), L("Settings…"), L("Quit %@", appName())} {
		if !slices.Contains(labels, want) {
			t.Errorf("no %q: %q", want, labels)
		}
	}
	// Reading a chat takes it off the list and out of the count.
	before := state.unread
	store.Chats[slices.IndexFunc(store.Chats, func(c *model.Chat) bool { return c.ID == state.chats[0].id })].UnreadCount = 0
	if after := currentTrayState(); after.unread >= before || len(after.chats) != len(state.chats)-1 || after.key() == state.key() {
		t.Errorf("after reading: %+v", after)
	}
}

// A long list is cut with a "more"; a muted chat is neither listed nor counted.
func TestTrayCutsALongList(t *testing.T) {
	demoWindow(t)
	runPosts()
	for i, chat := range store.Chats {
		chat.UnreadCount = i%3 + 1
	}
	for i := range 4 {
		extra := *store.Chats[0]
		extra.ID = fmt.Sprintf("extra-%d", i)
		extra.UnreadCount = 1
		store.Chats = append(store.Chats, &extra)
	}
	listed := len(unreadChats())
	if listed <= trayChatLimit {
		t.Fatalf("%d chats to list, want more than %d", listed, trayChatLimit)
	}
	state := currentTrayState()
	if len(state.chats) != trayChatLimit || state.more != listed-trayChatLimit {
		t.Fatalf("%d chats listed, %d more of %d", len(state.chats), state.more, listed)
	}
	if !slices.Contains(trayLabels(app.trayMenu(state)), L("%d more chats…", state.more)) {
		t.Errorf("no more row: %q", trayLabels(app.trayMenu(state)))
	}
}

// Without an account the menu offers the app alone.
func TestTrayWithoutAnAccount(t *testing.T) {
	demoWindow(t)
	runPosts()
	no := false
	store.HasIdentity = &no
	state := currentTrayState()
	if state.signedIn || state.unread != 0 || len(state.chats) != 0 {
		t.Fatalf("state %+v", state)
	}
	if slices.Contains(trayLabels(app.trayMenu(state)), L("New Bot…")) {
		t.Error("New Bot offered without an account")
	}
}
