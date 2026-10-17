package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

const (
	aiAssistant        = "ai-assistant"
	aiSummaries        = "ai-summaries"
	threadsDestination = "threads"
)
const pinnedRows = 3
const threadInterval = 30 * time.Second

type threadsTick struct{ generation int }
type threadsMsg struct {
	version int
	before  string
	page    slack.ThreadPage
	err     error
}

func (m Model) destinationTitle() string {
	switch m.destination {
	case aiAssistant:
		return "AI ASSISTANT"
	case aiSummaries:
		return "AI SUMMARIES"
	case threadsDestination:
		return "THREADS"
	}
	return ""
}
func (m *Model) openDestination(id string) tea.Cmd {
	m.leaveConversation()
	m.destination = id
	m.sidebarKey = "destination:" + id
	m.focus = messages
	if id == threadsDestination {
		m.status = "Threads · Enter opens replies"
		m.renderThreads()
		if !m.feedLoading {
			return m.requestThreads("")
		}
		return nil
	}
	m.status = "AI workspace views disabled · conversation summaries available with s"
	m.renderDestination()
	return nil
}
func (m *Model) renderDestination() {
	if m.destination == threadsDestination {
		m.renderThreads()
		return
	}
	if m.destination == "" {
		return
	}
	title := "Ask about your Slack conversations"
	detail := "Ask questions and send prompts about conversations from the last 14 days."
	if m.destination == aiSummaries {
		title = "Your daily catch-up"
		detail = "What you missed since your last visit\n\nTop to-dos\nUpdates and decisions\nMessages waiting for your reply\n\nDaily reminders and summaries will appear here when daily digest scheduling is implemented."
		if !m.previousVisit.IsZero() {
			detail += "\n\nLast visit to this client: " + m.previousVisit.Local().Format("Jan 02, 2006 15:04")
		}
	}
	width := m.viewport.Width()
	text := "AI features are disabled in this workspace view.\nAssistant retrieval and daily digest scheduling are not implemented yet.\n\nOpen a conversation and press s for a private 14-day summary. Configure its provider in Settings (,).\n\n" + detail
	card := lipgloss.NewStyle().Width(width).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#8792A2")).Padding(1).Render(ansi.Hardwrap(title+"\n\n"+text, max(1, width-4), true))
	m.viewport.SetContent(card)
}
func (m Model) fetchThreads(before string) tea.Cmd {
	api, ok := m.api.(slack.ThreadsAPI)
	if !ok {
		return func() tea.Msg {
			return threadsMsg{err: fmt.Errorf("followed threads are unavailable for this backend")}
		}
	}
	ctx, version := m.ctx, m.eventVersion
	return func() tea.Msg {
		page, err := api.Threads(ctx, before)
		return threadsMsg{before: before, page: page, err: err, version: version}
	}
}
func (m *Model) threadTimer() tea.Cmd {
	m.feedPollGeneration++
	generation := m.feedPollGeneration
	return tea.Tick(max(threadInterval, time.Until(m.retryUntil)), func(time.Time) tea.Msg { return threadsTick{generation} })
}
func (m *Model) requestThreads(before string) tea.Cmd {
	if m.feedLoading || !m.canRequest() {
		return nil
	}
	m.feedLoading = true
	return m.fetchThreads(before)
}
func (m *Model) updateThreads(msg threadsMsg) tea.Cmd {
	m.feedLoading = false
	if msg.err != nil {
		m.setError(msg.err)
		m.feedError = "Threads unavailable: " + m.status + " · R retries"
		if m.destination == threadsDestination {
			m.renderThreads()
		}
		if msg.before == "" {
			return m.threadTimer()
		}
		return nil
	}
	previous := map[string]slack.Thread{}
	for _, thread := range m.feed {
		previous[thread.Key()] = thread
	}
	changedCurrent := false
	for _, thread := range msg.page.Threads {
		thread.Root = m.withReactionSnapshot(thread.Root, thread.Root.Channel, msg.version)
		for i := range thread.LatestReplies {
			thread.LatestReplies[i] = m.withReactionSnapshot(thread.LatestReplies[i], thread.Root.Channel, msg.version)
		}
		for i := range thread.UnreadReplies {
			thread.UnreadReplies[i] = m.withReactionSnapshot(thread.UnreadReplies[i], thread.Root.Channel, msg.version)
		}
		if m.threadVersions[thread.Key()] > msg.version {
			continue
		}
		old, existed := previous[thread.Key()]
		m.remoteRead[thread.Key()] = laterTS(m.remoteRead[thread.Key()], thread.Root.LastRead)
		m.clearReadHighlights(thread.Key(), m.remoteRead[thread.Key()])
		if existed && slack.TimestampTime(thread.Latest()).After(slack.TimestampTime(old.Latest())) && m.thread != nil && thread.Key() == m.active+"/"+m.thread.ts {
			changedCurrent = true
		}

		previous[thread.Key()] = thread
	}
	m.feed = nil
	for _, thread := range previous {
		m.feed = append(m.feed, thread)
	}
	sort.Slice(m.feed, func(i, j int) bool {
		return slack.TimestampTime(m.feed[i].Latest()).After(slack.TimestampTime(m.feed[j].Latest()))
	})
	m.feedReady, m.feedError = true, ""
	if msg.before != "" {
		m.feedPaged = true
		m.feedNext = msg.page.Next
	} else if !m.feedPaged {
		m.feedNext = msg.page.Next
	}
	if msg.before == "" && msg.version >= m.lastThreadMutation {
		m.feedUnread = msg.page.TotalUnread
		for _, thread := range msg.page.Threads {
			for _, reply := range thread.UnreadReplies {
				if !slack.TimestampTime(reply.TS).After(slack.TimestampTime(m.readPositions[thread.Key()])) {
					m.feedUnread = max(0, m.feedUnread-1)
				}
			}
		}
	}
	if m.feedSelection == "" && len(m.feed) > 0 {
		m.feedSelection = m.feed[0].Key()
	}
	if m.destination == threadsDestination {
		m.renderThreads()
	}
	var timer tea.Cmd
	if msg.before == "" {
		timer = m.threadTimer()
	}
	var refresh tea.Cmd
	if changedCurrent && !m.loading {
		refresh = m.fetchHistory("")
	}
	return tea.Batch(timer, refresh)
}
func (m Model) threadUnreadCount(thread slack.Thread) int {
	read := laterTS(thread.Root.LastRead, m.readPositions[thread.Key()])
	count := 0
	for _, reply := range thread.UnreadReplies {
		if reply.User != m.session.UserID && slack.TimestampTime(reply.TS).After(slack.TimestampTime(read)) {
			count++
		}
	}
	return count
}
func (m *Model) renderThreads() {
	blocks := []string{}
	m.blockStarts = map[string]int{}
	line := 0
	for _, thread := range m.feed {
		root := thread.Root
		name := root.Channel
		for _, c := range m.conversations {
			if c.ID == root.Channel {
				name = m.label(c)
				break
			}
		}
		prefix := cleanLine(name)
		if count := m.threadUnreadCount(thread); count > 0 {
			word := "replies"
			if count == 1 {
				word = "reply"
			}
			prefix += fmt.Sprintf(" · %d unread %s", count, word)
		}
		copy := *m
		copy.destination = ""
		copy.newMessages = map[string]bool{root.TS: m.threadUnreadCount(thread) > 0}
		copy.selectedMessage = ""
		if thread.Key() == m.feedSelection {
			copy.selectedMessage = root.TS
		}
		root.Text = prefix + "\n\n" + root.Text
		var latest slack.Message
		for _, reply := range append(append([]slack.Message{}, thread.LatestReplies...), thread.UnreadReplies...) {
			if slack.TimestampTime(reply.TS).After(slack.TimestampTime(latest.TS)) {
				latest = reply
			}
		}
		if latest.TS != "" {
			root.Text += "\n\nLatest reply: " + latest.Text
		}
		block := copy.messageBlock(root, m.viewport.Width())
		m.blockStarts[thread.Key()] = line
		line += lipgloss.Height(block) + 1
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		empty := "No followed threads yet. Threads you participate in or follow in Slack will appear here."
		if !m.feedReady {
			empty = "Loading followed threads…"
		}
		if m.feedError != "" {
			empty = m.feedError
		}
		m.viewport.SetContent(ansi.Hardwrap(empty, m.viewport.Width(), true))
		return
	}
	content := strings.Join(blocks, "\n\n")
	if m.feedError != "" {
		content += "\n\n" + m.feedError
	}
	if m.feedNext != "" {
		content += "\n\nL: load older threads"
	}
	m.viewport.SetContent(content)
}
func (m *Model) selectFeed(index int) {
	if len(m.feed) == 0 {
		return
	}
	index = max(0, min(index, len(m.feed)-1))
	m.feedSelection = m.feed[index].Key()
	m.renderThreads()
	start := m.blockStarts[m.feedSelection]
	if start < m.viewport.YOffset() || start >= m.viewport.YOffset()+m.viewport.Height()-2 {
		m.viewport.SetYOffset(start)
	}
}
func (m *Model) moveFeed(delta int) {
	for i, thread := range m.feed {
		if thread.Key() == m.feedSelection {
			m.selectFeed(i + delta)
			return
		}
	}
	m.selectFeed(0)
}
func (m *Model) openFeedThread() tea.Cmd {
	if !m.canRequest() {
		return nil
	}
	for _, thread := range m.feed {
		if thread.Key() != m.feedSelection {
			continue
		}
		m.destination = ""
		m.active = thread.Root.Channel
		m.thread = &threadState{ts: thread.Root.TS, fromFeed: true}
		m.history = []slack.Message{thread.Root}
		m.remoteRead[thread.Key()] = laterTS(m.remoteRead[thread.Key()], thread.Root.LastRead)
		m.newMessages = map[string]bool{}
		m.selectedMessage = thread.Root.TS
		m.loading = false
		m.readEnabled = true
		m.generation++
		m.input.SetValue(m.drafts[m.draftKey()])
		m.input.CursorEnd()
		m.focus = messages
		m.resize()
		m.viewport.SetYOffset(0)
		return m.fetchHistory("")
	}
	return nil
}
