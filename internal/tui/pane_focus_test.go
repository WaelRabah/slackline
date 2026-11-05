package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// With the sidebar focused, j/k move the sidebar even while Latest is open.
func TestSidebarKeysWorkWhileLatestIsOpen(t *testing.T) {
	m := ready(t)
	m.destination = latestDestination
	m.focus = sidebar
	m.latestIndex = 0
	before := m.sidebarPosition(m.sidebarRows())
	next, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = next.(Model)
	if m.latestIndex != 0 {
		t.Fatal("j moved the Latest list while the sidebar had focus")
	}
	if m.sidebarPosition(m.sidebarRows()) == before {
		t.Fatal("j did not move the sidebar cursor")
	}
}
