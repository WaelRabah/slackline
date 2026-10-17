package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func key(value rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: value, Text: string(value)} }
func ready(t *testing.T) Model {
	t.Helper()
	api := slack.NewDemo()
	session, _ := api.AuthTest(context.Background())
	m := New(context.Background(), api, session, true)
	loaded, cmd := m.Update(m.bootstrap()())
	m = loaded.(Model)
	if cmd != nil {
		loaded, _ = m.Update(cmd())
		m = loaded.(Model)
	}
	return m
}

func TestStaleHistoryCannotReplaceActiveConversation(t *testing.T) {
	m := ready(t)
	before := len(m.history)
	next, _ := m.Update(historyMsg{channel: "OTHER", generation: m.generation, history: slack.History{Messages: []slack.Message{{TS: "1", Text: "wrong channel"}}}})
	m = next.(Model)
	if len(m.history) != before {
		t.Fatal("stale response changed history")
	}
	next, _ = m.Update(historyMsg{channel: m.active, generation: m.generation - 1, history: slack.History{Messages: []slack.Message{{TS: "1", Text: "old generation"}}}})
	if len(next.(Model).history) != before {
		t.Fatal("old generation changed history")
	}
}

func TestDraftSurvivesConversationSwitchAndSendFailure(t *testing.T) {
	m := ready(t)
	original := m.active
	m.input.SetValue("unsent draft")
	m.selected = (m.selected + 1) % len(m.conversations)
	cmd := m.openSelected()
	if cmd == nil {
		t.Fatal("expected history command")
	}
	if m.drafts[original] != "unsent draft" {
		t.Fatal("lost channel draft")
	}
	for i, c := range m.conversations {
		if c.ID == original {
			m.selected = i
		}
	}
	m.openSelected()
	if m.input.Value() != "unsent draft" {
		t.Fatal("draft not restored")
	}
	m.sending = true
	next, _ := m.Update(sentMsg{channel: original, text: "unsent draft", err: errors.New("connection lost")})
	if next.(Model).input.Value() != "unsent draft" {
		t.Fatal("lost draft on send failure")
	}
}

