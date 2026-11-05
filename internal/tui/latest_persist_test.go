package tui

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

// Unopened Latest entries survive a restart: they are saved with their
// conversation and message, and restored in order.
func TestLatestEntriesSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidebar.json")
	conv := slack.Conversation{ID: "C1", Name: "ops"}
	msg := slack.Message{TS: "1793833200.000100", User: "U2", Text: "deploy done"}
	m := ready(t)
	m.statePath = path
	m.conversations = []slack.Conversation{conv}
	m.noteRecent(recentItem{at: time.Unix(1793833200, 0), kind: "message", source: "Incoming message", key: "C1/" + msg.TS, conversation: conv, message: msg, label: "#ops · Ana", text: "deploy done"})
	m.noteRecent(recentItem{at: time.Unix(1793833300, 0), kind: "summary", source: "AI catch-up ready", key: "summary/x", label: "AI summary"})

	restarted := ready(t)
	restarted.statePath = path
	restarted.recent = nil
	restarted.loadState()
	if len(restarted.recent) != 2 || restarted.recent[0].key != "summary/x" || restarted.recent[1].message.Text != "deploy done" {
		t.Fatalf("restored entries wrong: %+v", restarted.recent)
	}
	if restarted.recent[1].conversation.ID != "C1" {
		t.Fatal("conversation lost; the entry could not be opened")
	}
	if !restarted.recentSeen["C1/"+msg.TS] {
		t.Fatal("restored entries are not de-duplicated")
	}
}

func TestPersistedEntryIsValidJSON(t *testing.T) {
	data, err := json.Marshal(persistRecent([]recentItem{{key: "k", kind: "message"}}))
	if err != nil || len(data) == 0 {
		t.Fatal("cannot encode entries", err)
	}
}
