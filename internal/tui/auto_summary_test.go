package tui

import (
	"errors"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
)

func TestSummaryIsStaleOnlyWhenANewerMessageExists(t *testing.T) {
	m := Model{latestSeen: map[string]string{"C1": "1700000200.000001"}, summaries: map[string]conversationSummary{}}
	if !m.summaryStale("C1") {
		t.Fatal("conversation without a summary should be stale")
	}
	m.summaries["C1"] = conversationSummary{Newest: "1700000100.000001"}
	if !m.summaryStale("C1") {
		t.Fatal("summary older than the newest message should be stale")
	}
	m.summaries["C1"] = conversationSummary{Newest: "1700000200.000001"}
	if m.summaryStale("C1") {
		t.Fatal("summary covering the newest message should not be regenerated")
	}
	if m.summaryStale("C2") {
		t.Fatal("unknown conversation with no seen messages should not run")
	}
}

func TestFailedAutoSummaryDoesNotChainRetries(t *testing.T) {
	m := Model{active: "C1", latestSeen: map[string]string{"C1": "1700000200.000001"}, summaries: map[string]conversationSummary{}}
	if cmd := m.summaryHook(summaryMsg{channel: "C1", err: errors.New("provider down")}); cmd != nil {
		t.Fatal("failed run scheduled another run")
	}
}

func TestLiveMessageRecordsNewestAndIgnoresEdits(t *testing.T) {
	m := Model{active: "C1", latestSeen: map[string]string{}, summaries: map[string]conversationSummary{}, summaryBusy: map[string]bool{}}
	m.summaryHook(slack.LiveEvent{Type: "message", Message: slack.Message{Channel: "C1", TS: "1700000300.000001"}})
	if m.latestSeen["C1"] != "1700000300.000001" {
		t.Fatal("new message not recorded")
	}
	m.summaryHook(slack.LiveEvent{Type: "message", Message: slack.Message{Channel: "C1", TS: "1700000400.000001", Subtype: "message_changed"}})
	if m.latestSeen["C1"] != "1700000300.000001" {
		t.Fatal("edit counted as a new message")
	}
}
