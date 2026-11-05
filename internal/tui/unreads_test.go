package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
)

func TestUnreadTargetsSortByCountAndLabelDMs(t *testing.T) {
	m := Model{
		users: map[string]string{"U2": "Ana"},
		unread: map[string]unreadState{
			"C1": {after: "1700000000.000001", latest: "1700000100.000001", count: 2},
			"D1": {after: "1700000000.000001", latest: "1700000200.000001", count: 9},
		},
		conversations: []slack.Conversation{
			{ID: "C1", Name: "general"},
			{ID: "D1", IsIM: true, User: "U2"},
			{ID: "C9", Name: "quiet"},
		},
	}
	targets := m.unreadTargets()
	if len(targets) != 2 || targets[0].id != "D1" || targets[0].label != "DM with Ana" || targets[1].label != "general" {
		t.Fatalf("unexpected order or labels: %+v", targets)
	}
}

func TestMergeUnreadMessagesDedupesAndSortsOldestFirst(t *testing.T) {
	channel := []slack.Message{{TS: "1700000300.000001", Text: "newer"}, {TS: "1700000200.000001", Text: "older"}}
	threads := []slack.Message{{TS: "1700000200.000001", Text: "duplicate"}, {TS: "1700000250.000001", Text: "reply"}}
	got := mergeUnreadMessages(channel, threads)
	if len(got) != 3 || got[0].Text != "older" || got[1].Text != "reply" || got[2].Text != "newer" {
		t.Fatalf("unexpected merge: %+v", got)
	}
}

func TestUnreadTranscriptStaysWithinBudgetAndCarriesLinks(t *testing.T) {
	long := strings.Repeat("x", 9000)
	var messages []slack.Message
	for i := range 10 {
		messages = append(messages, slack.Message{TS: fmt.Sprintf("17000000%02d.000001", i), User: "U1", Text: long})
	}
	transcript, trimmed := buildUnreadTranscript("T1", map[string]string{"U1": "Ben"}, "C1", messages)
	if !trimmed || len(transcript) > unreadBudget {
		t.Fatal("transcript exceeded its budget without being marked partial")
	}
	if !strings.Contains(transcript, "https://app.slack.com/client/T1/C1/thread/C1-") {
		t.Fatal("source links missing")
	}
}

func TestAutoUnreadsDoesNotRerunWhenNothingChanged(t *testing.T) {
	m := Model{
		unread: map[string]unreadState{"C1": {after: "1", latest: "2", count: 1}},
	}
	sig := m.unreadSignature()
	m.unreadSig = sig
	// Demo mode and a missing provider both return before any work starts, so this
	// test only checks the signature-based guard.
	if m.autoUnreads() != nil {
		t.Fatal("unchanged unreads scheduled a run")
	}
	m.unread["C1"] = unreadState{after: "1", latest: "3", count: 1}
	if m.unreadSignature() == sig {
		t.Fatal("new unread message did not change the signature")
	}
}
