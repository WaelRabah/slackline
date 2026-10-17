package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

const typingLifetime = 6 * time.Second
const liveCacheLimit = 8192

type typingTick struct{ generation int }

func (m *Model) rememberLive(key string) bool {
	if m.liveSeen[key] {
		return false
	}
	m.liveSeen[key] = true
	m.liveOrder = append(m.liveOrder, key)
	if len(m.liveOrder) > liveCacheLimit {
		delete(m.liveSeen, m.liveOrder[0])
		delete(m.messageVersions, m.liveOrder[0])
		m.liveOrder = m.liveOrder[1:]
	}
	return true
}
func realMessage(message slack.Message) bool {
	if message.Channel == "" || slack.TimestampTime(message.TS).IsZero() {
		return false
	}
	switch message.Subtype {
	case "", "bot_message", "file_share", "me_message", "thread_broadcast", "reply_broadcast":
		return true
	}
	return false
}
func (m *Model) updateLive(event slack.LiveEvent) tea.Cmd {
	message := event.Message
	switch event.Type {
	case "_connection":
		wasLive := m.liveState == "live"
		m.liveState = event.State
		if event.State != "live" {
			m.typing = map[string]map[string]time.Time{}
		}
		if event.State == "live" && !wasLive {
			m.status = "Live updates connected"
		}
		return nil
	case "user_typing":
		return m.updateTyping(message)
	case "channel_marked", "group_marked", "im_marked", "mpim_marked", "thread_marked":
		key := message.Channel
		if message.ThreadTS != "" {
			key += "/" + message.ThreadTS
		}
		m.remoteRead[key] = laterTS(m.remoteRead[key], laterTS(message.TS, message.LastRead))
		m.clearReadHighlights(key, m.remoteRead[key])
		if u, ok := m.unread[key]; ok && !slack.TimestampTime(u.latest).After(slack.TimestampTime(m.remoteRead[key])) {
			delete(m.unread, key)
		}
		return nil
	case "message":
	default:
		return nil
	}
	key := message.Channel + "/" + message.TS
	if message.Subtype == "message_changed" || message.Subtype == "message_replied" {
		if event.Changed == nil {
			return nil
		}
		changed := *event.Changed
		changed.Channel = message.Channel
		m.eventVersion++
		m.messageVersions[changed.Channel+"/"+changed.TS] = m.eventVersion
		m.threadVersions[changed.Channel+"/"+changed.TS] = m.eventVersion
		for i, old := range m.history {
			if old.TS == changed.TS && m.active == changed.Channel {
				m.history[i] = changed
			}
		}
		if m.thread != nil {
			for i, old := range m.thread.history {
				if old.TS == changed.TS && m.active == changed.Channel {
					m.thread.history[i] = changed
				}
			}
		}
		for i, thread := range m.feed {
			if thread.Root.Channel == changed.Channel && thread.Root.TS == changed.TS {
				m.feed[i].Root = changed
			}
		}
		m.renderHistory()
		return nil
	}
	if message.Subtype == "message_deleted" {
		m.eventVersion++
		m.messageVersions[message.Channel+"/"+event.DeletedTS] = m.eventVersion
		if m.active == message.Channel {
			kept := m.history[:0]
			for _, old := range m.history {
				if old.TS != event.DeletedTS {
					kept = append(kept, old)
				}
			}
			m.history = kept
			if m.thread != nil {
				parent := []slack.Message{}
				for _, old := range m.thread.history {
					if old.TS != event.DeletedTS {
						parent = append(parent, old)
					}
				}
				m.thread.history = parent
			}
			m.renderHistory()
		}
		return nil
	}
	if !realMessage(message) || !m.rememberLive(key) {
		return nil
	}
	m.eventVersion++
	m.messageVersions[key] = m.eventVersion
	m.channelVersions[message.Channel] = m.eventVersion
	own := message.User == m.session.UserID
	thread := message.ThreadTS
	isReply := thread != "" && thread != message.TS
	broadcast := message.Subtype == "thread_broadcast" || message.Subtype == "reply_broadcast"
	m.clearTyping(message.Channel, thread, message.User)
	if own {
		m.ownLatest[message.Channel] = laterTS(m.ownLatest[message.Channel], message.TS)
		m.recordInteraction(message.Channel)
	}
	if !isReply || broadcast {
		previous := m.latest[message.Channel]
		m.latest[message.Channel] = laterTS(previous, message.TS)
		if !own && slack.TimestampTime(message.TS).After(slack.TimestampTime(laterTS(m.remoteRead[message.Channel], m.readPositions[message.Channel]))) {
			u, exists := m.unread[message.Channel]
			if !exists || slack.TimestampTime(message.TS).After(slack.TimestampTime(u.latest)) {
				if exists {
					u.count++
				} else {
					u = unreadState{after: laterTS(m.remoteRead[message.Channel], m.readPositions[message.Channel]), count: 1, exact: previous != "" && !slack.TimestampTime(previous).After(slack.TimestampTime(laterTS(m.remoteRead[message.Channel], m.readPositions[message.Channel])))}
				}
				u.latest = m.latest[message.Channel]
				if u.exact {
					u.counted = u.latest
				}
				m.unread[message.Channel] = u
			}
		}
	}
	var feedRefresh tea.Cmd
	if isReply {
		found := false
		for _, item := range m.feed {
			if item.Root.Channel == message.Channel && item.Root.TS == thread {
				found = true
				break
			}
		}
		if !found && (m.destination == threadsDestination || own || (m.thread != nil && m.active == message.Channel && m.thread.ts == thread)) && !m.feedLoading && m.now().Sub(m.lastLiveFeedRefresh) >= 2*time.Second {
			m.lastLiveFeedRefresh = m.now()
			feedRefresh = m.requestThreads("")
		}
		m.lastThreadMutation = m.eventVersion
		m.threadVersions[message.Channel+"/"+thread] = m.eventVersion
		m.messageVersions[message.Channel+"/"+thread] = m.eventVersion
		if m.active == message.Channel {
			updateReplyParent(m.history, thread, message.TS)
			if m.thread != nil {
				updateReplyParent(m.thread.history, thread, message.TS)
			}
		}
		for i, item := range m.feed {
			if item.Root.Channel != message.Channel || item.Root.TS != thread {
				continue
			}
			m.feed[i].LatestReplies = mergeMessages(item.LatestReplies, []slack.Message{message})
			if slack.TimestampTime(message.TS).After(slack.TimestampTime(item.Root.LatestReply)) {
				m.feed[i].Root.ReplyCount++
			}
			m.feed[i].Root.LatestReply = laterTS(item.Root.LatestReply, message.TS)
			present := false
			for _, old := range item.UnreadReplies {
				if old.TS == message.TS {
					present = true
				}
			}
			if !own && !present && slack.TimestampTime(message.TS).After(slack.TimestampTime(laterTS(item.Root.LastRead, m.readPositions[item.Key()]))) {
				m.feed[i].UnreadReplies = mergeMessages(item.UnreadReplies, []slack.Message{message})
				m.feedUnread++
			}
		}
		sort.SliceStable(m.feed, func(i, j int) bool {
			return slack.TimestampTime(m.feed[i].Latest()).After(slack.TimestampTime(m.feed[j].Latest()))
		})
	}
	if m.thread != nil && !m.thread.fromFeed && m.active == message.Channel && (!isReply || broadcast) {
		m.thread.history = mergeMessages(m.thread.history, []slack.Message{message})
	}
	matchesView := m.destination == "" && m.active == message.Channel && ((m.thread == nil && (!isReply || broadcast)) || (m.thread != nil && thread == m.thread.ts))
	var read tea.Cmd
	if matchesView {
		bottom := m.viewport.AtBottom()
		m.history = mergeMessages(m.history, []slack.Message{message})
		read = m.readLoadedHistory()
		m.renderHistory()
		if bottom {
			m.viewport.GotoBottom()
		}
	} else if m.active == message.Channel && m.thread == nil && isReply {
		m.renderHistory()
	}
	if m.destination == threadsDestination {
		m.renderThreads()
	}
	known := false
	for _, c := range m.conversations {
		if c.ID == message.Channel {
			known = true
			break
		}
	}
	var bootstrap tea.Cmd
	if !known && m.ready && !m.booting && time.Now().After(m.retryUntil) {
		m.booting = true
		bootstrap = m.bootstrap()
	}
	var sound tea.Cmd
	if !own && (message.User != "" || message.BotID != "" || message.Username != "") && slack.TimestampTime(message.TS).After(m.startedAt) && m.soundEnabled {
		sound = m.notify()
	}
	m.status = "New message received"
	if own {
		m.status = "Message updated"
	}
	return tea.Batch(read, sound, bootstrap, feedRefresh)
}
func typingKey(channel, thread string) string {
	if thread != "" {
		return channel + "/" + thread
	}
	return channel
}
func (m *Model) updateTyping(message slack.Message) tea.Cmd {
	if m.liveState != "live" || message.Channel == "" || message.User == "" || message.User == m.session.UserID {
		return nil
	}
	key := typingKey(message.Channel, message.ThreadTS)
	if m.typing[key] == nil {
		m.typing[key] = map[string]time.Time{}
	}
	m.typing[key][message.User] = m.now().Add(typingLifetime)
	if m.typingScheduled {
		return nil
	}
	m.typingScheduled = true
	m.typingGeneration++
	generation := m.typingGeneration
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return typingTick{generation} })
}
func (m *Model) expireTyping(msg typingTick) tea.Cmd {
	if msg.generation != m.typingGeneration {
		return nil
	}
	m.typingScheduled = false
	for key, users := range m.typing {
		for user, until := range users {
			if !until.After(m.now()) {
				delete(users, user)
			}
		}
		if len(users) == 0 {
			delete(m.typing, key)
		}
	}
	if len(m.typing) == 0 {
		return nil
	}
	m.typingScheduled = true
	generation := m.typingGeneration
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return typingTick{generation} })
}
func (m *Model) clearTyping(channel, thread, user string) {
	delete(m.typing[typingKey(channel, thread)], user)
	// Some Slack clients omit thread_ts in the typing event.
	delete(m.typing[channel], user)
}
func (m Model) typingText(channel, thread string) string {
	if m.liveState != "live" || channel == "" {
		return ""
	}
	names := []string{}
	for user, until := range m.typing[typingKey(channel, thread)] {
		if !until.After(m.now()) {
			continue
		}
		name := m.users[user]
		if name == "" {
			name = "Someone"
		}
		names = append(names, cleanLine(name))
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing…"
	case 2:
		return strings.Join(names, " and ") + " are typing…"
	default:
		return fmt.Sprintf("%d people are typing…", len(names))
	}
}
func (m Model) channelTyping(channel string) bool {
	if m.liveState != "live" {
		return false
	}
	for key, users := range m.typing {
		if key != channel && !strings.HasPrefix(key, channel+"/") {
			continue
		}
		for _, until := range users {
			if until.After(m.now()) {
				return true
			}
		}
	}
	return false
}

func updateReplyParent(messages []slack.Message, thread, latest string) {
	for i, root := range messages {
		if root.TS == thread && slack.TimestampTime(latest).After(slack.TimestampTime(root.LatestReply)) {
			messages[i].ReplyCount++
			messages[i].LatestReply = latest
		}
	}
}
