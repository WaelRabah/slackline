package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func click(m Model, x, y int) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	return next.(Model), cmd
}

func latestFixture(t *testing.T) (Model, slack.Conversation, slack.Message) {
	t.Helper()
	m := ready(t)
	conv := m.conversations[0]
	msg := slack.Message{TS: "1793833200.000100", User: "UALICE", Text: "ping <@UYOU>"}
	m.noteRecent(recentItem{at: time.Unix(1793833200, 0), kind: "message", key: conv.ID + "/" + msg.TS, conversation: conv, message: msg, label: "#x", text: "ping", source: "Incoming message"})
	return m, conv, msg
}

func TestClickingLatestEntryOpensItsChannelAtThatMessage(t *testing.T) {
	m, conv, msg := latestFixture(t)
	m.focus = sidebar
	m.destination = latestDestination
	m.renderDestination()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	y := sidebarBodyTop + 3 - m.viewport.YOffset() // first entry's head line
	if y >= len(lines) || !strings.Contains(lines[y], "#x") {
		t.Fatalf("first Latest entry not drawn at row %d:\n%s", y, strings.Join(lines, "\n"))
	}
	m, cmd := click(m, m.sidebarWidth()+6, y)
	if m.destination != "" || m.active != conv.ID {
		t.Fatalf("click should open %s, got active=%q destination=%q", conv.ID, m.active, m.destination)
	}
	if m.revealMessage != msg.TS {
		t.Fatalf("message to reveal not set: %q", m.revealMessage)
	}
	if cmd == nil {
		t.Fatal("opening should load history")
	}
}

func TestClickBelowLatestEntriesDoesNothing(t *testing.T) {
	m, _, _ := latestFixture(t)
	m.destination = latestDestination
	m.renderDestination()
	m, _ = click(m, m.sidebarWidth()+6, sidebarBodyTop+m.viewport.Height()-1)
	if m.destination != latestDestination {
		t.Fatal("click on empty space left Latest")
	}
}

func TestClickingUnreadBlockOpensChannel(t *testing.T) {
	m := ready(t)
	conv := m.conversations[1]
	m.unread = map[string]unreadState{conv.ID: {after: "1793833100.000001", latest: "1793833300.000001", count: 2}}
	m.unreadSections = []unreadSection{{channel: conv.ID, label: "dev", text: "- FYI"}}
	m.destination = unreadsDestination
	m.focus = sidebar
	m.renderDestination()
	_, _, spans := m.unreadsBlocks()
	if len(spans) != 1 {
		t.Fatalf("want one block, got %v", spans)
	}
	y := sidebarBodyTop + spans[0][0] + 1 - m.viewport.YOffset()
	m, _ = click(m, m.sidebarWidth()+6, y)
	if m.active != conv.ID || m.destination != "" || m.revealMessage != "1793833300.000001" {
		t.Fatalf("unread click: active=%q dest=%q reveal=%q", m.active, m.destination, m.revealMessage)
	}
}

func TestClickingChannelMessageSelectsIt(t *testing.T) {
	m := ready(t)
	if len(m.history) < 2 {
		t.Skip("demo needs two messages")
	}
	m.focus = sidebar
	first := m.history[0].TS
	m.selectedMessage = m.history[len(m.history)-1].TS
	m.renderHistory()
	m.viewport.SetYOffset(0)
	y := sidebarBodyTop + m.blockStarts[first] + 1
	m, _ = click(m, m.sidebarWidth()+6, y)
	if m.selectedMessage != first || m.focus != messages {
		t.Fatalf("click should select the first message: selected=%q focus=%v", m.selectedMessage, m.focus)
	}
}

func TestPickerOpensFromLatestAndUnreads(t *testing.T) {
	m, conv, msg := latestFixture(t)
	m.destination = latestDestination
	m.focus = sidebar
	m = press(t, m, "e")
	if m.mode != reactMode || m.picker.channel != conv.ID || m.picker.ts != msg.TS {
		t.Fatalf("Latest should target its message: mode=%v picker=%+v", m.mode, m.picker)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "ping") {
		t.Fatalf("picker should name the target:\n%s", view)
	}
	m = typeText(t, m, "tada")
	m = press(t, m, "enter")
	for _, r := range m.recent[0].message.Reactions {
		if r.Name == "tada" && r.Count == 1 {
			goto unreads
		}
	}
	t.Fatalf("Latest copy should show the reaction: %v", m.recent[0].message.Reactions)

unreads:
	m.destination = unreadsDestination
	m.unread = map[string]unreadState{conv.ID: {after: "1.1", latest: "1793833300.000001", count: 1}}
	m = press(t, m, "e")
	if m.mode != reactMode || m.picker.ts != "1793833300.000001" {
		t.Fatalf("Unreads should target the block's newest message: %+v", m.picker)
	}
}

func TestPickerOpensFromSidebarFocusAndCtrlEWhileTyping(t *testing.T) {
	m := ready(t)
	m = selectFirstMessage(t, m)
	m.focus = sidebar
	m = press(t, m, "e", "esc")
	if m.mode != normal {
		t.Fatalf("picker from the sidebar should close back to normal, mode=%v", m.mode)
	}
	m = press(t, m, "i")
	m = typeText(t, m, "draft ")
	next, _ := m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m = next.(Model)
	if m.mode != reactMode {
		t.Fatalf("Ctrl+E should open the picker while typing, mode=%v", m.mode)
	}
	m = press(t, m, "esc")
	if m.mode != insert || m.input.Value() != "draft " {
		t.Fatalf("closing must return to the draft: mode=%v draft=%q", m.mode, m.input.Value())
	}
}
