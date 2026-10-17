package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

type dmOpened struct {
	conversation slack.Conversation
	generation   int
	err          error
}

func (m Model) finderLabel(index int) string {
	if index <= messageMatchBase {
		hit := m.searchHits[messageMatchBase-index]
		name := m.label(hit.Conversation)
		return name + " · " + cleanLine(m.users[hit.Message.User]) + " · " + cleanLine(hit.Message.Text)
	}
	if index >= 0 {
		return m.label(m.conversations[index])
	}
	id := m.finderUsers[-index-1]
	return "@" + cleanLine(m.users[id]) + " · workspace user"
}
func (m *Model) openFinderUser(user string) tea.Cmd {
	for i, c := range m.conversations {
		if c.IsIM && c.User == user {
			m.selected = i
			m.mode = normal
			m.query.Blur()
			return m.openSelected()
		}
	}
	api, ok := m.api.(interface {
		OpenDM(context.Context, string) (slack.Conversation, error)
	})
	if !ok {
		m.status = "Opening a new DM is unavailable for this backend"
		return nil
	}
	if !m.canRequest() {
		return nil
	}
	m.finderGeneration++
	generation := m.finderGeneration
	ctx := m.ctx
	m.status = "Opening direct message…"
	m.mode = normal
	m.query.Blur()
	return func() tea.Msg {
		c, err := api.OpenDM(ctx, user)
		if err == nil && c.ID == "" {
			err = fmt.Errorf("Slack returned no direct message")
		}
		return dmOpened{c, generation, err}
	}
}

const messageMatchBase = -1000000

type searchMsg struct {
	generation int
	page       slack.SearchPage
	err        error
}

func (m *Model) searchMessages() tea.Cmd {
	query := strings.TrimSpace(strings.TrimPrefix(m.query.Value(), "?"))
	if query == "" || m.searchLoading {
		return nil
	}
	api, ok := m.api.(slack.SearchAPI)
	if !ok {
		m.status = "Message search is unavailable for this backend"
		return nil
	}
	if !m.canRequest() {
		return nil
	}
	m.finderGeneration++
	generation := m.finderGeneration
	m.searchLoading = true
	m.searchHits = nil
	m.matches = nil
	ctx, since := m.ctx, m.now().Add(-14*24*time.Hour)
	m.status = "Searching messages from the last 14 days…"
	return func() tea.Msg {
		page, err := api.SearchMessages(ctx, query, since)
		return searchMsg{generation, page, err}
	}
}
func (m *Model) openSearchHit(hit slack.SearchHit) tea.Cmd {
	index := -1
	for i, c := range m.conversations {
		if c.ID == hit.Conversation.ID {
			index = i
			break
		}
	}
	if index < 0 {
		m.conversations = append(m.conversations, hit.Conversation)
		index = len(m.conversations) - 1
	}
	m.selected = index
	m.mode = normal
	m.query.Blur()
	m.openSelected() // Invalidate outstanding history before installing search context.
	m.generation++
	m.loading = false
	if m.thread != nil {
		m.closeThread()
	}
	m.history = mergeMessages(m.history, []slack.Message{hit.Message})
	m.selectedMessage = hit.Message.TS
	m.resize()
	for i, message := range m.history {
		if message.TS == hit.Message.TS {
			m.selectBlock(i)
			break
		}
	}
	m.revealSidebarSelection()
	if hit.Message.ThreadTS != "" && hit.Message.ThreadTS != hit.Message.TS {
		return m.openThread()
	}
	// Keep the result visible while loading current history around it.
	m.revealMessage = hit.Message.TS
	return m.fetchHistory("")
}
