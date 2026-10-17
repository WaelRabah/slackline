package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

type unreadState struct {
	after, latest string
	count         int
	exact         bool
	counted       string
	retryAt       time.Time
}
type unreadCountMsg struct {
	channel, after, latest string
	count                  slack.UnreadCount
	err                    error
}
type countTick struct{}
type readTick struct{}
type readRequest struct{ channel, thread, ts string }
type markedMsg struct {
	request readRequest
	err     error
}

func laterTS(a, b string) string {
	if slack.TimestampTime(a).After(slack.TimestampTime(b)) {
		return a
	}
	return b
}
func (m Model) unreadBadge(id string) string {
	u, ok := m.unread[id]
	if !ok {
		return ""
	}
	count := max(1, u.count)
	if !u.exact {
		return fmt.Sprintf("%d+", count)
	}
	return fmt.Sprint(count)
}
func (m *Model) updateUnread(row slack.Activity) {
	m.remoteRead[row.ID] = laterTS(m.remoteRead[row.ID], row.LastRead)
	if !row.HasUnreads {
		m.remoteRead[row.ID] = laterTS(m.remoteRead[row.ID], row.Latest)
	}
	m.clearReadHighlights(row.ID, m.remoteRead[row.ID])
	read := laterTS(m.remoteRead[row.ID], m.readPositions[row.ID])
	if !row.HasUnreads || !slack.TimestampTime(row.Latest).After(slack.TimestampTime(read)) {
		delete(m.unread, row.ID)
		return
	}
	old := m.unread[row.ID]
	if old.after != read || old.latest != row.Latest {
		old = unreadState{after: read, latest: row.Latest, count: 1}
	}
	if row.UnreadCount != nil && read == row.LastRead {
		old.count = *row.UnreadCount
		old.exact = true
		old.counted = row.Latest
	}
	m.unread[row.ID] = old
}
func (m *Model) nextUnreadCount() tea.Cmd {
	api, ok := m.api.(slack.ReadAPI)
	if !ok || m.countBusy {
		return nil
	}
	if time.Now().Before(m.retryUntil) {
		return tea.Tick(time.Until(m.retryUntil), func(time.Time) tea.Msg { return countTick{} })
	}
	for _, c := range m.conversations {
		u, ok := m.unread[c.ID]
		if !ok || u.counted == u.latest || time.Now().Before(u.retryAt) {
			continue
		}
		m.countBusy = true
		ctx, self := m.ctx, m.session.UserID
		channel, after, latest := c.ID, u.after, u.latest
		return func() tea.Msg {
			count, err := api.CountUnread(ctx, channel, after, latest, self)
			return unreadCountMsg{channel, after, latest, count, err}
		}
	}
	return nil
}
func (m *Model) applyUnreadCount(msg unreadCountMsg) tea.Cmd {
	m.countBusy = false
	if u, ok := m.unread[msg.channel]; ok && u.after == msg.after && u.latest == msg.latest {
		u.count = max(u.count, msg.count.Count)
		u.exact = msg.count.Exact && msg.err == nil
		u.counted = msg.latest
		if msg.err != nil {
			m.setError(msg.err)
			u.retryAt = time.Now().Add(time.Minute)
			u.counted = ""
		}
		if msg.count.Exact && msg.count.Count == 0 && msg.err == nil {
			delete(m.unread, msg.channel)
		} else {
			m.unread[msg.channel] = u
		}
	}
	return tea.Tick(max(2*time.Second, time.Until(m.retryUntil)), func(time.Time) tea.Msg { return countTick{} })
}
func (m Model) messageUnread(message slack.Message) bool {
	if m.destination != "" {
		return false
	}
	if m.newMessages[message.TS] {
		return true
	}
	if message.User == m.session.UserID {
		return false
	}
	key := m.active
	if m.thread != nil {
		key = m.active + "/" + m.thread.ts
	}
	read := laterTS(m.remoteRead[key], m.readPositions[key])
	if m.thread != nil && message.TS == m.thread.ts {
		return false
	}
	_, knownUnread := m.unread[m.active]
	return (read != "" || knownUnread) && slack.TimestampTime(message.TS).After(slack.TimestampTime(read))
}
func (m *Model) readLoadedHistory() tea.Cmd {
	if !m.readEnabled || m.active == "" || len(m.history) == 0 {
		return nil
	}
	key := m.active
	if m.thread != nil {
		key = m.active + "/" + m.thread.ts
	}
	read := laterTS(m.remoteRead[key], m.readPositions[key])
	through := read
	_, inUnread := m.unread[m.active]
	known := read != "" || (inUnread && m.thread == nil)
	for _, message := range m.history {
		through = laterTS(through, message.TS)
		if known && message.User != m.session.UserID && slack.TimestampTime(message.TS).After(slack.TimestampTime(read)) && (m.thread == nil || message.TS != m.thread.ts) {
			m.newMessages[message.TS] = true
		}
	}
	if !slack.TimestampTime(through).After(slack.TimestampTime(read)) {
		if slack.TimestampTime(through).After(slack.TimestampTime(m.remoteRead[key])) && through != "" && m.pendingMarks[key].ts == "" {
			m.pendingMarks[key] = readRequest{m.active, m.threadTS(), through}
			return m.scheduleRead()
		}
		return nil
	}
	m.readPositions[key] = through
	m.pendingMarks[key] = readRequest{m.active, m.threadTS(), through}
	if m.thread == nil {
		if u, ok := m.unread[m.active]; ok && !slack.TimestampTime(u.latest).After(slack.TimestampTime(through)) {
			delete(m.unread, m.active)
		}
	} else {
		for _, thread := range m.feed {
			if thread.Key() == key {
				seen := 0
				for _, reply := range thread.UnreadReplies {
					if reply.User != m.session.UserID && slack.TimestampTime(reply.TS).After(slack.TimestampTime(read)) && !slack.TimestampTime(reply.TS).After(slack.TimestampTime(through)) {
						seen++
					}
				}
				m.feedUnread = max(0, m.feedUnread-seen)
				break
			}
		}
	}
	m.recordInteraction(m.active)
	return m.scheduleRead()
}
func (m *Model) scheduleRead() tea.Cmd {
	if m.markScheduled || m.markBusy || len(m.pendingMarks) == 0 {
		return nil
	}
	m.markScheduled = true
	return tea.Tick(max(5*time.Second, time.Until(m.retryUntil)), func(time.Time) tea.Msg { return readTick{} })
}
func (m *Model) flushRead() tea.Cmd {
	m.markScheduled = false
	if m.markBusy {
		return nil
	}
	if time.Now().Before(m.retryUntil) {
		return m.scheduleRead()
	}
	api, ok := m.api.(slack.ReadAPI)
	for key, request := range m.pendingMarks {
		if !ok {
			delete(m.pendingMarks, key)
			continue
		}
		if !slack.TimestampTime(request.ts).After(slack.TimestampTime(m.remoteRead[key])) {
			delete(m.pendingMarks, key)
			continue
		}
		m.markBusy = true
		ctx := m.ctx
		return func() tea.Msg {
			return markedMsg{request, api.MarkRead(ctx, request.channel, request.thread, request.ts)}
		}
	}
	return nil
}
func (m *Model) applyMarked(msg markedMsg) tea.Cmd {
	m.markBusy = false
	key := msg.request.channel
	if msg.request.thread != "" {
		key += "/" + msg.request.thread
	}
	if msg.err != nil {
		m.setError(msg.err)
		m.status = "Read locally; Slack read sync failed: " + m.status
	}
	if time.Now().Before(m.retryUntil) && msg.err != nil {
		return m.scheduleRead()
	}
	if m.pendingMarks[key].ts == msg.request.ts {
		delete(m.pendingMarks, key)
	}
	if msg.err == nil && m.active == msg.request.channel && m.threadTS() == msg.request.thread {
		for ts := range m.newMessages {
			if !slack.TimestampTime(ts).After(slack.TimestampTime(msg.request.ts)) {
				delete(m.newMessages, ts)
			}
		}
		m.renderHistory()
	}
	return m.scheduleRead()
}

func (m *Model) clearReadHighlights(key, through string) {
	current := m.active
	if m.thread != nil {
		current += "/" + m.thread.ts
	}
	if key != current {
		return
	}
	changed := false
	for ts := range m.newMessages {
		if !slack.TimestampTime(ts).After(slack.TimestampTime(through)) {
			delete(m.newMessages, ts)
			changed = true
		}
	}
	if changed {
		m.renderHistory()
	}
}
