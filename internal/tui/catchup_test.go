package tui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCatchUpStoresAnswerAndSkipsUnchangedUnreads(t *testing.T) {
	m := blockModel()
	m.viewport.SetWidth(80)
	sig := m.unreadSignature()
	m.applyCatchUp(catchUpMsg{signature: sig, text: "- **What you missed:** deploy done", generated: time.Now()})
	if m.catchUp.sig != sig || m.catchUp.busy {
		t.Fatal("catch-up state not stored")
	}
	view := ansiStrip(m.catchUpView())
	if !strings.Contains(view, "What you missed") || strings.Contains(view, "**") {
		t.Fatalf("catch-up not rendered as Markdown: %q", view)
	}
	// Same unread state: no new run is scheduled when the view opens again.
	if cmd := m.autoCatchUp(); cmd != nil {
		t.Fatal("unchanged unreads scheduled another catch-up")
	}
}

func TestCatchUpFailureIsVisibleAndNotLooped(t *testing.T) {
	m := blockModel()
	m.viewport.SetWidth(80)
	m.catchUp.busy = true
	m.applyCatchUp(catchUpMsg{signature: "x", err: errors.New("provider down")})
	if m.catchUp.busy || m.catchUp.err == "" || m.catchUp.sig == "x" {
		t.Fatal("failed catch-up not recorded for retry")
	}
	if !strings.Contains(ansiStrip(m.catchUpView()), "Could not write the catch-up") {
		t.Fatal("failure not shown")
	}
}

func TestCatchUpWithNothingUnreadSaysCaughtUp(t *testing.T) {
	m := Model{unread: map[string]unreadState{}, remoteRead: map[string]string{}}
	m.viewport.SetWidth(80)
	if !strings.Contains(ansiStrip(m.catchUpView()), "All caught up") {
		t.Fatal("empty unreads not reported as caught up")
	}
}
