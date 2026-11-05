package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

const (
	latestDestination = "latest"
	maxLatest         = 200
)

// recentItem is one entry in the Latest list. Every entry records what made it
// and which sound, if any, played for it, so the source of a chime is visible.
type recentItem struct {
	at           time.Time
	kind         string // "message" or "summary"
	source       string // what triggered the entry, e.g. "Incoming message" or "AI catch-up ready"
	sound        string // the sound that played, empty if none
	key          string
	conversation slack.Conversation
	message      slack.Message
	label, text  string
}

// noteRecent adds an entry, newest first, without duplicates, and keeps the list bounded.
func (m *Model) noteRecent(item recentItem) {
	if m.recentSeen == nil {
		m.recentSeen = map[string]bool{}
	}
	if m.recentSeen[item.key] {
		return
	}
	m.recentSeen[item.key] = true
	m.recent = append(m.recent, item)
	sort.SliceStable(m.recent, func(i, j int) bool { return m.recent[i].at.After(m.recent[j].at) })
	for len(m.recent) > maxLatest {
		delete(m.recentSeen, m.recent[len(m.recent)-1].key)
		m.recent = m.recent[:len(m.recent)-1]
	}
	m.latestIndex = min(m.latestIndex, max(0, len(m.recent)-1))
	m.persistLatest()
}

// noteIncomingMessage records a live message that did not come from the user.
func (m *Model) noteIncomingMessage(channel string, message slack.Message, sound string) {
	conversation := m.conversationFor(channel)
	text := cleanLine(m.messageText(message.DisplayText()))
	sender := m.users[message.User]
	if sender == "" {
		sender = message.Username
	}
	if sender == "" {
		sender = "Slack"
	}
	m.noteRecent(recentItem{
		at:           slack.TimestampTime(message.TS),
		kind:         "message",
		source:       "Incoming message",
		sound:        sound,
		key:          channel + "/" + message.TS,
		conversation: conversation,
		message:      message,
		label:        m.conversationLabel(conversation) + " · " + cleanLine(sender),
		text:         text,
	})
}

// noteSummaryReady records a finished AI summary, with the sound that announced it.
func (m *Model) noteSummaryReady(source string, at time.Time) {
	m.noteRecent(recentItem{
		at:     at,
		kind:   "summary",
		source: source,
		sound:  m.soundLabel(),
		key:    "summary/" + source + "/" + at.Format(time.RFC3339Nano),
		label:  "AI summary",
	})
}

// soundLabel names the chime that plays now, or is empty when sounds are off.
func (m Model) soundLabel() string {
	if !m.soundEnabled {
		return ""
	}
	_, label := chimeSource()
	return label
}

// conversationFor returns the known conversation for an ID, or a bare one.
func (m Model) conversationFor(id string) slack.Conversation {
	for _, conversation := range m.conversations {
		if conversation.ID == id {
			return conversation
		}
	}
	return slack.Conversation{ID: id}
}

func (m Model) conversationLabel(conversation slack.Conversation) string {
	if conversation.IsIM {
		return "DM with " + m.users[conversation.User]
	}
	if conversation.Name != "" {
		return "#" + conversation.Name
	}
	return conversation.ID
}

// openLatest opens the selected entry. Summary entries have no message, so they
// stay where they are.
func (m *Model) openLatest() tea.Cmd {
	if len(m.recent) == 0 {
		return nil
	}
	index := min(m.latestIndex, len(m.recent)-1)
	item := m.recent[index]
	if item.kind != "message" {
		m.status = "Summary entries open with the AI Summaries view"
		return nil
	}
	// Opening a message takes it out of Latest: it has been seen.
	delete(m.recentSeen, item.key)
	m.recent = append(m.recent[:index], m.recent[index+1:]...)
	m.latestIndex = min(m.latestIndex, max(0, len(m.recent)-1))
	m.persistLatest()
	return m.openSearchHit(slack.SearchHit{Message: item.message, Conversation: item.conversation})
}

