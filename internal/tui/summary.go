package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

type conversationSummary struct {
	Text             string `json:"text"`
	Generated, Since time.Time
	Messages         int
	Partial          bool
}
type summaryMsg struct {
	channel string
	summary conversationSummary
	err     error
}

func (m *Model) summarizeConversation() tea.Cmd {
	if m.active == "" || m.destination != "" {
		m.status = "Open a conversation to summarize it"
		return nil
	}
	if m.demo {
		m.status = "Inference is offline in the demo; configure a provider in the signed-in app"
		return nil
	}
	if !m.provider.Ready() {
		m.status = "Configure and enable conversation summaries in Settings (,)"
		return m.openSettings()
	}
	if m.summaryBusy[m.active] {
		m.status = "Summary already in progress"
		return nil
	}
	if !m.canRequest() {
		return nil
	}
	api, ok := m.api.(slack.ContextAPI)
	if !ok {
		m.status = "Conversation context is unavailable for this backend"
		return nil
	}
	channel, label, team, provider, ctx := m.active, m.activeLabel(), m.session.TeamID, m.provider, m.ctx
	until := m.now()
	since := until.Add(-14 * 24 * time.Hour)
	names := map[string]string{}
	for id, name := range m.users {
		names[id] = name
	}
	m.summaryBusy[channel] = true
	m.status = "Summarizing the last 14 days…"
	m.resize()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		page, err := api.ConversationContext(ctx, channel, since, until)
		if err != nil {
			return summaryMsg{channel: channel, err: err}
		}
		sort.Slice(page.Messages, func(i, j int) bool {
			return slack.TimestampTime(page.Messages[i].TS).After(slack.TimestampTime(page.Messages[j].TS))
		})
		lines := []string{}
		size, count := 0, 0
		partial := page.Partial
		for _, message := range page.Messages {
			name := names[message.User]
			if name == "" {
				name = message.User
			}
			if name == "" {
				name = message.Username
			}
			source := "https://app.slack.com/client/" + team + "/" + channel + "/thread/" + channel + "-" + message.TS
			line := fmt.Sprintf("[%s] %s (thread %s) %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(name), message.ThreadTS, source, clean(message.Text))
			if size+len(line) > 100000 {
				partial = true
				continue
			}
			lines = append(lines, line)
			size += len(line)
			count++
		}
		summary := conversationSummary{Generated: until, Since: since, Messages: count, Partial: partial}
		if count == 0 {
			summary.Text = "No accessible messages found in the last 14 days. Slack search and workspace retention may limit coverage."
			return summaryMsg{channel: channel, summary: summary}
		}
		// Restore chronological order after selecting the newest bounded context.
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		transcript := fmt.Sprintf("Conversation: %s\nWindow: %s through %s\nMessages: %d. Partial coverage: %t. Slack search/retention may limit results.\n\n", cleanLine(label), since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339), count, partial) + strings.Join(lines, "\n")
		text, err := inference.Summarize(ctx, provider, transcript)
		summary.Text = clean(text)
		return summaryMsg{channel: channel, summary: summary, err: err}
	}
}
func (m Model) summaryHeight() int {
	if m.active == "" || m.destination != "" || m.thread != nil {
		return 0
	}
	if _, ok := m.summaries[m.active]; !ok && !m.summaryBusy[m.active] {
		return 0
	}
	return min(10, max(0, m.mainHeight()/2))
}
func (m Model) summaryCard(width, height int) string {
	summary, ok := m.summaries[m.active]
	text := "Gathering the last 14 days and generating a summary…"
	label := "PRIVATE SUMMARY · only you"
	if ok {
		text = summary.Text
		label += fmt.Sprintf(" · %d messages · %s", summary.Messages, summary.Generated.Local().Format("Jan 02 15:04"))
		if summary.Partial {
			label += " · PARTIAL"
		}
		if m.summaryBusy[m.active] {
			label += " · updating…"
		}
	}
	label += " · S expand / s refresh"
	text = ansi.Hardwrap(clean(text), max(1, width-4), true)
	lines := strings.Split(text, "\n")
	limit := max(1, height-3)
	if len(lines) > limit {
		lines = lines[:limit]
		lines[limit-1] = ansi.Truncate(lines[limit-1], max(1, width-6), "…") + "…"
	}
	return m.paneFrame(label, strings.Join(lines, "\n"), width, height, false)
}
func (m *Model) expandSummary() {
	summary, ok := m.summaries[m.active]
	if !ok {
		m.status = "s generates a private conversation summary"
		return
	}
	m.mode = summaryMode
	text := fmt.Sprintf("PRIVATE · only visible in this client\n%s – %s · %d messages", summary.Since.Format("Jan 02"), summary.Generated.Format("Jan 02 15:04"), summary.Messages)
	if summary.Partial {
		text += " · PARTIAL context"
	}
	text += "\nSlack search and retention may limit coverage.\n\n" + clean(summary.Text)
	m.summaryViewport.SetContent(ansi.Hardwrap(text, max(1, m.width-4), true))
	m.summaryViewport.GotoTop()
}

// conversationBody reserves a fixed bottom card while history scrolls independently.
func (m Model) conversationBody(width int) string {
	body := m.viewport.View()
	if height := m.summaryHeight(); height > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.summaryCard(width, height))
	}
	return body
}
