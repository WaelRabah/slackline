package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

const activityInterval = 15 * time.Second
const recentInterval = 15 * time.Minute

type activityTick struct{}
type recentTick struct{}
type activityMsg struct {
	version int
	rows    []slack.Activity
	err     error
}
type recentMsg struct {
	sent map[string]time.Time
	err  error
}
type soundDoneMsg struct{ fallback bool }

func (m Model) activityCmd() tea.Cmd {
	api, ok := m.api.(slack.ActivityAPI)
	if !ok {
		return nil
	}
	ctx, version := m.ctx, m.eventVersion
	return func() tea.Msg {
		rows, err := api.Activity(ctx)
		return activityMsg{rows: rows, err: err, version: version}
	}
}
func (m Model) recentCmd() tea.Cmd {
	api, ok := m.api.(slack.RecentAPI)
	if !ok {
		return nil
	}
	ctx, since := m.ctx, m.now().Add(-time.Duration(max(14, m.recentDays))*24*time.Hour)
	return func() tea.Msg { sent, err := api.RecentSent(ctx, since); return recentMsg{sent, err} }
}
func (m Model) activityTimer() tea.Cmd {
	delay := max(activityInterval, time.Until(m.retryUntil))
	return tea.Tick(delay, func(time.Time) tea.Msg { return activityTick{} })
}
func (m Model) recentTimer() tea.Cmd {
	return tea.Tick(max(recentInterval, time.Until(m.retryUntil)), func(time.Time) tea.Msg { return recentTick{} })
}
func (m *Model) updateActivity(msg activityMsg) tea.Cmd {
	if msg.err != nil {
		m.setError(msg.err)
		m.status = "Activity check failed; " + m.status
		return m.activityTimer()
	}
	changed := false
	reload := false
	known := map[string]bool{}
	for _, c := range m.conversations {
		known[c.ID] = true
	}
	for _, row := range msg.rows {
		if row.ID == "" {
			continue
		}
		if m.channelVersions[row.ID] > msg.version {
			continue
		}
		m.updateUnread(row)
		if row.HasUnreads && !known[row.ID] && m.ready {
			reload = true
		}
		at := slack.TimestampTime(row.LastRead)
		if at.After(m.interactions[row.ID]) {
			m.interactions[row.ID] = at
		}
		latest, previous := slack.TimestampTime(row.Latest), slack.TimestampTime(m.latest[row.ID])
		if !latest.After(previous) {
			continue
		}
		m.latest[row.ID] = row.Latest
		if m.activityReady {
			if row.ID == m.active {
				changed = true
			}

		}
	}
	m.activityReady = true
	m.pruneInteractions()
	var refresh tea.Cmd
	if changed && !m.loading {
		refresh = m.fetchHistory("")
	}
	var bootstrap tea.Cmd
	if reload && !m.booting && time.Now().After(m.retryUntil) {
		m.booting = true
		bootstrap = m.bootstrap()
	}
	return tea.Batch(m.activityTimer(), refresh, m.nextUnreadCount(), bootstrap)
}
func (m Model) filterReady() bool { return m.activityReady && m.sentReady }
func (m Model) visibleConversation(id string) bool {
	if _, ok := m.unread[id]; ok {
		return true
	}
	if !m.recentOnly || !m.filterReady() {
		return true
	}
	at := m.interactions[id]
	return !at.IsZero() && !at.Before(m.now().Add(-time.Duration(m.recentDays)*24*time.Hour))
}
func (m *Model) recordInteraction(id string) {
	if id != "" {
		m.interactions[id] = m.now()
	}
}
func (m *Model) pruneInteractions() {
	cutoff := m.now().Add(-time.Duration(max(14, m.recentDays)) * 24 * time.Hour)
	for id, at := range m.interactions {
		if at.Before(cutoff) {
			delete(m.interactions, id)
		}
	}
}
func (m Model) filterLabel() string {
	if !m.recentOnly {
		return "all"
	}
	if !m.filterReady() {
		return fmt.Sprintf("%dd pending; showing all", m.recentDays)
	}
	return fmt.Sprintf("last %d days", m.recentDays)
}
func (m Model) sidebarTitle() string {
	if m.recentOnly && m.filterReady() {
		return fmt.Sprintf("CHANNELS · %dD", m.recentDays)
	}
	return "CHANNELS · ALL"
}
func notification(ctx context.Context) tea.Cmd {
	if runtime.GOOS != "darwin" {
		return tea.Raw("\a")
	}
	return func() tea.Msg {
		soundCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		err := exec.CommandContext(soundCtx, "/usr/bin/afplay", "/System/Library/Sounds/Glass.aiff").Run()
		return soundDoneMsg{fallback: err != nil && ctx.Err() == nil}
	}
}
