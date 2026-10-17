package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func applyLive(m *Model, event slack.LiveEvent) {
	next, _ := m.Update(event)
	*m = next.(Model)
}
func TestOnlyNewIncomingMessageEventsChime(t *testing.T) {
	m, calls := activityModel(t)
	applyLive(m, slack.LiveEvent{Type: "_connection", State: "live"})
	message := slack.Message{Channel: m.active, User: "UALICE", TS: ts(m.now().Add(time.Second)), Text: "actual incoming"}
	applyLive(m, slack.LiveEvent{Type: "message", Message: message})
	if *calls != 1 || m.history[len(m.history)-1].Text != "actual incoming" {
		t.Fatal("live message did not chime or update conversation immediately")
	}
	applyLive(m, slack.LiveEvent{Type: "message", Message: message})
	changed := message
	changed.Text = "edited"
	applyLive(m, slack.LiveEvent{Type: "message", Message: slack.Message{Channel: message.Channel, Subtype: "message_changed"}, Changed: &changed})
	applyLive(m, slack.LiveEvent{Type: "message", Message: slack.Message{Channel: message.Channel, Subtype: "message_deleted"}, DeletedTS: message.TS})
	applyLive(m, slack.LiveEvent{Type: "user_typing", Message: slack.Message{Channel: message.Channel, User: "UBOB"}})
	applyLive(m, slack.LiveEvent{Type: "pong"})
	applyLive(m, slack.LiveEvent{Type: "_connection", State: "reconnecting"})
	applyLive(m, slack.LiveEvent{Type: "_connection", State: "live"})
	if *calls != 1 {
		t.Fatal("duplicate/edit/delete/typing/reconnect chimed")
	}
	message.TS = ts(m.now().Add(2 * time.Second))
	message.User = m.session.UserID
	applyLive(m, slack.LiveEvent{Type: "message", Message: message})
	message.TS = ts(m.now().Add(3 * time.Second))
	message.User = "UALICE"
	m.soundEnabled = false
	applyLive(m, slack.LiveEvent{Type: "message", Message: message})
	if *calls != 1 {
		t.Fatal("own message or muted message chimed")
	}
}
func TestTypingIsNamedScopedExpiresAndClearsOnMessage(t *testing.T) {
	m, calls := activityModel(t)
	now := m.now()
	m.now = func() time.Time { return now }
	applyLive(m, slack.LiveEvent{Type: "_connection", State: "live"})
	event := slack.LiveEvent{Type: "user_typing", Message: slack.Message{Channel: m.active, User: "UALICE"}}
	applyLive(m, event)
	if m.typingText(m.active, "") != "Alice is typing…" || m.typingText("OTHER", "") != "" || m.typingText(m.active, "100.1") != "" {
		t.Fatal("typing name/scope wrong")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Alice is typing…") {
		t.Fatal("typing not visible in conversation")
	}
	event.Message.User = "UBOB"
	applyLive(m, event)
	if !strings.Contains(m.typingText(m.active, ""), "Alice and Bob are typing") {
		t.Fatal("multiple typers missing")
	}
	now = now.Add(4 * time.Second)
	applyLive(m, event)
	now = now.Add(3 * time.Second)
	next, _ := m.Update(typingTick{m.typingGeneration})
	*m = next.(Model)
	if m.typingText(m.active, "") != "Bob is typing…" {
		t.Fatal("typing refresh or expiry failed")
	}
	applyLive(m, slack.LiveEvent{Type: "message", Message: slack.Message{Channel: m.active, User: "UBOB", TS: ts(now.Add(time.Second)), Text: "done"}})
	if m.typingText(m.active, "") != "" {
		t.Fatal("typing remained after message")
	}
	event.Message.User = m.session.UserID
	applyLive(m, event)
	if m.typingText(m.active, "") != "" {
		t.Fatal("own typing displayed")
	}
	if *calls != 1 {
		t.Fatal("typing events chimed")
	}
}
func TestThreadRepliesUpdateCurrentThreadWithoutChimingPolls(t *testing.T) {
	m, calls := activityModel(t)
	cmd := m.openDestination(threadsDestination)
	next, _ := m.Update(cmd())
	*m = next.(Model)
	cmd = m.openFeedThread()
	next, _ = m.Update(cmd())
	*m = next.(Model)
	before := len(m.history)
	reply := slack.Message{Channel: m.active, ThreadTS: m.thread.ts, User: "UBOB", TS: ts(m.now().Add(time.Second)), Text: "instant thread reply"}
	applyLive(m, slack.LiveEvent{Type: "message", Message: reply})
	if len(m.history) != before+1 || m.history[len(m.history)-1].Text != reply.Text || *calls != 1 {
		t.Fatal("thread did not update instantly")
	}
	applyLive(m, slack.LiveEvent{Type: "message", Message: reply})
	if len(m.history) != before+1 || *calls != 1 {
		t.Fatal("thread replay duplicated or chimed")
	}
}
func TestStaleSnapshotsCannotOverwriteLiveMessageAndUnreadState(t *testing.T) {
	m, _ := activityModel(t)
	m.soundEnabled = false
	channel := m.active
	read := ts(m.now())
	m.remoteRead[channel] = read
	message := slack.Message{Channel: channel, User: "UALICE", TS: ts(m.now().Add(time.Second)), Text: "newest live text"}
	applyLive(m, slack.LiveEvent{Type: "message", Message: message})
	applyActivity(m, slack.Activity{ID: channel, LastRead: read, Latest: read, HasUnreads: false})
	if m.unreadBadge(channel) == "" {
		t.Fatal("in-flight old counts cleared new unread")
	}
	next, _ := m.Update(historyMsg{channel: channel, generation: m.generation, version: 0, history: slack.History{Messages: []slack.Message{{TS: message.TS, Text: "old page"}}}})
	*m = next.(Model)
	if m.history[len(m.history)-1].Text != message.Text {
		t.Fatal("in-flight history overwrote live message")
	}
	applyLive(m, slack.LiveEvent{Type: "message", Message: slack.Message{Channel: channel, Subtype: "message_deleted"}, DeletedTS: message.TS})
	next, _ = m.Update(historyMsg{channel: channel, generation: m.generation, version: 0, history: slack.History{Messages: []slack.Message{message}}})
	*m = next.(Model)
	for _, item := range m.history {
		if item.TS == message.TS {
			t.Fatal("stale history resurrected deleted message")
		}
	}
}

