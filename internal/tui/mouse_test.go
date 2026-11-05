package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A left click on a conversation in the sidebar opens that conversation, at the
// row where the sidebar is drawn.
func TestClickingSidebarConversationOpensIt(t *testing.T) {
	m := ready(t)
	rows := m.sidebarRows()
	count := max(1, m.viewport.Height())
	shown := m.sidebarShown(rows, m.sidebarPosition(rows), count)
	target := -1
	for k, i := range shown {
		if rows[i].index >= 0 {
			target = k
			break
		}
	}
	if target < 0 {
		t.Skip("demo sidebar has no conversation rows")
	}
	wantID := m.conversations[rows[shown[target]].index].ID

	// The rendered frame must place that row at the Y the click handler uses.
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	label := rows[shown[target]].label
	label = strings.TrimSpace(strings.TrimLeft(label, "•● "))
	if y := sidebarBodyTop + target; y >= len(lines) || !strings.Contains(lines[y], label) {
		t.Fatalf("row %q not drawn at screen row %d", label, sidebarBodyTop+target)
	}

	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 3, Y: sidebarBodyTop + target, Button: tea.MouseLeft}))
	m = next.(Model)
	if m.active != wantID {
		t.Fatalf("click did not open the conversation: active=%q want=%q", m.active, wantID)
	}
}