// latestView renders the list, newest first, with the source of each entry.
func (m Model) latestView() string {
	width := max(1, m.viewport.Width())
	muted := lipgloss.NewStyle().Foreground(m.muted())
	accent := lipgloss.NewStyle().Bold(true).Foreground(m.accent())
	if len(m.recent) > 0 {
		accent = accent.Foreground(latestYellow)
	}
	var b strings.Builder
	b.WriteString(accent.Render(fmt.Sprintf("Latest · %d", len(m.recent))) + "\n")
	if len(m.recent) == 0 {
		b.WriteString("\nNothing yet. New messages and finished summaries appear here.")
		return ansi.Hardwrap(b.String(), width, true)
	}
	for i, item := range m.recent {
		cursor := "  "
		if i == min(m.latestIndex, len(m.recent)-1) {
			cursor = "▶ "
		}
		icon := "✉"
		if item.kind == "summary" {
			icon = "✦"
		}
		head := fmt.Sprintf("%s%s %s · %s", cursor, item.at.Local().Format("Jan 02 15:04"), icon, item.label)
		b.WriteString("\n" + accent.Render(ansi.Truncate(head, width, "…")) + "\n")
		body := "    " + item.text
		if item.kind == "summary" {
			body = "    " + item.source
		}
		b.WriteString(ansi.Truncate(body, width, "…") + "\n")
		source := "    from: " + item.source
		b.WriteString(muted.Render(ansi.Truncate(source, width, "…")) + "\n")
	}
	return b.String()
}

// latestKey moves through the list or opens the selected entry.
func (m *Model) latestKey(key string) (tea.Cmd, bool) {
	switch key {
	case "j", "down", "n":
		if len(m.recent) > 0 {
			m.latestIndex = min(m.latestIndex+1, len(m.recent)-1)
			m.renderDestination()
		}
		return nil, true
	case "k", "up", "p":
		m.latestIndex = max(m.latestIndex-1, 0)
		m.renderDestination()
		return nil, true
	case "enter":
		return m.openLatest(), true
	case "x":
		m.dismissLatest()
		return nil, true
	}
	return nil, false
}

// dismissLatest removes the selected entry without opening it.
func (m *Model) dismissLatest() {
	if len(m.recent) == 0 {
		return
	}
	index := min(m.latestIndex, len(m.recent)-1)
	delete(m.recentSeen, m.recent[index].key)
	m.recent = append(m.recent[:index], m.recent[index+1:]...)
	m.latestIndex = min(m.latestIndex, max(0, len(m.recent)-1))
	m.persistLatest()
	m.renderDestination()
}

// latestYellow marks Latest while it has entries.
var latestYellow = lipgloss.Color("#F4B860")

// persistedRecent is the on-disk form of a Latest entry. It keeps the conversation
// and message, so an entry opens after a restart.
type persistedRecent struct {
	At           time.Time          `json:"at"`
	Kind         string             `json:"kind"`
	Source       string             `json:"source"`
	Sound        string             `json:"sound,omitempty"`
	Key          string             `json:"key"`
	Conversation slack.Conversation `json:"conversation"`
	Message      slack.Message      `json:"message"`
	Label        string             `json:"label"`
	Text         string             `json:"text"`
}

func persistRecent(items []recentItem) []persistedRecent {
	out := make([]persistedRecent, 0, len(items))
	for _, item := range items {
		out = append(out, persistedRecent{At: item.at, Kind: item.kind, Source: item.source, Sound: item.sound, Key: item.key, Conversation: item.conversation, Message: item.message, Label: item.label, Text: item.text})
	}
	return out
}

// restoreRecent rebuilds the Latest list from disk, newest first and bounded.
func (m *Model) restoreRecent(saved []persistedRecent) {
	m.recent = nil
	m.recentSeen = map[string]bool{}
	for _, entry := range saved {
		if entry.Key == "" || m.recentSeen[entry.Key] || len(m.recent) >= maxLatest {
			continue
		}
		m.recentSeen[entry.Key] = true
		m.recent = append(m.recent, recentItem{at: entry.At, kind: entry.Kind, source: entry.Source, sound: entry.Sound, key: entry.Key, conversation: entry.Conversation, message: entry.Message, label: entry.Label, text: entry.Text})
	}
	sort.SliceStable(m.recent, func(i, j int) bool { return m.recent[i].at.After(m.recent[j].at) })
}

// persistLatest writes the Latest list to the sidebar state.
func (m *Model) persistLatest() {
	if err := m.SaveState(); err != nil {
		m.setError(err)
	}
}
