package tui

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

func (m *Model) closeRightmost() {
	m.mode = normal
	m.input.Blur()
	if m.thread != nil {
		m.closeThread()
		return
	}
	if m.active != "" || m.destination != "" {
		m.leaveConversation()
		return
	}
	m.focus = sidebar
}
func (m *Model) cyclePane(delta int) {
	panes := []pane{sidebar, messages, composer}
	if m.thread != nil {
		panes = []pane{sidebar, parentPane, messages, composer}
	}
	for i, p := range panes {
		if m.focus == p {
			m.focus = panes[(i+delta+len(panes))%len(panes)]
			return
		}
	}
	m.focus = sidebar
}
func (m Model) parentModel(width int) Model {
	copy := m
	copy.viewport = viewport.New(viewport.WithWidth(max(1, width-4)), viewport.WithHeight(max(1, m.mainHeight()-3)))
	copy.thread = nil
	copy.focus = messages
	copy.newMessages = map[string]bool{}
	if m.thread.fromFeed {
		copy.destination = threadsDestination
		copy.renderThreads()
	} else {
		copy.history = m.thread.history
		copy.cursor = m.thread.cursor
		copy.selectedMessage = m.thread.selection
		copy.viewport.SetHeight(max(1, m.mainHeight()-3-copy.summaryHeight()))
		copy.renderHistory()
	}
	copy.viewport.SetYOffset(m.thread.offset)
	return copy
}
func (m Model) conversationPanes(width int) string {
	if m.thread == nil {
		return m.paneFrame(m.messagePaneLabel(), m.conversationBody(max(1, width-4)), width, m.mainHeight(), m.focus == messages)
	}
	left := (width - 1) / 2
	right := width - left - 1
	parent := m.parentModel(left)
	channel := m.paneFrame(parent.messagePaneLabel(), parent.conversationBody(max(1, left-4)), left, m.mainHeight(), m.focus == parentPane)
	thread := m.paneFrame("REPLIES · "+m.activeLabel(), m.viewport.View(), right, m.mainHeight(), m.focus == messages)
	return lipgloss.JoinHorizontal(lipgloss.Top, channel, " ", thread)
}
func (m *Model) navigateParent(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	if k == "enter" || k == "r" {
		fromFeed := m.thread.fromFeed
		m.closeThread()
		var cmd tea.Cmd
		if fromFeed {
			cmd = m.openFeedThread()
		} else {
			cmd = m.openThread()
		}
		if k == "r" && m.thread != nil {
			m.focus = composer
			m.mode = insert
			return *m, tea.Batch(cmd, m.input.Focus())
		}
		return *m, cmd
	}
	parent := m.parentModel((m.width - m.sidebarWidth() - 2) / 2)
	switch k {
	case "j", "down":
		parent.moveBlock(1)
	case "k", "up":
		parent.moveBlock(-1)
	case "G":
		if m.thread.fromFeed {
			parent.selectFeed(len(parent.feed) - 1)
		} else {
			parent.selectBlock(len(parent.history) - 1)
		}
	case "g":
		if m.pendingG {
			if m.thread.fromFeed {
				parent.selectFeed(0)
			} else {
				parent.selectBlock(0)
			}
		}
		m.pendingG = !m.pendingG
	case "ctrl+d":
		parent.viewport.HalfPageDown()
	case "ctrl+u":
		parent.viewport.HalfPageUp()
	}
	m.thread.offset = parent.viewport.YOffset()
	m.thread.selection = parent.selectedMessage
	if m.thread.fromFeed {
		m.feedSelection = parent.feedSelection
	}
	return *m, nil
}

// Parent refreshes are independently tagged so a thread request cannot replace
// channel history (or vice versa).
func (m Model) fetchParentHistory() tea.Cmd {
	if m.thread == nil || m.thread.fromFeed || !m.canRequest() {
		return nil
	}
	channel, generation, version, api, ctx := m.active, m.generation, m.eventVersion, m.api, m.ctx
	return func() tea.Msg {
		h, err := api.History(ctx, channel, "")
		return parentHistoryMsg{channel, generation, version, h, err}
	}
}

type parentHistoryMsg struct {
	channel             string
	generation, version int
	history             slack.History
	err                 error
}
