package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
)

const (
	assistantTurns      = 20
	assistantToolBudget = 300000 // bytes of Slack content the tools may return for one question
	assistantToolOutput = 60000  // bytes per tool result
	assistantMaxDays    = 90
	assistantDefaultDay = 14
	assistantDateLayout = "2006-01-02"
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

// assistantTools is the tool list the model sees.
var assistantTools = []inference.Tool{
	{
		Name:        "list_conversations",
		Description: "List the conversations the user can access: channels, DMs, group DMs and followed threads, with IDs and unread counts. Call this first to find the right channel_id or thread_ts.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
	},
	{
		Name:        "get_messages",
		Description: "Read messages from one conversation in a date range. Use thread_ts to read one thread. The default range is the last 14 days; the maximum range is 90 days. Dates are YYYY-MM-DD, and end_date is inclusive.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Conversation ID from list_conversations."},
				"thread_ts":  map[string]any{"type": "string", "description": "Optional thread timestamp from list_conversations."},
				"start_date": map[string]any{"type": "string", "description": "First day, YYYY-MM-DD. Defaults to 14 days before end_date."},
				"end_date":   map[string]any{"type": "string", "description": "Last day, YYYY-MM-DD. Defaults to today."},
			},
			"required":             []string{"channel_id"},
			"additionalProperties": false,
		},
	},
}

// askAssistant answers the composer's question. The model reads Slack through
// tools, so it can search any conversation, not just the open one.
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
	if _, ok := m.api.(slack.ContextAPI); !ok {
		m.status = "Conversation context is unavailable for this backend"
		return nil
	}
	m.input.Reset()
	m.assistantBusy = true
	m.status = "Looking through your conversations…"
	m.renderDestination()
	handler := m.assistantHandler()
	ctx, provider := m.ctx, m.provider
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		defer cancel()
		answer, err := inference.Assist(ctx, provider, inference.AssistantSystem, question, assistantTools, handler.run)
		return assistantMsg{question: question, answer: answer, partial: handler.partial(), err: err}
	}
}

// assistantToolHandler runs the model's tool calls against Slack. It is used from
// a single goroutine per question, so its counters need no locking.
type assistantToolHandler struct {
	api       slack.API
	team      string
	names     map[string]string
	labels    map[string]string
	list      []conversationEntry
	now       time.Time
	used      int
	truncated bool
}

type conversationEntry struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	ThreadTS string `json:"thread_ts,omitempty"`
	Unread   int    `json:"unread,omitempty"`
}

func (m *Model) assistantHandler() *assistantToolHandler {
	h := &assistantToolHandler{api: m.api, team: m.session.TeamID, names: map[string]string{}, labels: map[string]string{}, now: m.now()}
	for id, name := range m.users {
		h.names[id] = name
	}
	for _, conversation := range m.conversations {
		if conversation.IsArchived {
			continue
		}
		entry := conversationEntry{Type: "channel", ID: conversation.ID, Name: conversation.Name}
		switch {
		case conversation.IsIM:
			entry = conversationEntry{Type: "dm", ID: conversation.ID, Name: "DM with " + h.names[conversation.User]}
		case conversation.IsMPIM:
			entry.Type = "group"
		case conversation.IsPrivate:
			entry.Type = "private channel"
		}
		if state, ok := m.unread[conversation.ID]; ok {
			entry.Unread = max(1, state.count)
		}
		h.labels[conversation.ID] = entry.Name
		h.list = append(h.list, entry)
	}
	return h
}

func (h *assistantToolHandler) partial() bool { return h.truncated }

