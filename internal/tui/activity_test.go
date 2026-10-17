package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

func activityModel(t *testing.T) (*Model, *int) {
	t.Helper()
	m := ready(t)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.startedAt = now
	m.interactions = map[string]time.Time{}
	calls := new(int)
	m.notify = func() tea.Cmd { *calls++; return nil }
	return &m, calls
}
func ts(at time.Time) string { return fmt.Sprintf("%d.%06d", at.Unix(), at.Nanosecond()/1000) }
func applyActivity(m *Model, rows ...slack.Activity) {
	next, _ := m.Update(activityMsg{rows: rows})
	*m = next.(Model)
}
func applyKey(m *Model, k tea.KeyPressMsg) tea.Cmd {
	next, cmd := m.Update(k)
	*m = next.(Model)
	return cmd
}

func TestActivityPollingAlwaysStaysSilent(t *testing.T) {
	m, calls := activityModel(t)
	base := m.now().Add(-time.Hour)
	for i := 0; i < 6; i++ {
		applyActivity(m, slack.Activity{ID: "CGENERAL", Latest: ts(base.Add(time.Duration(i) * time.Second)), HasUnreads: true})
	}
	applyActivity(m, slack.Activity{ID: "NEW", Latest: ts(m.now().Add(time.Second)), HasUnreads: true})
	if *calls != 0 {
		t.Fatal("activity polling must never chime")
	}
}
func TestBackgroundCooldownAndHistoryRefresh(t *testing.T) {
	m, calls := activityModel(t)
	m.loading = false
	base := m.now().Add(-time.Hour)
	applyActivity(m, slack.Activity{ID: m.active, Latest: ts(base)})
	applyActivity(m, slack.Activity{ID: m.active, Latest: ts(base.Add(time.Second)), HasUnreads: true})
	if !m.loading {
		t.Fatal("active messages did not refresh")
	}
	next, _ := m.Update(activityMsg{err: &slack.RateLimitError{RetryAfter: time.Minute}})
	*m = next.(Model)
	if time.Until(m.retryUntil) < 59*time.Second {
		t.Fatal("poll did not respect Retry-After")
	}
	before := *calls
	next, cmd := m.Update(activityTick{})
	*m = next.(Model)
	if cmd == nil || *calls != before {
		t.Fatal("cooldown should reschedule without notifying")
	}
}
func TestRecentFilterCombinesReadSentAndLocalOpens(t *testing.T) {
	m, _ := activityModel(t)
	m.conversations = []slack.Conversation{{ID: "READ", Name: "read"}, {ID: "SENT", Name: "sent"}, {ID: "OLD", Name: "old"}, {ID: "ACTIVE", Name: "active"}, {ID: "BOUNDARY", Name: "boundary"}}
	cutoff := m.now().Add(-14 * 24 * time.Hour)
	applyActivity(m, slack.Activity{ID: "READ", LastRead: ts(m.now().Add(-time.Hour))}, slack.Activity{ID: "OLD", LastRead: ts(cutoff.Add(-time.Microsecond))}, slack.Activity{ID: "ACTIVE", Latest: ts(m.now()), HasUnreads: true}, slack.Activity{ID: "BOUNDARY", LastRead: ts(cutoff)})
	if !m.visibleConversation("OLD") {
		t.Fatal("partial metadata must show all")
	}
	next, _ := m.Update(recentMsg{sent: map[string]time.Time{"SENT": m.now().Add(-time.Hour)}})
	*m = next.(Model)
	for _, id := range []string{"READ", "SENT", "BOUNDARY"} {
		if !m.visibleConversation(id) {
			t.Fatalf("missing interaction %s", id)
		}
	}
	if m.visibleConversation("OLD") {
		t.Fatal("old conversation passed interaction filter")
	}
	if !m.visibleConversation("ACTIVE") {
		t.Fatal("unread conversations must remain visible outside the recent filter")
	}
	m.recordInteraction("OLD")
	if !m.visibleConversation("OLD") {
		t.Fatal("local open not visible immediately")
	}
	applyKey(m, key('f'))
	if !m.visibleConversation("ACTIVE") {
		t.Fatal("all toggle still hides conversations")
	}
	applyKey(m, key('f'))
	if m.visibleConversation("UNKNOWN") {
		t.Fatal("recent toggle did not restore filter")
	}
}
func TestFinderCanOpenHiddenConversationAndRevealIt(t *testing.T) {
	m, _ := activityModel(t)
	m.activityReady, m.sentReady = true, true
	m.collapsed["local-dms"] = true
	if m.visibleConversation("DALICE") {
		t.Fatal("fixture should be hidden")
	}
	applyKey(m, key('/'))
	for _, r := range "Alice" {
		applyKey(m, key(r))
	}
	cmd := applyKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.active != "DALICE" || !m.visibleConversation("DALICE") {
		t.Fatal("finder could not open/reveal hidden conversation")
	}
	if m.collapsed["local-dms"] {
		t.Fatal("finder left opened conversation hidden in collapsed section")
	}
}
func TestFilterFailureShowsAllAndRetainsLocalData(t *testing.T) {
	m, _ := activityModel(t)
	m.recordInteraction("LOCAL")
	applyActivity(m)
	next, _ := m.Update(recentMsg{err: errors.New("search restricted")})
	*m = next.(Model)
	if m.filterReady() || !m.visibleConversation("OLD") || m.interactions["LOCAL"].IsZero() {
		t.Fatal("failed search hid conversations or lost local interaction")
	}
}
func TestSectionHeadingsRemainSelectableWhenAllCollapsed(t *testing.T) {
	m, _ := activityModel(t)
	m.focus = sidebar
	m.selectSidebar(pinnedRows)
	first, _ := m.sidebarSelection()
	if first.index >= 0 {
		t.Fatal("expected section heading")
	}
	applyKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.collapsed[first.group] {
		t.Fatal("Enter did not fold heading")
	}
	applyKey(m, key('j'))
	second, _ := m.sidebarSelection()
	if second.index >= 0 || second.group == first.group {
		t.Fatal("could not navigate to next heading")
	}
	applyKey(m, key(' '))
	applyKey(m, key('k'))
	applyKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.collapsed[first.group] {
		t.Fatal("could not reopen previous collapsed heading")
	}
	applyKey(m, key('j'))
	row, _ := m.sidebarSelection()
	if row.index < 0 {
		t.Fatal("could not navigate into expanded group")
	}
	applyKey(m, key(' '))
	row, _ = m.sidebarSelection()
	if row.index >= 0 || !m.collapsed[row.group] {
		t.Fatal("folding from conversation must select heading")
	}
	for _, group := range m.sidebarGroups() {
		m.collapsed[group.id] = true
	}
	applyKey(m, key('G'))
	row, _ = m.sidebarSelection()
	if row.index >= 0 {
		t.Fatal("G could not select final folded heading")
	}
}
func TestReloadDoesNotReopenConversationAfterEscape(t *testing.T) {
	m := ready(t)
	applyKey(&m, tea.KeyPressMsg{Code: tea.KeyEscape})
	next, cmd := m.Update(bootstrapMsg{conversations: m.conversations, sections: m.sections})
	m = next.(Model)
	if m.active != "" || cmd != nil {
		t.Fatal("reload undid Escape")
	}
}
func TestSidebarStatePersistsAndScopesByUserAndWorkspace(t *testing.T) {
	m, _ := activityModel(t)
	path := StatePath(filepath.Join(t.TempDir(), "config.toml"), "T1", "U1")
	if path == StatePath(filepath.Join(filepath.Dir(path), "config.toml"), "T2", "U1") || path == StatePath(filepath.Join(filepath.Dir(path), "config.toml"), "T1", "U2") {
		t.Fatal("state scope collision")
	}
	m.statePath = path
	m.recordInteraction("READ")
	m.interactions["EXPIRED"] = m.now().Add(-15 * 24 * time.Hour)
	m.collapsed["engineering"] = true
	m.recentOnly, m.soundEnabled = false, false
	if err := m.SaveState(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("state should be owner-only")
	}
	restored, _ := activityModel(t)
	restored.statePath = path
	restored.loadState()
	if !restored.collapsed["engineering"] || restored.recentOnly || restored.soundEnabled || restored.interactions["READ"].IsZero() || !restored.interactions["EXPIRED"].IsZero() {
		t.Fatal("state restore lost settings or retained expired interaction")
	}
}

func TestAutoOpenDoesNotUnfoldRestoredSections(t *testing.T) {
	m := ready(t)
	m.collapsed["engineering"] = true
	m.active = ""
	m.openSelected()
	if !m.collapsed["engineering"] {
		t.Fatal("automatic opening undid saved folding")
	}
	row, ok := m.sidebarSelection()
	if !ok || row.index >= 0 || row.group != "engineering" {
		t.Fatal("active conversation in folded group must select its heading")
	}
}
func TestSearchFailureAfterSuccessFallsBackToAll(t *testing.T) {
	m, _ := activityModel(t)
	m.activityReady, m.sentReady = true, true
	next, _ := m.Update(recentMsg{err: errors.New("search unavailable")})
	*m = next.(Model)
	if !m.visibleConversation("OLD") {
		t.Fatal("failed refresh kept hiding conversations")
	}
}