func TestTypingAndNavigationNeverSend(t *testing.T) {
	m := ready(t)
	next, cmd := m.Update(key('i'))
	m = next.(Model)
	if m.mode != insert {
		t.Fatal("i did not enter insert mode")
	}
	if cmd != nil { /* cursor blink command is expected */
	}
	for _, r := range "hello" {
		next, _ = m.Update(key(r))
		m = next.(Model)
	}
	if m.sending || m.input.Value() != "hello" {
		t.Fatal("typing sent or corrupted text")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.sending {
		t.Fatal("normal-mode enter sent a message")
	}
	m.selected = 0
	m.openSelected()
	m.input.SetValue("hello")
	if m.send() == nil {
		t.Fatal("explicit send failed")
	}
	if m.send() != nil {
		t.Fatal("duplicate send allowed while pending")
	}
}

func TestSendResponseDoesNotClearAnotherConversationDraft(t *testing.T) {
	m := ready(t)
	m.input.SetValue("current draft")
	m.drafts["OTHER"] = "sent text"
	next, _ := m.Update(sentMsg{channel: "OTHER", text: "sent text", message: slack.Message{TS: "200.000001"}})
	m = next.(Model)
	if m.input.Value() != "current draft" || m.drafts["OTHER"] != "" {
		t.Fatal("send response cleared wrong draft")
	}
}

func TestFinderAndVimNavigation(t *testing.T) {
	m := ready(t)
	next, _ := m.Update(key('/'))
	m = next.(Model)
	for _, r := range "gnrl" {
		next, _ = m.Update(key(r))
		m = next.(Model)
	}
	if len(m.matches) != 1 || m.conversations[m.matches[0]].Name != "general" {
		t.Fatal("fuzzy matching failed")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.mode != normal || m.active != "CGENERAL" {
		t.Fatal("finder did not open channel")
	}
	m.focus = sidebar
	next, _ = m.Update(key('G'))
	m = next.(Model)
	if m.selected != len(m.conversations)-1 {
		t.Fatal("G did not go to end")
	}
	next, _ = m.Update(key('g'))
	m = next.(Model)
	next, _ = m.Update(key('g'))
	m = next.(Model)
	if row, ok := m.sidebarSelection(); !ok || row.index >= 0 {
		t.Fatal("gg did not select first section heading")
	}
}

func TestTerminalControlsAreRemoved(t *testing.T) {
	value := "hello\x1b[31m red\x1b[0m\x1b]52;c;c2VjcmV0\x07\x1b[2J\rworld"
	got := clean(value)
	if strings.ContainsAny(got, "\x1b\x07\r") || strings.Contains(got, "52;") {
		t.Fatal("terminal controls survived")
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatal("removed ordinary content")
	}
}

func TestMergeOrdersWithoutTimestampPrecisionLoss(t *testing.T) {
	result := mergeMessages([]slack.Message{{TS: "1792188000.000002", Text: "old"}, {TS: "1792188000.000001"}}, []slack.Message{{TS: "1792188000.000002", Text: "new"}, {TS: "1792188000.000003"}})
	if len(result) != 3 || result[0].TS != "1792188000.000001" || result[1].Text != "new" {
		t.Fatal("merge lost ordering or deduplication")
	}
}

func TestSmallTerminalDoesNotPanic(t *testing.T) {
	m := ready(t)
	for _, size := range [][2]int{{0, 0}, {1, 1}, {59, 11}, {60, 12}, {120, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		_ = m.View()
	}
}

func TestReloadAfterLeavingActiveConversation(t *testing.T) {
	m := ready(t)
	previous := m.active
	m.input.SetValue("draft in departed channel")
	next, cmd := m.Update(bootstrapMsg{conversations: []slack.Conversation{{ID: "NEW", Name: "new-channel"}}})
	m = next.(Model)
	if m.active != "NEW" || cmd == nil {
		t.Fatal("reload kept a departed channel active")
	}
	if m.drafts[previous] != "draft in departed channel" {
		t.Fatal("reload lost the previous draft")
	}
	next, _ = m.Update(bootstrapMsg{conversations: []slack.Conversation{}})
	m = next.(Model)
	if m.active != "" || len(m.history) != 0 {
		t.Fatal("empty workspace kept stale conversation")
	}
}

func TestWorkspaceLabelsCannotInjectLayout(t *testing.T) {
	m := ready(t)
	label := m.label(slack.Conversation{Name: "channel\nextra\tline"})
	if strings.ContainsAny(label, "\n\t") {
		t.Fatal("workspace name injected layout")
	}
}

func TestMessageBlockWrapsWithinPane(t *testing.T) {
	m := ready(t)
	m.users["U1"] = "A very long display name with 日本語"
	text := "A multiline message\n" + strings.Repeat("界", 35) + "\n" + strings.Repeat("x", 90) + "\n\tTabbed text"
	for _, width := range []int{10, 28, 60, 90} {
		block := m.messageBlock(slack.Message{TS: "1792188000.000001", User: "U1", Text: text, ReplyCount: 3}, width)
		for _, line := range strings.Split(block, "\n") {
			if lipgloss.Width(line) != width {
				t.Fatalf("message frame escaped width %d: line has %d columns", width, lipgloss.Width(line))
			}
		}
		plain := ansi.Strip(block)
		if strings.Count(plain, "界") != 35 || strings.Count(plain, "x") != 91 {
			t.Fatal("wrapping lost message characters")
		}
	}
}

func TestPaneLayoutKeepsFocusVisibleAndFitsTerminal(t *testing.T) {
	m := ready(t)
	for _, size := range [][2]int{{60, 12}, {80, 24}, {120, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		for _, focus := range []pane{sidebar, messages, composer} {
			m.focus = focus
			view := m.View().Content
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "▶ ") || !strings.Contains(plain, "CHANNELS") || !strings.Contains(plain, "COMPOSER") {
				t.Fatal("pane headings or focus marker missing")
			}
			if lipgloss.Height(view) > size[1] || lipgloss.Width(view) > size[0] {
				t.Fatal("pane layout exceeded terminal dimensions")
			}
		}
	}
}

func TestBlockSelectionThreadReplyAndDraftIsolation(t *testing.T) {
	m := ready(t)
	for i, c := range m.conversations {
		if c.ID == "CGENERAL" {
			m.selected = i
		}
	}
	cmd := m.openSelected()
	next, _ := m.Update(cmd())
	m = next.(Model)
	m.input.SetValue("channel draft")
	parent := m.selectedMessage
	next, _ = m.Update(key('k'))
	m = next.(Model)
	if m.selectedMessage == parent {
		t.Fatal("k did not select previous block")
	}
	next, _ = m.Update(key('j'))
	m = next.(Model)
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.thread == nil || cmd == nil {
		t.Fatal("Enter did not open selected thread")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.history) != 2 || m.input.Value() != "" {
		t.Fatal("thread failed to load or inherited channel draft")
	}
	m.input.SetValue("reply draft")
	cmd = m.send()
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.history) != 3 || m.history[2].ThreadTS != parent {
		t.Fatal("reply did not stay in thread")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = next.(Model)
	if m.thread != nil || len(m.history) != 3 || m.input.Value() != "channel draft" {
		t.Fatal("thread return lost channel history or draft")
	}
}
func TestEscClearsConversationAndCommandPOpensFinder(t *testing.T) {
	m := ready(t)
	channel := m.active
	m.input.SetValue("keep me")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.active != "" || len(m.history) != 0 || m.viewport.GetContent() != "" || m.drafts[channel] != "keep me" {
		t.Fatal("Esc did not leave conversation cleanly")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModSuper})
	m = next.(Model)
	if m.mode != finder {
		t.Fatal("Cmd+P did not open finder")
	}
}
func TestSidebarSectionsKeepEachConversationVisibleOnce(t *testing.T) {
	m := ready(t)
	m.conversations = append(m.conversations, slack.Conversation{ID: "G1", Name: "group", IsMPIM: true}, slack.Conversation{ID: "P1", Name: "private", IsPrivate: true})
	seen := map[int]bool{}
	names := map[string]bool{}
	for _, g := range m.sidebarGroups() {
		names[g.title] = true
		for _, i := range g.indices {
			if seen[i] {
				t.Fatal("conversation duplicated between sections")
			}
			seen[i] = true
		}
	}
	if len(seen) != len(m.conversations) || !names["Engineering"] || !names["Direct messages"] || !names["Group DMs"] || !names["Private channels"] {
		t.Fatal("missing sidebar categories or conversations")
	}
	m.selected = 0
	m.sidebarKey = ""
	m.toggleSection()
	if !m.selectedSectionCollapsed() {
		t.Fatal("section did not fold")
	}
}
