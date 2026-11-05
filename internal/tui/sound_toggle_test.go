package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ctrl+y toggles sounds from any mode, including while composing.
func TestCtrlYTogglesSoundFromAnyMode(t *testing.T) {
	m := ready(t)
	before := m.soundEnabled
	m.mode = insert
	next, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = next.(Model)
	if m.soundEnabled == before {
		t.Fatal("ctrl+y did not toggle sound")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if next.(Model).soundEnabled != before {
		t.Fatal("second ctrl+y did not toggle back")
	}
}