func TestSendAckAndLiveEchoDoNotDuplicateReplies(t *testing.T) {
	for _, echoFirst := range []bool{false, true} {
		m, calls := activityModel(t)
		cmd := m.openDestination(threadsDestination)
		next, _ := m.Update(cmd())
		*m = next.(Model)
		cmd = m.openFeedThread()
		next, _ = m.Update(cmd())
		*m = next.(Model)
		before := m.feed[0].Root.ReplyCount
		message := slack.Message{Channel: m.active, ThreadTS: m.thread.ts, User: m.session.UserID, TS: ts(m.now().Add(time.Second)), Text: "my reply"}
		ack := sentMsg{channel: m.active, thread: m.thread.ts, message: message}
		echo := slack.LiveEvent{Type: "message", Message: message}
		if echoFirst {
			applyLive(m, echo)
		}
		next, _ = m.Update(ack)
		*m = next.(Model)
		if !echoFirst {
			applyLive(m, echo)
		}
		if len(m.history) != 3 || *calls != 0 || m.feed[0].Root.ReplyCount != before+1 {
			t.Fatal("send ack/live echo duplicated reply, count, or sound")
		}
	}
}
func TestThreadTypingAndReconnectClearIndicators(t *testing.T) {
	m, _ := activityModel(t)
	applyLive(m, slack.LiveEvent{Type: "_connection", State: "live"})
	applyLive(m, slack.LiveEvent{Type: "user_typing", Message: slack.Message{Channel: m.active, ThreadTS: "100.1", User: "UALICE"}})
	if m.typingText(m.active, "100.1") == "" || m.typingText(m.active, "") != "" || !m.channelTyping(m.active) {
		t.Fatal("thread typing scope wrong")
	}
	applyLive(m, slack.LiveEvent{Type: "_connection", State: "reconnecting"})
	if m.typingText(m.active, "100.1") != "" || m.channelTyping(m.active) {
		t.Fatal("disconnected typing stayed visible")
	}
}
