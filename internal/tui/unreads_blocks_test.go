package tui

import (
	"strings"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func blockModel() Model {
	return Model{
		destination:   unreadsDestination,
		users:         map[string]string{},
		remoteRead:    map[string]string{},
		newMessages:   map[string]bool{},
		markingUnread: map[string]bool{},
		unread: map[string]unreadState{
			"C1": {after: "1700000000.000001", latest: "1700000300.000001", count: 3},
			"C2": {after: "1700000000.000001", latest: "1700000200.000001", count: 2},
			"C3": {after: "1700000000.000001", latest: "1700000100.000001", count: 1},
		},
		conversations: []slack.Conversation{{ID: "C1", Name: "general"}, {ID: "C2", Name: "ops"}, {ID: "C3", Name: "random"}},
		unreadSections: []unreadSection{
			{channel: "C1", label: "general", text: "- **To reply:** ship it"},
			{channel: "C2", label: "ops", text: "- FYI: deploy done"},
		},
	}
}

func TestUnreadBlocksCycleAndWrap(t *testing.T) {
	m := blockModel()
	if _, handled := m.unreadsKey("n"); !handled || m.unreadIndex != 1 {
		t.Fatal("next did not move to block 2")
	}
	m.unreadsKey("n")
	m.unreadsKey("n")
	if m.unreadIndex != 0 {
		t.Fatal("next did not wrap to the first block")
	}
	m.unreadsKey("p")
	if m.unreadIndex != 2 {
		t.Fatal("previous did not wrap to the last block")
	}
	if _, handled := m.unreadsKey("z"); handled {
		t.Fatal("unrelated key was captured by the unreads view")
	}
}

func TestUnreadsViewShowsEachBlockInItsOwnBox(t *testing.T) {
	m := blockModel()
	m.viewport.SetWidth(80)
	view := ansiStrip(m.unreadsView())
	if !strings.Contains(view, "Unreads Summaries · 3 channels") {
		t.Fatalf("unexpected header: %q", view)
	}
	for _, label := range []string{"general", "ops", "random"} {
		if !strings.Contains(view, label) {
			t.Fatalf("block %q missing", label)
		}
	}
	// Each block has its own top and bottom border.
	if strings.Count(view, "╭") < 3 || strings.Count(view, "╰") < 3 {
		t.Fatal("blocks are not drawn in separate boxes")
	}
	if strings.Contains(view, "**") {
		t.Fatal("markdown markers were not rendered")
	}
}

func TestMarkedBlockDisappearsAndStaysGone(t *testing.T) {
	m := blockModel()
	m.applyMarkedBlock(markedBlockMsg{channel: "C1", latest: "1700000300.000001"})
	if _, ok := m.unread["C1"]; ok {
		t.Fatal("marked block still in unread state")
	}
	for _, section := range m.unreadSections {
		if section.channel == "C1" {
			t.Fatal("marked block summary still present")
		}
	}
	if m.remoteRead["C1"] != "1700000300.000001" {
		t.Fatal("read position not recorded, so refresh could bring the block back")
	}
	if m.unreadSig != m.unreadSignature() {
		t.Fatal("removing a block would trigger a rerun of the remaining summaries")
	}
	if len(m.unreadTargets()) != 2 {
		t.Fatal("expected two blocks left")
	}
}

func TestFailedMarkKeepsBlock(t *testing.T) {
	m := blockModel()
	m.markingUnread["C1"] = true
	m.applyMarkedBlock(markedBlockMsg{channel: "C1", latest: "1700000300.000001", err: errTest("slack refused")})
	if _, ok := m.unread["C1"]; !ok {
		t.Fatal("block removed although Slack did not confirm the read")
	}
	if m.markingUnread["C1"] {
		t.Fatal("marking state was not cleared after failure")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func ansiStrip(s string) string { return ansi.Strip(s) }