func (h *assistantToolHandler) run(ctx context.Context, name, arguments string) (string, error) {
	switch name {
	case "list_conversations":
		return h.listConversations(ctx)
	case "get_messages":
		return h.getMessages(ctx, arguments)
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

func (h *assistantToolHandler) listConversations(ctx context.Context) (string, error) {
	entries := append([]conversationEntry{}, h.list...)
	if threads, ok := h.api.(slack.ThreadsAPI); ok {
		page, err := threads.Threads(ctx, "")
		if err == nil {
			for _, thread := range page.Threads {
				label := cleanLine(h.labels[thread.Root.Channel])
				if label == "" {
					label = thread.Root.Channel
				}
				snippet := []rune(cleanLine(thread.Root.Text))
				if len(snippet) > 80 {
					snippet = append(snippet[:80], '…')
				}
				entries = append(entries, conversationEntry{Type: "thread", ID: thread.Root.Channel, Name: label + ": " + string(snippet), ThreadTS: thread.Root.TS, Unread: len(thread.UnreadReplies)})
			}
		}
	}
	out, err := json.Marshal(map[string]any{"conversations": entries})
	if err != nil {
		return "", errors.New("cannot list conversations")
	}
	return h.budget(string(out))
}

type messagesArgs struct {
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"thread_ts"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func (h *assistantToolHandler) getMessages(ctx context.Context, arguments string) (string, error) {
	var args messagesArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil || strings.TrimSpace(args.ChannelID) == "" {
		return "", errors.New("channel_id is required")
	}
	end, err := parseAssistantDate(args.EndDate, h.now)
	if err != nil {
		return "", errors.New("end_date must be YYYY-MM-DD")
	}
	start, err := parseAssistantDate(args.StartDate, end.AddDate(0, 0, -assistantDefaultDay))
	if err != nil {
		return "", errors.New("start_date must be YYYY-MM-DD")
	}
	if end.Before(start) {
		return "", errors.New("end_date is before start_date")
	}
	if end.Sub(start) > assistantMaxDays*24*time.Hour {
		return "", fmt.Errorf("period is longer than %d days; use a narrower range", assistantMaxDays)
	}
	end = end.Add(24*time.Hour - time.Second)

	var messages []slack.Message
	partial := false
	if args.ThreadTS != "" {
		replies, ok := h.api.(interface {
			Replies(context.Context, string, string, string) (slack.History, error)
		})
		if !ok {
			return "", errors.New("threads are unavailable for this backend")
		}
		cursor := ""
		for page := 0; page < 3; page++ {
			history, err := replies.Replies(ctx, args.ChannelID, args.ThreadTS, cursor)
			if err != nil {
				return "", errors.New("cannot read that thread")
			}
			messages = append(messages, history.Messages...)
			cursor = history.ResponseMetadata.NextCursor
			if cursor == "" || !history.HasMore {
				break
			}
			if page == 2 {
				partial = true
			}
		}
	} else {
		page, err := h.api.(slack.ContextAPI).ConversationContext(ctx, args.ChannelID, start, end)
		if err != nil {
			return "", errors.New("cannot read that conversation: " + cleanLine(err.Error()))
		}
		messages = page.Messages
		partial = page.Partial
	}
	sort.Slice(messages, func(i, j int) bool {
		return slack.TimestampTime(messages[i].TS).Before(slack.TimestampTime(messages[j].TS))
	})

	var out strings.Builder
	fmt.Fprintf(&out, "Conversation %s (%s), %s to %s. Messages: %d.\n", args.ChannelID, cleanLine(h.labels[args.ChannelID]), start.Format(assistantDateLayout), end.Format(assistantDateLayout), len(messages))
	for _, message := range messages {
		name := h.names[message.User]
		if name == "" {
			name = message.User
		}
		fmt.Fprintf(&out, "[%s] %s %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(name), messageSourceURL(h.team, args.ChannelID, message.TS), clean(message.Text))
		if out.Len() > assistantToolOutput {
			partial = true
			break
		}
	}
	if partial {
		out.WriteString("Partial: this read was capped; the answer may be incomplete.\n")
	}
	h.truncated = h.truncated || partial
	return h.budget(out.String())
}

// budget enforces the per-question cap on Slack content returned to the model.
func (h *assistantToolHandler) budget(text string) (string, error) {
	if len(text) > assistantToolOutput {
		text = text[:assistantToolOutput]
		h.truncated = true
	}
	if h.used+len(text) > assistantToolBudget {
		h.truncated = true
		return "", errors.New("tool budget for this question is used up; answer with what was read and say what is missing")
	}
	h.used += len(text)
	return text, nil
}

func parseAssistantDate(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	return time.ParseInLocation(assistantDateLayout, value, time.Local)
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

// assistantView renders the question log for the assistant pane. Answers are
// Markdown; questions and errors are plain text.
func (m Model) assistantView() string {
	width := max(1, m.viewport.Width()-4)
	muted := lipgloss.NewStyle().Foreground(m.muted())
	var b strings.Builder
	for _, turn := range m.assistant {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("You: "+clean(turn.question)) + "\n\n")
		if turn.err != "" {
			b.WriteString("Could not answer: " + turn.err + "\n")
		} else {
			b.WriteString(renderMarkdown(turn.answer, width) + "\n")
			if turn.partial {
				b.WriteString(muted.Render("Partial coverage: some conversations or messages were not read.") + "\n")
			}
		}
		b.WriteString("\n")
	}
	if m.assistantBusy {
		b.WriteString(muted.Render("Looking through your conversations and writing an answer…"))
	}
	return strings.TrimRight(b.String(), "\n")
}
