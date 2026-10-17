package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
	"github.com/kyokomi/emoji/v2"
)

type emojisMsg struct{ values map[string]string }

func (m Model) fetchEmojis() tea.Cmd {
	api, ok := m.api.(slack.EmojiAPI)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		values, err := api.Emojis(m.ctx)
		if err != nil {
			values = nil
		}
		return emojisMsg{values}
	}
}

// Code and links retain their literal contents. An unfinished code span also
// remains literal rather than changing a command someone is trying to copy.
var emojiToken = regexp.MustCompile("(?s)```.*?(?:```|$)|`[^`\\n]*(?:`|$)|<[^>\\n]+>|https?://[^\\s<>]+|:[A-Za-z0-9_+.-]+:(?::skin-tone-[2-6]:)?")

func (m Model) emojify(text string) string {
	return emojiToken.ReplaceAllStringFunc(text, func(token string) string {
		if token[0] != ':' {
			return token
		}
		return m.emojiGlyph(token[1 : len(token)-1])
	})
}

func (m Model) emojiGlyph(name string) string {
	original := ":" + cleanLine(name) + ":"
	if base, tone, ok := strings.Cut(name, "::skin-tone-"); ok && len(tone) == 1 && tone[0] >= '2' && tone[0] <= '6' {
		glyph := m.emojiGlyph(base)
		if strings.HasPrefix(glyph, ":") {
			return original
		}
		_, size := utf8.DecodeRuneInString(glyph)
		return glyph[:size] + string(rune(0x1f3fb)+rune(tone[0]-'2')) + strings.TrimPrefix(glyph[size:], "\ufe0f")
	}
	for range 16 {
		// A workspace emoji can override a standard name. Image-backed custom
		// emoji stay readable until an image renderer is available.
		if value, ok := m.emojis[name]; ok {
			if !strings.HasPrefix(value, "alias:") {
				return original
			}
			name = strings.TrimPrefix(value, "alias:")
			continue
		}
		if glyph := emoji.CodeMap()[":"+name+":"]; glyph != "" {
			return glyph
		}
		// Slack uses :flag-us:; Unicode alias tables generally use :us:.
		if strings.HasPrefix(name, "flag-") {
			if glyph := emoji.CodeMap()[":"+strings.TrimPrefix(name, "flag-")+":"]; glyph != "" {
				return glyph
			}
		}
		return original
	}
	return original // Alias cycles never hang the UI.
}

func (m Model) reactionBadges(reactions []slack.Reaction, width int) string {
	var badges []string
	for _, reaction := range reactions {
		if reaction.Count <= 0 {
			continue
		}
		label := fmt.Sprintf(" %s %d ", m.emojiGlyph(reaction.Name), reaction.Count)
		style := lipgloss.NewStyle().Foreground(m.muted())
		if slices.Contains(reaction.Users, m.session.UserID) {
			label = "✓" + label
			style = style.Foreground(m.accent()).Bold(true)
		}
		badges = append(badges, style.Render(label))
	}
	return ansi.Hardwrap(strings.Join(badges, " "), width, true)
}

type reactionRequest struct {
	channel, ts string
	version     int
}
type reactionSnapshot struct {
	version int
	values  []slack.Reaction
}
type reactionsMsg struct {
	request reactionRequest
	values  []slack.Reaction
	err     error
}

// Events invalidate counts; Users can be incomplete, so never derive totals by
// adding/subtracting membership. Only one authoritative refresh runs at a time.
func (m *Model) updateReaction(event slack.LiveEvent) tea.Cmd {
	r := event.Reaction
	if r == nil || r.Channel == "" || r.TS == "" {
		return nil
	}
	relevant := m.active == r.Channel
	for _, thread := range m.feed {
		relevant = relevant || (thread.Root.Channel == r.Channel && thread.Root.TS == r.TS)
	}
	if !relevant {
		return nil
	}
	if r.EventTS != "" && !m.rememberLive("reaction/"+event.Type+"/"+r.Channel+"/"+r.TS+"/"+r.User+"/"+r.Name+"/"+r.EventTS) {
		return nil
	}
	m.eventVersion++
	key := r.Channel + "/" + r.TS
	if _, queued := m.reactionQueue[key]; !queued && len(m.reactionQueue) >= 128 {
		return nil
	}
	m.reactionQueue[key] = reactionRequest{r.Channel, r.TS, m.eventVersion}
	return m.nextReaction()
}
func (m *Model) nextReaction() tea.Cmd {
	api, ok := m.api.(slack.ReactionAPI)
	if !ok || m.reactionLoading != "" || !m.canRequest() {
		return nil
	}
	for key, request := range m.reactionQueue {
		m.reactionLoading = key
		return func() tea.Msg {
			values, err := api.Reactions(m.ctx, request.channel, request.ts)
			return reactionsMsg{request, values, err}
		}
	}
	return nil
}
func (m *Model) applyReactions(msg reactionsMsg) tea.Cmd {
	key := msg.request.channel + "/" + msg.request.ts
	m.reactionLoading = ""
	latest := m.reactionQueue[key]
	if msg.err != nil {
		delete(m.reactionQueue, key)
		m.setError(msg.err)
		return m.nextReaction()
	}
	if latest.version != msg.request.version {
		return m.nextReaction()
	}
	delete(m.reactionQueue, key)
	if len(m.reactionSnapshots) >= 128 {
		oldest := ""
		for id, snapshot := range m.reactionSnapshots {
			if oldest == "" || snapshot.version < m.reactionSnapshots[oldest].version {
				oldest = id
			}
		}
		delete(m.reactionSnapshots, oldest)
	}
	m.reactionSnapshots[key] = reactionSnapshot{msg.request.version, msg.values}
	apply := func(message *slack.Message) {
		if message.TS == msg.request.ts {
			message.Reactions = msg.values
		}
	}
	if m.active == msg.request.channel {
		for i := range m.history {
			apply(&m.history[i])
		}
		if m.thread != nil {
			for i := range m.thread.history {
				apply(&m.thread.history[i])
			}
		}
	}
	for i := range m.feed {
		if m.feed[i].Root.Channel != msg.request.channel {
			continue
		}
		apply(&m.feed[i].Root)
		for j := range m.feed[i].LatestReplies {
			apply(&m.feed[i].LatestReplies[j])
		}
		for j := range m.feed[i].UnreadReplies {
			apply(&m.feed[i].UnreadReplies[j])
		}
	}
	m.renderHistory()
	return m.nextReaction()
}
func (m Model) withReactionSnapshot(message slack.Message, channel string, version int) slack.Message {
	if snapshot, ok := m.reactionSnapshots[channel+"/"+message.TS]; ok && snapshot.version > version {
		message.Reactions = snapshot.values
	}
	return message
}
