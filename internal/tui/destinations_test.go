package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func TestPinnedAIDestinationsRemainDisabledAndPreserveDrafts(t *testing.T) {
	m := ready(t)
	original := m.active
	m.input.SetValue("private draft")
	for _, id := range []string{aiAssistant, aiSummaries} {
		if cmd := m.openDestination(id); cmd != nil {
			t.Fatal("disabled AI started a command")
		}
		if m.active != "" || !strings.Contains(m.viewport.GetContent(), "AI features are disabled") {
			t.Fatal("AI disabled state missing")
		}
		applyKey(&m, key('i'))
		if m.mode != normal || m.send() != nil {
			t.Fatal("AI destination allowed composing/posting to Slack")
		}
		applyKey(&m, key('r'))
		if m.destination != id {
			t.Fatal("refresh left disabled AI view")
		}
	}
	if m.drafts[original] != "private draft" {
		t.Fatal("AI navigation lost Slack draft")
	}
	rows := m.sidebarRows()
	if rows[0].destination != aiAssistant || rows[1].destination != aiSummaries || rows[2].destination != threadsDestination {
		t.Fatal("destinations are not pinned first")
	}
	next, _ := m.Update(historyMsg{channel: original, generation: m.generation - 1, history: slack.History{Messages: []slack.Message{{TS: "100.1", Text: "stale"}}}})
	m = next.(Model)
	if !strings.Contains(m.viewport.GetContent(), "AI features are disabled") {
		t.Fatal("stale Slack history replaced AI view")
	}
}
func TestUnreadGroupingCountsAndReadReturnToOriginalSection(t *testing.T) {
	m := ready(t)
	m.soundEnabled = false
	m.activityReady, m.sentReady = true, true
	m.interactions = map[string]time.Time{}
	count := 2
	applyActivity(&m, slack.Activity{ID: "CGENERAL", LastRead: "1792188000.000001", Latest: "1792188120.000001", HasUnreads: true, UnreadCount: &count})
	if !m.visibleConversation("CGENERAL") || m.unreadBadge("CGENERAL") != "2" {
		t.Fatal("unread did not bypass recent filter or display count")
	}
	groups := m.sidebarGroups()
	if groups[0].id != "local-unread" || len(groups[0].indices) != 1 {
		t.Fatal("unread group not first")
	}
	occurrences := 0
	for _, group := range groups {
		for _, i := range group.indices {
			if m.conversations[i].ID == "CGENERAL" {
				occurrences++
			}
		}
	}
	if occurrences != 1 {
		t.Fatal("unread conversation duplicated in native section")
	}
	for i, c := range m.conversations {
		if c.ID == "CGENERAL" {
			m.selected = i
		}
	}
	cmd := m.openSelected()
	if cmd == nil {
		t.Fatal("opening unread did not load history")
	}
	next, markCmd := m.Update(cmd())
	m = next.(Model)
	if m.unreadBadge("CGENERAL") != "" || !m.visibleConversation("CGENERAL") || markCmd == nil {
		t.Fatal("open did not clear local unread and schedule mark")
	}
	for _, group := range m.sidebarGroups() {
		if group.id == "local-unread" {
			t.Fatal("opened conversation remained in Unread")
		}
	}
	if !strings.Contains(ansi.Strip(m.viewport.GetContent()), "NEW") {
		t.Fatal("new message styling missing")
	}
	// Execute the demo mark explicitly; no real Slack writes.
	request := m.pendingMarks["CGENERAL"]
	next, mark := m.Update(readTick{})
	m = next.(Model)
	if mark == nil || request.ts != "1792188120.000001" {
		t.Fatal("read marker not coalesced through newest loaded message")
	}
	next, _ = m.Update(mark())
	m = next.(Model)
	if len(m.newMessages) != 0 || strings.Contains(ansi.Strip(m.viewport.GetContent()), "NEW") {
		t.Fatal("acknowledged messages remained highlighted")
	}
	applyActivity(&m, slack.Activity{ID: "CGENERAL", LastRead: "1792188000.000001", Latest: request.ts, HasUnreads: true})
	if m.unreadBadge("CGENERAL") != "" {
		t.Fatal("stale counts restored cleared unread")
	}
	applyActivity(&m, slack.Activity{ID: "CGENERAL", LastRead: request.ts, Latest: "1792188121.000001", HasUnreads: true})
	if m.unreadBadge("CGENERAL") == "" {
		t.Fatal("new message after local read was lost")
	}
}
func TestFailedHistoryDoesNotClearUnread(t *testing.T) {
	m := ready(t)
	m.unread["CGENERAL"] = unreadState{after: "100.1", latest: "200.1", count: 3, exact: true}
	m.active = "CGENERAL"
	m.readEnabled = true
	m.loading = true
	next, _ := m.Update(historyMsg{channel: m.active, generation: m.generation, err: errors.New("offline")})
	m = next.(Model)
	if m.unreadBadge("CGENERAL") != "3" || len(m.pendingMarks) != 0 {
		t.Fatal("failed load marked conversation read")
	}
}
func TestStaleUnreadCountCannotRestoreReadOrReplaceNewerCount(t *testing.T) {
	m := ready(t)
	m.unread["CGENERAL"] = unreadState{after: "100.1", latest: "300.1", count: 4, exact: true}
	next, _ := m.Update(unreadCountMsg{channel: "CGENERAL", after: "100.1", latest: "200.1", count: slack.UnreadCount{Count: 99, Exact: true}})
	m = next.(Model)
	if m.unreadBadge("CGENERAL") != "4" {
		t.Fatal("stale count replaced newer snapshot")
	}
	delete(m.unread, "CGENERAL")
	next, _ = m.Update(unreadCountMsg{channel: "CGENERAL", after: "100.1", latest: "300.1", count: slack.UnreadCount{Count: 99, Exact: true}})
	m = next.(Model)
	if m.unreadBadge("CGENERAL") != "" {
		t.Fatal("late count restored already-read conversation")
	}
}
func TestReadAckRetainsNewerPendingPositionAndRespectsRateLimit(t *testing.T) {
	m := ready(t)
	m.pendingMarks["CGENERAL"] = readRequest{"CGENERAL", "", "300.1"}
	m.newMessages["300.1"] = true
	next, _ := m.Update(markedMsg{request: readRequest{"CGENERAL", "", "200.1"}})
	m = next.(Model)
	if m.pendingMarks["CGENERAL"].ts != "300.1" || !m.newMessages["300.1"] {
		t.Fatal("older read ack discarded newer read")
	}
	next, _ = m.Update(markedMsg{request: m.pendingMarks["CGENERAL"], err: &slack.RateLimitError{RetryAfter: time.Minute}})
	m = next.(Model)
	if m.pendingMarks["CGENERAL"].ts != "300.1" || time.Until(m.retryUntil) < 59*time.Second {
		t.Fatal("rate-limited mark lost pending read or cooldown")
	}
}
func TestThreadsFeedOpensRepliesAndReturnsToFeed(t *testing.T) {
	m := ready(t)
	cmd := m.openDestination(threadsDestination)
	next, _ := m.Update(cmd())
	m = next.(Model)
	if len(m.feed) != 1 || m.feedUnread != 1 || !strings.Contains(m.viewport.GetContent(), "Latest reply") {
		t.Fatal("followed-thread feed missing replies/unread count")
	}
	channelDraft := m.drafts["CGENERAL"]
	cmd = applyKey(&m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.thread == nil || !m.thread.fromFeed || m.active != "CGENERAL" {
		t.Fatal("Enter did not open feed thread")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.history) != 2 || m.feedUnread != 0 {
		t.Fatal("thread replies did not load/read")
	}
	m.input.SetValue("thread draft")
	applyKey(&m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.destination != threadsDestination || m.thread != nil || m.drafts["CGENERAL/1792188120.000001"] != "thread draft" || m.drafts["CGENERAL"] != channelDraft {
		t.Fatal("thread return lost feed/draft isolation")
	}
	cmd = applyKey(&m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.input.Value() != "thread draft" {
		t.Fatal("reopening feed thread lost draft")
	}
}
func TestThreadPaginationDoesNotMakeOlderRepliesChime(t *testing.T) {
	m, calls := activityModel(t)
	old := slack.Thread{Root: slack.Message{Channel: "C1", TS: "100.1", LastRead: "100.1", LatestReply: "200.1"}, UnreadReplies: []slack.Message{{TS: "200.1", User: "OTHER"}}}
	next, _ := m.Update(threadsMsg{page: slack.ThreadPage{Threads: []slack.Thread{old}, TotalUnread: 1, Next: "200.1"}})
	*m = next.(Model)
	if *calls != 0 {
		t.Fatal("old thread backlog chimed")
	}
	older := old
	older.Root.Channel = "C2"
	next, _ = m.Update(threadsMsg{before: "200.1", page: slack.ThreadPage{Threads: []slack.Thread{older}}})
	*m = next.(Model)
	if len(m.feed) != 2 || *calls != 0 {
		t.Fatal("pagination lost composite identity or chimed old unread")
	}
	newest := old
	newest.Root.LatestReply = "201.1"
	newest.UnreadReplies = []slack.Message{{TS: "201.1", User: "OTHER"}}
	next, _ = m.Update(threadsMsg{page: slack.ThreadPage{Threads: []slack.Thread{newest}, TotalUnread: 2}})
	*m = next.(Model)
	if *calls != 0 {
		t.Fatal("thread polling must never chime")
	}
}
func TestPinnedViewsAndShortSidebarFitTerminal(t *testing.T) {
	m := ready(t)
	m.conversations = append(m.conversations, slack.Conversation{ID: "Z", Name: strings.Repeat("z", 50)})
	m.selectSidebar(len(m.sidebarRows()) - 1)
	for _, size := range [][2]int{{60, 12}, {80, 24}, {120, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		for _, dest := range []string{aiAssistant, aiSummaries, threadsDestination} {
			m.openDestination(dest)
			view := m.View().Content
			if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
				t.Fatal("destination layout exceeded terminal")
			}
		}
		if size[1] >= 24 && (!strings.Contains(ansi.Strip(m.View().Content), "AI assistant") || !strings.Contains(ansi.Strip(m.View().Content), "AI summaries")) {
			t.Fatal("pinned AI views scrolled away")
		}
	}
}
func TestInitialBackgroundLoadCannotReplaceSelectedAIView(t *testing.T) {
	api := slack.NewDemo()
	session, _ := api.AuthTest(context.Background())
	m := New(context.Background(), api, session, true)
	m.openDestination(aiAssistant)
	next, _ := m.Update(m.bootstrap()())
	m = next.(Model)
	if m.destination != aiAssistant || m.active != "" {
		t.Fatal("bootstrap replaced explicit AI selection")
	}
}

func TestThreadRefreshKeepsOlderPaginationCursorAndOneTimer(t *testing.T) {
	m := ready(t)
	thread := slack.Thread{Root: slack.Message{Channel: "C1", TS: "100.1", LatestReply: "200.1"}}
	next, _ := m.Update(threadsMsg{page: slack.ThreadPage{Threads: []slack.Thread{thread}, Next: "200.1"}})
	m = next.(Model)
	oldGeneration := m.feedPollGeneration
	next, _ = m.Update(threadsMsg{before: "200.1", page: slack.ThreadPage{Next: "150.1"}})
	m = next.(Model)
	next, _ = m.Update(threadsMsg{page: slack.ThreadPage{Threads: []slack.Thread{thread}, Next: "200.1"}})
	m = next.(Model)
	if m.feedNext != "150.1" {
		t.Fatal("newest poll reset older thread pagination")
	}
	next, cmd := m.Update(threadsTick{generation: oldGeneration})
	m = next.(Model)
	if cmd != nil || m.feedLoading {
		t.Fatal("outdated timer started a second polling chain")
	}
}
func TestRemoteReadClearsNewHighlightWithoutReopening(t *testing.T) {
	m := ready(t)
	m.active = "CGENERAL"
	m.newMessages["200.1"] = true
	m.history = []slack.Message{{TS: "200.1", User: "OTHER", Text: "new"}}
	m.renderHistory()
	applyActivity(&m, slack.Activity{ID: m.active, LastRead: "200.1", Latest: "200.1"})
	if m.newMessages["200.1"] || strings.Contains(ansi.Strip(m.viewport.GetContent()), "NEW") {
		t.Fatal("remote read left stale message highlighting")
	}
}

func TestFeedThreadCanOpenItsChannelFromSidebar(t *testing.T) {
	m := ready(t)
	cmd := m.openDestination(threadsDestination)
	next, _ := m.Update(cmd())
	m = next.(Model)
	cmd = m.openFeedThread()
	next, _ = m.Update(cmd())
	m = next.(Model)
	for i, c := range m.conversations {
		if c.ID == m.active {
			m.selected = i
		}
	}
	cmd = m.openSelected()
	if cmd == nil || m.destination != "" || m.thread != nil || m.active != "CGENERAL" {
		t.Fatal("selecting the thread's channel returned to the feed instead")
	}
}
func TestThreadUnreadStyleIncludesOwnRootAndNewestPreview(t *testing.T) {
	m := ready(t)
	m.destination = threadsDestination
	m.active = ""
	root := slack.Message{User: m.session.UserID, Channel: "C1", TS: "100.1", LatestReply: "300.1", LastRead: "100.1", Text: "my root"}
	m.feed = []slack.Thread{{Root: root, UnreadReplies: []slack.Message{{User: "OTHER", TS: "300.1", Text: "newest preview"}}, LatestReplies: []slack.Message{{TS: "300.1", Text: "newest preview"}, {TS: "200.1", Text: "older preview"}}}}
	m.renderThreads()
	text := ansi.Strip(m.viewport.GetContent())
	if !strings.Contains(text, "NEW") || !strings.Contains(text, "Latest reply: newest preview") || strings.Contains(text, "Latest reply: older preview") {
		t.Fatal("own-root unread thread style or latest-reply selection failed")
	}
}
