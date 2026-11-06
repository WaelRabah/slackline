package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

// The list scrolls to the selected entry: its first line is the offset used by
// the viewport, and it is inside the rendered text.
func TestLatestScrollsToSelectedEntry(t *testing.T) {
	m := Model{recentSeen: map[string]bool{}}
	m.viewport.SetWidth(80)
	for i := range 30 {
		m.noteRecent(recentItem{at: time.Unix(int64(1000-i), 0), kind: "summary", key: "k" + strings.Repeat("x", i), label: "AI summary", source: "AI catch-up ready"})
	}
	m.latestIndex = 25
	view, offset := m.latestLayout()
	lines := strings.Split(view, "\n")
	if offset <= 0 || offset >= len(lines) || !strings.Contains(lines[offset], "▶") {
		t.Fatalf("selected entry not at offset %d", offset)
	}
}

// Entries that arrive while Latest is closed are counted; opening Latest clears the count.
func TestLatestCountsUnseenEntries(t *testing.T) {
	m := Model{recentSeen: map[string]bool{}, destination: ""}
	m.noteRecent(recentItem{at: time.Unix(1, 0), key: "a"})
	m.noteRecent(recentItem{at: time.Unix(2, 0), key: "b"})
	if m.latestUnseen != 2 || m.latestLabel() != "⏱ Latest" {
		t.Fatalf("unseen=%d label=%q", m.latestUnseen, m.latestLabel())
	}
	m.destination = latestDestination
	m.latestUnseen = 0
	m.noteRecent(recentItem{at: time.Unix(3, 0), key: "c"})
	if m.latestUnseen != 0 {
		t.Fatal("entry arriving while Latest is open was counted")
	}
	_ = slack.Message{}
}
