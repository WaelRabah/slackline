package tui

import (
	"errors"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
)

// A failed background summary is reported once and not queued again by itself.
func TestFailedBackgroundSummaryIsNotRequeued(t *testing.T) {
	m := Model{active: "C1", latestSeen: map[string]string{"C1": "1700000200.000001"}, summaries: map[string]conversationSummary{}, summaryBusy: map[string]bool{}, bgActive: map[string]bool{}}
	job := bgJob{kind: bgSummary, channel: "C1"}
	m.bgActive[jobKey(bgSummary, "C1")] = true
	m.summaryBusy["C1"] = true
	m.bgRunning = 1
	m.applyBG(bgResult{job: job, err: errors.New("provider down")})
	if m.summaryBusy["C1"] || m.bgActive[jobKey(bgSummary, "C1")] || len(m.bgQueue) != 0 {
		t.Fatal("failed summary stayed busy or was queued again")
	}
}

// New messages make a stored summary stale; that is the only trigger for a rerun.
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
