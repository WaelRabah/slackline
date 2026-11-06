package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSidebarMarksActiveConversationWhenExpandedAndCollapsed(t *testing.T) {
	m := ready(t)
	m.width, m.height = 120, 40
	m.resize()
	active := m.activeIndex()
	if active < 0 {
		t.Fatal("fixture has no open conversation")
	}

	if !strings.Contains(ansi.Strip(m.View().Content), "▶ "+m.label(m.conversations[active])) {
		t.Fatal("open conversation not marked in the expanded sidebar")
	}

	for _, g := range m.sidebarGroups() {
		if m.indexInGroup(active, g.id) {
			m.collapsed[g.id] = true
		}
	}
	m.resize()
	if !strings.Contains(ansi.Strip(m.View().Content), "· ▶ ") {
		t.Fatal("collapsed section heading does not name the open conversation")
	}
}

func TestSectionHeadingGlowsWithUnreadTotal(t *testing.T) {
	m := ready(t)
	m.width, m.height = 120, 40
	m.resize()
	active := m.activeIndex()
	var group string
	for _, g := range m.sidebarGroups() {
		if m.indexInGroup(active, g.id) {
			group = g.id
		}
	}
	if group == "" {
		t.Fatal("open conversation is in no section")
	}
	id := m.conversations[active].ID
	m.unread[id] = unreadState{count: 2, exact: true}
	total, exact, any := m.groupUnread(group)
	if total != 2 || !exact || !any {
		t.Fatalf("groupUnread = %d %v %v, want 2 true true", total, exact, any)
	}
	m.resize()
	// Badges are right-aligned with padding, so match the heading line rather than an exact gap.
	found := false
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(line, "(") && strings.Contains(line, "[2]") {
			found = true
		}
	}
	if !found {
		t.Fatal("section heading does not show the unread total")
	}
}
