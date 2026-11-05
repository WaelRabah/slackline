package tui

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
)

// dailyCatchUpHour is the local hour when the day's catch-up runs. The app must
// be open; if it starts later the same day, the catch-up runs at startup.
const dailyCatchUpHour = 11

type dailyTick struct{}

// dailyTickCmd checks the schedule once a minute.
func dailyTickCmd() tea.Cmd {
	return tea.Tick(time.Minute, func(time.Time) tea.Msg { return dailyTick{} })
}

// dailyDue reports whether today's catch-up should start now.
func dailyDue(now time.Time, lastRun string, ready, busy bool) bool {
	if !ready || busy || now.Hour() < dailyCatchUpHour {
		return false
	}
	return lastRun != now.Local().Format("2006-01-02")
}

// dailyCatchUp starts today's catch-up once it is due. The day is recorded when
// the run starts, so a failure does not restart it every minute.
func (m *Model) dailyCatchUp(now time.Time) tea.Cmd {
	if m.demo || !dailyDue(now, m.lastDailyRun, m.provider.Ready(), m.catchUp.busy) {
		return nil
	}
	m.lastDailyRun = now.Local().Format("2006-01-02")
	if err := m.SaveState(); err != nil {
		m.setError(err)
	}
	sig := m.unreadSignature()
	if sig == "" {
		return nil
	}
	m.status = "Daily catch-up started"
	return m.runCatchUp(sig)
}

// summaryChime plays the chime when a summary finished successfully.
func (m Model) summaryChime(ok bool) tea.Cmd {
	if !ok || !m.soundEnabled {
		return nil
	}
	return chime(m.ctx)
}

func chime(ctx context.Context) tea.Cmd {
	if runtime.GOOS != "darwin" {
		return tea.Raw("\a")
	}
	return func() tea.Msg {
		soundCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_ = exec.CommandContext(soundCtx, "/usr/bin/afplay", "/System/Library/Sounds/Ping.aiff").Run()
		return nil
	}
}

// unreadsSucceeded reports whether every channel in a run was summarized.
func unreadsSucceeded(msg unreadsMsg) bool {
	for _, section := range msg.sections {
		if section.err != "" {
			return false
		}
	}
	return true
}
