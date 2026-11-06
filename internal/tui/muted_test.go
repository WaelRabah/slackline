package tui

import (
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

// Latest drops entries from muted channels, and new messages from them are not added.
func TestMutedChannelsStayOutOfLatest(t *testing.T) {
	m := ready(t)
	conv := slack.Conversation{ID: "C1", Name: "ops"}
	m.noteRecent(recentItem{at: time.Unix(10, 0), kind: "message", key: "C1/1", conversation: conv, message: slack.Message{TS: "1"}})
	m.noteRecent(recentItem{at: time.Unix(20, 0), kind: "summary", key: "summary/x"})
	m.applyMuted(mutedMsg{channels: map[string]bool{"C1": true}})
	if len(m.recent) != 1 || m.recent[0].key != "summary/x" {
		t.Fatalf("muted channel still in Latest: %+v", m.recent)
	}
	m.mutedChannels = map[string]bool{"C2": true}
	m.noteIncomingMessage("C2", slack.Message{TS: "5", User: "U2", Text: "hi"}, "")
	if len(m.recent) != 1 {
		t.Fatal("message from a muted channel was added to Latest")
	}
}

// A channel muted in Slack must reach Latest within a minute, not a session.
func TestMutedListRefreshesWithinAMinute(t *testing.T) {
	if mutedRefresh > time.Minute {
		t.Fatalf("mute list refresh is %s; a mute made in Slack would show in Latest for that long", mutedRefresh)
	}
}
