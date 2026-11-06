package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

// mutedRefresh bounds how long a mute made in Slack takes to reach Latest. The daily tick runs every minute.
const mutedRefresh = time.Minute

// mutedMsg carries the muted conversations read from the user's Slack preferences.
type mutedMsg struct {
	channels map[string]bool
	err      error
}

// mutedCmd reads the muted conversations in the background.
func (m *Model) mutedCmd() tea.Cmd {
	api, ok := m.api.(slack.MutedAPI)
	if !ok || m.demo {
		return nil
	}
	m.mutedRequested = time.Now()
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		channels, err := api.MutedChannels(ctx)
		return mutedMsg{channels: channels, err: err}
	}
}

// refreshMuted reads the muted list again when it is older than mutedRefresh.
func (m *Model) refreshMuted() tea.Cmd {
	if m.mutedRequested.IsZero() || time.Since(m.mutedRequested) < mutedRefresh {
		return nil
	}
	return m.mutedCmd()
}

// applyMuted stores the muted list and removes Latest entries from channels that are now muted.
func (m *Model) applyMuted(msg mutedMsg) {
	if msg.err != nil {
		return
	}
	m.mutedChannels = msg.channels
	kept := m.recent[:0]
	for _, item := range m.recent {
		if item.kind == "message" && m.mutedChannels[item.conversation.ID] {
			delete(m.recentSeen, item.key)
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) != len(m.recent) {
		m.recent = kept
		m.latestIndex = min(m.latestIndex, max(0, len(m.recent)-1))
		m.persistLatest()
	}
}
