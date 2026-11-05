package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The private summary card must be drawn inside the conversation pane, below the
// messages, for the open conversation.
func TestPrivateSummaryIsPinnedBelowMessages(t *testing.T) {
	m := ready(t)
	if len(m.conversations) == 0 {
		t.Skip("demo has no conversations")
	}
	m.active = m.conversations[0].ID
	m.summaries = map[string]conversationSummary{m.active: {Text: "Pinned summary line", Generated: time.Now(), Messages: 3, Newest: "1"}}
	m.resize()
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	cardLine, summaryLine := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "PRIVATE SUMMARY") && cardLine < 0 {
			cardLine = i
		}
		if strings.Contains(line, "Pinned summary line") {
			summaryLine = i
		}
	}
	if cardLine < 0 || summaryLine < 0 {
		t.Fatalf("summary card not rendered in the frame:\n%s", view)
	}
	// The card frame ends on the row above the messages pane's bottom border, so it is
	// the last thing in the conversation pane.
	if cardLine+9 >= len(lines) || !strings.Contains(lines[cardLine+8], "╰") || !strings.Contains(lines[cardLine+9], "╯ ╰") {
		t.Fatalf("summary card is not pinned at the bottom of the messages pane (card starts at line %d):\n%s", cardLine, view)
	}
}
