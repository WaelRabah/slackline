package tui

import (
	"context"
	"errors"
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

const summaryWindow = 14 * 24 * time.Hour

type conversationSummary struct {
	Text             string `json:"text"`
	Generated, Since time.Time
	Messages         int
	Partial          bool
	// Newest is the newest message timestamp known when the summary started. A
	// later message makes the summary stale; nothing else does.
	Newest string `json:"newest"`
}
type summaryMsg struct {
	channel string
	summary conversationSummary
	err     error
}

// summarizeConversation handles the manual s key: it always regenerates.
func (m *Model) summarizeConversation() tea.Cmd {
	if m.destination == unreadsDestination {
		return m.refreshUnreads()
	}
	if m.destination == aiSummaries {
		return m.refreshCatchUp()
	}
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
	if !m.canRequest() {
		return nil
	}
	return m.startSummary(m.active)
}

// autoSummary runs for the open conversation when it has no summary yet or a
// message arrived after the stored summary. Otherwise it does nothing.
func (m *Model) autoSummary(channel string) tea.Cmd {
	if channel == "" || m.demo || !m.provider.Ready() || m.summaryBusy[channel] || !m.summaryStale(channel) {
		return nil
	}
	return m.startSummary(channel)
}

// summaryStale reports whether the known newest message is missing from the stored summary.
func (m *Model) summaryStale(channel string) bool {
	latest := m.latestSeen[channel]
	if latest == "" {
		return false
	}
	summary, ok := m.summaries[channel]
	return !ok || slack.TimestampTime(latest).After(slack.TimestampTime(summary.Newest))
}

// summaryHook keeps the newest-message record current and starts automatic runs.
// It runs after every update, so no event path can skip it.
func (m *Model) summaryHook(msg tea.Msg) tea.Cmd {
	if m.latestSeen == nil {
		m.latestSeen = map[string]string{}
	}
	switch event := msg.(type) {
	case historyMsg:
		if event.err != nil {
			return nil
		}
		for _, message := range event.history.Messages {
			// Thread replies are not part of the conversation view's newest-message check.
			if message.ThreadTS == "" || message.ThreadTS == message.TS {
				m.latestSeen[event.channel] = laterTS(m.latestSeen[event.channel], message.TS)
			}
		}
		if event.channel == m.active && event.cursor == "" {
			return m.autoSummary(event.channel)
		}
	case slack.LiveEvent:
		message := event.Message
		if event.Type != "message" || message.Channel == "" || message.TS == "" {
			return nil
		}
		if message.Subtype == "message_changed" || message.Subtype == "message_deleted" {
			return nil
		}
		m.latestSeen[message.Channel] = laterTS(m.latestSeen[message.Channel], message.TS)
		if message.Channel == m.active {
			return m.autoSummary(message.Channel)
		}
	case summaryMsg:
		// A failed run is not retried here, so a persistent provider error cannot loop.
		if event.err == nil && event.channel == m.active {
			return m.autoSummary(event.channel)
		}
	case assistantMsg:
		m.recordAnswer(event)
	case unreadsMsg:
		return m.applyUnreads(event)
	case catchUpMsg:
		m.applyCatchUp(event)
		return m.summaryChime(event.err == nil && event.text != "")
	case dailyTick:
		return tea.Batch(m.dailyCatchUp(m.now()), dailyTickCmd())
	case markedBlockMsg:
		m.applyMarkedBlock(event)
	}
	return nil
}

// startSummary requests a summary of the last 14 days for channel. The result
// covers every message known when it started.
func (m *Model) startSummary(channel string) tea.Cmd {
	api, ok := m.api.(slack.ContextAPI)
	if !ok {
		m.status = "Conversation context is unavailable for this backend"
		return nil
	}
	label, team, provider, ctx := m.activeLabel(), m.session.TeamID, m.provider, m.ctx
	until := m.now()
	since := until.Add(-summaryWindow)
	covered := m.latestSeen[channel]
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
			line := fmt.Sprintf("[%s] %s (thread %s) %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(name), message.ThreadTS, messageSourceURL(team, channel, message.TS), clean(message.Text))
			if size+len(line) > 100000 {
				partial = true
				continue
			}
			lines = append(lines, line)
			size += len(line)
			count++
		}
		summary := conversationSummary{Generated: until, Since: since, Messages: count, Partial: partial, Newest: covered}
		if count == 0 {
			summary.Text = "No messages found in this conversation in the last 14 days. Slack retention may limit coverage."
			return summaryMsg{channel: channel, summary: summary}
		}
		// Restore chronological order after selecting the newest bounded context.
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		transcript := fmt.Sprintf("Conversation: %s\nWindow: %s through %s\nMessages: %d. Partial coverage: %t. Slack retention may limit results.\n\n", cleanLine(label), since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339), count, partial) + strings.Join(lines, "\n")
		text, err := inference.Summarize(ctx, provider, transcript)
		summary.Text = clean(text)
		if err != nil {
			return summaryMsg{channel: channel, err: err}
		}
		return summaryMsg{channel: channel, summary: summary}
	}
}

// messageSourceURL builds the Slack link for one message in a conversation.
func messageSourceURL(team, channel, ts string) string {
	return "https://app.slack.com/client/" + team + "/" + channel + "/thread/" + channel + "-" + ts
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
	text += "\nSlack retention may limit coverage.\n\n" + clean(summary.Text)
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

var errNoConversations = errors.New("no conversations to search")
