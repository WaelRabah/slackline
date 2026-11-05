package tui

import (
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

// Opening an entry from Latest must not remove it from the list.
func TestOpeningLatestEntryKeepsIt(t *testing.T) {
	m := ready(t)
	if len(m.conversations) == 0 {
		t.Skip("demo has no conversations")
	}
	conv := m.conversations[0]
	msg := slack.Message{TS: "1793833200.000100", User: "U2", Text: "hello"}
	m.noteRecent(recentItem{at: time.Unix(1793833200, 0), kind: "message", key: conv.ID + "/" + msg.TS, conversation: conv, message: msg, label: "#x", text: "hello", source: "Incoming message"})
	m.destination = latestDestination
	m.latestIndex = 0
	m.latestKey("enter")
	if len(m.recent) != 1 {
		t.Fatalf("opening the entry removed it from Latest: %d entries", len(m.recent))
	}
}
