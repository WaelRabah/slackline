package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/config"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

func TestSplitThreadEscapeReplyAndReload(t *testing.T) {
	m := ready(t)
	channel := m.active
	m.input.SetValue("channel draft")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.thread == nil || cmd == nil {
		t.Fatal("thread did not open")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	for _, width := range []int{60, 80, 120, 180} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 35})
		m = next.(Model)
		view := m.View().Content
		if !strings.Contains(ansi.Strip(view), "REPLIES") || lipgloss.Width(view) > width {
			t.Fatal("split layout missing or overflow")
		}
	}
	m.focus = parentPane
	next, _ = m.Update(key('k'))
	m = next.(Model)
	if m.thread.selection == "" {
		t.Fatal("parent lost selection")
	}
	next, cmd = m.Update(key('R'))
	m = next.(Model)
	if cmd == nil || m.active != channel || m.booting {
		t.Fatal("R must reload conversation, not bootstrap")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.thread != nil || m.active != channel || m.input.Value() != "channel draft" {
		t.Fatal("Esc lost channel or draft")
	}
	next, cmd = m.Update(key('r'))
	m = next.(Model)
	if m.mode != insert || m.focus != composer || m.thread == nil || cmd == nil {
		t.Fatal("r did not focus thread reply")
	}
	m.input.SetValue("reply draft")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.thread != nil || m.active != channel || m.input.Value() != "channel draft" {
		t.Fatal("insert Esc did not close thread")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.active != "" || m.focus != sidebar {
		t.Fatal("second Esc did not leave conversation")
	}
}
func TestCommandPAndWorkspaceUsers(t *testing.T) {
	m := ready(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper})
	m = next.(Model)
	if m.mode == finder {
		t.Fatal("Cmd+T still mapped")
	}
	m.directory = append(m.directory, slack.User{ID: "UNEW", Name: "unique-person", RealName: "Unique Person"})
	m.users["UNEW"] = "Unique Person"
	next, _ = m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModSuper})
	m = next.(Model)
	m.query.SetValue("unique-person")
	m.updateMatches()
	if m.mode != finder || len(m.matches) != 1 || m.matches[0] >= 0 || !strings.Contains(m.finderLabel(m.matches[0]), "Unique Person") {
		t.Fatal("workspace user not searchable")
	}
}
func TestSettingsMaskedCancelAndPersistentPreferences(t *testing.T) {
	m := ready(t)
	m.provider.APIKey = "secret-provider-key"
	next, _ := m.Update(key(','))
	m = next.(Model)
	if m.mode != settingsMode || strings.Contains(m.View().Content, "secret-provider-key") {
		t.Fatal("settings missing or key exposed")
	}
	m.settings.row = 2
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if strings.Contains(m.View().Content, "secret-provider-key") {
		t.Fatal("editing key is not masked")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.settings != nil || m.mode != normal {
		t.Fatal("settings Esc failed")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[slack]\ntoken='xoxc-fixture'\ncookie='fixture'\n[custom]\nkeep=true\n"), 0600)
	err := config.SavePreferences(path, inference.Settings{BaseURL: "https://provider.example/v1", ChatModel: "model", APIKey: "private"}, "ocean", false, 7)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Slack.Token != "xoxc-fixture" || cfg.UI.Theme != "ocean" || *cfg.Sidebar.RecentDays != 7 {
		t.Fatal("preferences lost auth or did not persist")
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if !strings.Contains(string(data), "keep = true") || info.Mode().Perm() != 0600 {
		t.Fatal("lost unknown keys or private permissions")
	}
}

type summaryFixture struct{ slack.API }

func (summaryFixture) ConversationContext(context.Context, string, time.Time, time.Time) (slack.ContextPage, error) {
	return slack.ContextPage{Messages: []slack.Message{{TS: "1792188000.000001", User: "U1", Text: "Release is blocked. Alice will review tomorrow."}}, Partial: true}, nil
}
func TestPrivateSummaryInferencePersistenceAndPin(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"What's happening: release blocked. What's next: Alice reviews tomorrow."}}]}`)
	}))
	defer server.Close()
	m := ready(t)
	m.demo = false
	m.api = summaryFixture{m.api}
	m.provider = inference.Settings{Enabled: true, BaseURL: server.URL, ChatModel: "test"}
	m.statePath = filepath.Join(t.TempDir(), "state.json")
	oldHistory := len(m.history)
	channel := m.active
	cmd := m.summarizeConversation()
	if cmd == nil {
		t.Fatal("summary not requested")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if calls != 1 || m.summaries[channel].Text == "" || !m.summaries[channel].Partial || len(m.history) != oldHistory || m.sending {
		t.Fatal("summary posted to history or missing provider result")
	}
	m.width = 120
	m.height = 40
	m.resize()
	if !strings.Contains(ansi.Strip(m.View().Content), "PRIVATE SUMMARY") {
		t.Fatal("summary is not pinned")
	}
	m.viewport.GotoTop()
	if !strings.Contains(ansi.Strip(m.View().Content), "PRIVATE SUMMARY") {
		t.Fatal("scroll hid fixed summary")
	}
	restored := New(context.Background(), m.api, m.session, false, Options{StatePath: m.statePath})
	if restored.summaries[channel].Text == "" {
		t.Fatal("private summary not restored")
	}
	m.expandSummary()
	if m.mode != summaryMode || !strings.Contains(m.summaryViewport.GetContent(), "Alice") {
		t.Fatal("summary expansion failed")
	}
}

func TestLiveUpdatesBothHalvesAndIndependentDrafts(t *testing.T) {
	m := ready(t)
	m.soundEnabled = false
	cmd := m.openThread()
	if cmd == nil {
		t.Fatal("missing thread")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	thread := m.thread.ts
	channel := m.active
	parentCount := len(m.thread.history)
	m.input.SetValue("thread draft")
	next, _ = m.Update(slack.LiveEvent{Type: "message", Message: slack.Message{Channel: channel, TS: "1892188000.000001", User: "OTHER", Text: "new channel post"}})
	m = next.(Model)
	if len(m.thread.history) != parentCount+1 || m.input.Value() != "thread draft" {
		t.Fatal("parent live update or draft lost")
	}
	count := len(m.history)
	next, _ = m.Update(slack.LiveEvent{Type: "message", Message: slack.Message{Channel: channel, TS: "1892188000.000002", ThreadTS: thread, User: "OTHER", Text: "new reply"}})
	m = next.(Model)
	if len(m.history) != count+1 || m.history[len(m.history)-1].Text != "new reply" {
		t.Fatal("thread live update missing")
	}
	next, _ = m.Update(slack.LiveEvent{Type: "message", Message: slack.Message{Channel: channel, Subtype: "message_deleted"}, DeletedTS: "1892188000.000001"})
	m = next.(Model)
	if len(m.thread.history) != parentCount {
		t.Fatal("deleted parent message still visible")
	}
}
func TestMessageSearchRejectsStaleResultsAndOpensHit(t *testing.T) {
	m := ready(t)
	next, _ := m.Update(key('/'))
	m = next.(Model)
	m.query.SetValue("? release")
	m.updateMatches()
	generation := m.finderGeneration
	hit := slack.SearchHit{Conversation: slack.Conversation{ID: m.active, Name: "dev"}, Message: slack.Message{Channel: m.active, TS: "1792000000.000001", User: "OTHER", Text: "release planning"}}
	next, _ = m.Update(searchMsg{generation: generation, page: slack.SearchPage{Hits: []slack.SearchHit{hit}}})
	m = next.(Model)
	if len(m.matches) != 1 || !strings.Contains(m.finderLabel(m.matches[0]), "release planning") {
		t.Fatal("message search match missing")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || m.selectedMessage != hit.Message.TS || m.mode != normal {
		t.Fatal("search hit did not open")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.selectedMessage != hit.Message.TS {
		t.Fatal("history load moved selection away from search hit")
	}
	next, _ = m.Update(key('/'))
	m = next.(Model)
	m.query.SetValue("general")
	m.updateMatches()
	next, _ = m.Update(searchMsg{generation: generation, page: slack.SearchPage{Hits: []slack.SearchHit{hit}}})
	m = next.(Model)
	if len(m.searchHits) != 0 {
		t.Fatal("stale search replaced new finder query")
	}
}
func TestSettingsSaveAppliesThemeAndKeepsDraft(t *testing.T) {
	m := ready(t)
	m.input.SetValue("draft survives settings")
	m.openSettings()
	m.settings.values.Theme = "ocean"
	m.settings.values.Days = 7
	m.settings.values.Sound = false
	next, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("settings save not requested")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.theme != "ocean" || m.recentDays != 7 || m.soundEnabled || m.input.Value() != "draft survives settings" || m.mode != normal {
		t.Fatal("settings not applied or draft lost")
	}
}
