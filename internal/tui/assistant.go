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

const (
	assistantConversations = 15
	assistantBudget        = 150000 // bytes of transcript sent with one question
	assistantTurns         = 20
)

type assistantTurn struct {
	question, answer string
	partial          bool
	err              string
}

type assistantMsg struct {
	question, answer string
	partial          bool
	err              error
}

type conversationTarget struct {
	id, label string
}

// askAssistant answers the composer's question from the last 14 days of the
// first conversations in the list, citing source links.
func (m *Model) askAssistant() tea.Cmd {
	question := strings.TrimSpace(m.input.Value())
	if question == "" {
		return nil
	}
	if m.demo {
		m.status = "The assistant is offline in the demo; configure AI in the signed-in app"
		return nil
	}
	if !m.provider.Ready() {
		m.status = "Enable AI and set a chat model in Settings (,)"
		return m.openSettings()
	}
	if m.assistantBusy {
		m.status = "Still answering the last question"
		return nil
	}
	api, ok := m.api.(slack.ContextAPI)
	if !ok {
		m.status = "Conversation context is unavailable for this backend"
		return nil
	}
	names := map[string]string{}
	for id, name := range m.users {
		names[id] = name
	}
	var targets []conversationTarget
	for _, conversation := range m.conversations {
		if conversation.IsArchived {
			continue
		}
		label := conversation.Name
		if conversation.IsIM {
			label = "DM with " + names[conversation.User]
		}
		targets = append(targets, conversationTarget{id: conversation.ID, label: label})
		if len(targets) == assistantConversations {
			break
		}
	}
	if len(targets) == 0 {
		m.status = errNoConversations.Error()
		return nil
	}
	m.input.Reset()
	m.assistantBusy = true
	m.status = "Searching the last 14 days…"
	m.renderDestination()
	ctx, provider, team := m.ctx, m.provider, m.session.TeamID
	until := m.now()
	since := until.Add(-summaryWindow)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		transcript, partial, read := buildAssistantTranscript(ctx, api, team, names, targets, since, until)
		if read == 0 {
			return assistantMsg{question: question, err: errors.New("could not read any conversation; check the Slack connection")}
		}
		answer, err := inference.Ask(ctx, provider, question, transcript, partial)
		return assistantMsg{question: question, answer: answer, partial: partial, err: err}
	}
}

// buildAssistantTranscript reads each target and returns the transcript, whether
// coverage is partial, and how many conversations were read successfully.
func buildAssistantTranscript(ctx context.Context, api slack.ContextAPI, team string, names map[string]string, targets []conversationTarget, since, until time.Time) (string, bool, int) {
	var out strings.Builder
	partial, read := false, 0
	for _, target := range targets {
		if ctx.Err() != nil {
			return out.String(), true, read
		}
		page, err := api.ConversationContext(ctx, target.id, since, until)
		if err != nil {
			partial = true
			continue
		}
		read++
		partial = partial || page.Partial
		sort.Slice(page.Messages, func(i, j int) bool {
			return slack.TimestampTime(page.Messages[i].TS).Before(slack.TimestampTime(page.Messages[j].TS))
		})
		section := fmt.Sprintf("\n=== Conversation: %s ===\n", cleanLine(target.label))
		if out.Len()+len(section) > assistantBudget {
			return out.String(), true, read
		}
		out.WriteString(section)
		for _, message := range page.Messages {
			name := names[message.User]
			if name == "" {
				name = message.User
			}
			line := fmt.Sprintf("[%s] %s %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(name), messageSourceURL(team, target.id, message.TS), clean(message.Text))
			if out.Len()+len(line) > assistantBudget {
				return out.String(), true, read
			}
			out.WriteString(line)
		}
	}
	return out.String(), partial, read
}

// recordAnswer appends a finished question to the assistant log.
func (m *Model) recordAnswer(event assistantMsg) {
	m.assistantBusy = false
	turn := assistantTurn{question: event.question, answer: event.answer, partial: event.partial}
	if event.err != nil {
		turn.err = cleanLine(event.err.Error())
		m.setError(event.err)
	} else {
		m.status = "Answer ready · review the sources before acting"
	}
	m.assistant = append(m.assistant, turn)
	if len(m.assistant) > assistantTurns {
		m.assistant = m.assistant[len(m.assistant)-assistantTurns:]
	}
	if m.destination == aiAssistant {
		m.renderDestination()
	}
}

// assistantView renders the question log for the assistant pane.
func (m Model) assistantView() string {
	width := max(1, m.viewport.Width()-4)
	var b strings.Builder
	for _, turn := range m.assistant {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("You: "+clean(turn.question)) + "\n\n")
		switch {
		case turn.err != "":
			b.WriteString("Could not answer: " + turn.err + "\n")
		default:
			b.WriteString(clean(turn.answer) + "\n")
			if turn.partial {
				b.WriteString(lipgloss.NewStyle().Foreground(m.muted()).Render("Partial coverage: some conversations or messages were not read.") + "\n")
			}
		}
		b.WriteString("\n")
	}
	if m.assistantBusy {
		b.WriteString(lipgloss.NewStyle().Foreground(m.muted()).Render("Searching the last 14 days and writing an answer…"))
	}
	return ansi.Hardwrap(strings.TrimRight(b.String(), "\n"), width, true)
}
