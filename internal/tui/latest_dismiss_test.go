package tui

import (
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

// x removes the selected Latest entry without opening it.
func TestDismissLatestRemovesWithoutOpening(t *testing.T) {
	m := ready(t)
	conv := slack.Conversation{ID: "C1", Name: "ops"}
	m.noteRecent(recentItem{at: time.Unix(1793833200, 0), kind: "message", key: "C1/1", conversation: conv, message: slack.Message{TS: "1"}})
	m.noteRecent(recentItem{at: time.Unix(1793833100, 0), kind: "summary", key: "summary/x"})
	m.destination = latestDestination
	m.latestIndex = 0
	active := m.active
	m.latestKey("x")
	if len(m.recent) != 1 || m.recent[0].key != "summary/x" {
		t.Fatalf("dismiss did not remove the selected entry: %+v", m.recent)
	}
	if m.active != active {
		t.Fatal("dismiss opened a conversation")
	}
}
