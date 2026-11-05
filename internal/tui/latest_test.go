package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func TestLatestKeepsNewestFirstWithoutDuplicates(t *testing.T) {
	m := Model{}
	base := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	m.noteRecent(recentItem{at: base, key: "a", label: "#one", kind: "message"})
	m.noteRecent(recentItem{at: base.Add(time.Minute), key: "b", label: "#two", kind: "message"})
	m.noteRecent(recentItem{at: base.Add(time.Minute), key: "b", label: "#two", kind: "message"})
	if len(m.recent) != 2 || m.recent[0].key != "b" || m.recent[1].key != "a" {
		t.Fatalf("unexpected order: %+v", m.recent)
	}
}

func TestLatestCapsTheList(t *testing.T) {
	m := Model{}
	for i := range maxLatest + 5 {
		m.noteRecent(recentItem{at: time.Unix(int64(i), 0), key: string(rune('a'+i%26)) + time.Unix(int64(i), 0).String()})
	}
	if len(m.recent) != maxLatest {
		t.Fatalf("list grew to %d", len(m.recent))
	}
}

func TestLatestShowsWhereEachChimeCameFrom(t *testing.T) {
	m := Model{soundEnabled: true, users: map[string]string{"U2": "Ana"}}
	m.viewport.SetWidth(100)
	m.noteIncomingMessage("C1", slack.Message{TS: "1793833200.000100", User: "U2", Text: "deploy done"}, "Slack knock_brush.mp3")
	m.noteSummaryReady("AI catch-up ready", time.Unix(1793833300, 0))
	view := ansi.Strip(m.latestView())
	for _, want := range []string{"Latest · 2", "deploy done", "from: Incoming message · sound: Slack knock_brush.mp3", "from: AI catch-up ready"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in:\n%s", want, view)
		}
	}
}

func TestSlackSoundIsUsedWhenInstalled(t *testing.T) {
	dir := t.TempDir()
	if _, ok := slackSoundPath(dir, messageSoundName); ok {
		t.Fatal("found a sound that is not there")
	}
	path := filepath.Join(dir, messageSoundName)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := slackSoundPath(dir, messageSoundName); !ok || got != path {
		t.Fatalf("Slack sound not found: %q %v", got, ok)
	}
}
