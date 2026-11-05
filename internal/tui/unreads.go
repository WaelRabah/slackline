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
)

const (
	unreadsChannels = 20    // channels summarized per run, most unread first
	unreadBudget    = 40000 // bytes of transcript per channel
)

// unreadSection is one channel's categorized unreads.
type unreadSection struct {
	channel, label string
	count          int
	text, err      string
	partial        bool
}

type unreadsMsg struct {
	signature string
	sections  []unreadSection
	generated time.Time
}

// markedBlockMsg reports a finished mark-as-read for one channel block.
type markedBlockMsg struct {
	channel, latest string
	threads         []string
	err             error
}

// unreadTarget is one channel with unread messages, ordered for display.
type unreadTarget struct {
	id, label, after, latest string
	count                    int
	threadReplies            []slack.Message
}

// unreadTargets lists channels with unread messages, most unread first.
func (m Model) unreadTargets() []unreadTarget {
	names := map[string]string{}
	for id, name := range m.users {
		names[id] = name
	}
	var targets []unreadTarget
	for _, conversation := range m.conversations {
		state, ok := m.unread[conversation.ID]
		if !ok {
			continue
		}
		label := conversation.Name
		if conversation.IsIM {
			label = "DM with " + names[conversation.User]
		}
		target := unreadTarget{id: conversation.ID, label: label, after: state.after, latest: state.latest, count: max(1, state.count)}
		for _, thread := range m.feed {
			if thread.Root.Channel == conversation.ID {
				target.threadReplies = append(target.threadReplies, thread.UnreadReplies...)
			}
		}
		targets = append(targets, target)
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].count > targets[j].count })
	return targets
}

// unreadSignature changes whenever the set of unread channels or their newest messages changes.
func (m Model) unreadSignature() string {
	parts := make([]string, 0, len(m.unread))
	for id, state := range m.unread {
		parts = append(parts, id+"="+state.latest)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// autoUnreads runs when the view opens. It reruns only if the unreads changed.
func (m *Model) autoUnreads() tea.Cmd {
	if m.demo || !m.provider.Ready() || m.unreadBusy {
		return nil
	}
	sig := m.unreadSignature()
	if sig == "" {
		m.unreadSections = nil
		m.unreadSig = ""
		return nil
	}
	if sig == m.unreadSig {
		return nil
	}
	return m.runUnreads(sig)
}

// refreshUnreads is the manual s key: it always regenerates.
func (m *Model) refreshUnreads() tea.Cmd {
	if m.demo {
		m.status = "Unread summaries are offline in the demo"
		return nil
	}
	if !m.provider.Ready() {
		m.status = "Enable AI and set a chat model in Settings (,) to summarize unreads"
		return m.openSettings()
	}
	if m.unreadBusy {
		m.status = "Already summarizing unreads"
		return nil
	}
	return m.runUnreads(m.unreadSignature())
}

func (m *Model) runUnreads(sig string) tea.Cmd {
	api, ok := m.api.(slack.UnreadsAPI)
	if !ok {
		m.status = "Unread messages are unavailable for this backend"
		return nil
	}
	targets := m.unreadTargets()
	if len(targets) == 0 {
		m.unreadSections = nil
		m.unreadSig = ""
		return nil
	}
	if len(targets) > unreadsChannels {
		targets = targets[:unreadsChannels]
	}
	ctx, provider, team, self := m.ctx, m.provider, m.session.TeamID, m.session.UserID
	names := map[string]string{}
	for id, name := range m.users {
		names[id] = name
	}
	m.unreadBusy = true
	m.status = fmt.Sprintf("Summarizing unreads in %d channels…", len(targets))
	m.renderDestination()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		var sections []unreadSection
		for _, target := range targets {
			section := unreadSection{channel: target.id, label: target.label}
			page, err := api.UnreadMessages(ctx, target.id, target.after, target.latest, self)
			if err != nil {
				section.err = cleanLine(err.Error())
				sections = append(sections, section)
				continue
			}
			messages := mergeUnreadMessages(page.Messages, target.threadReplies)
			section.count = len(messages)
			section.partial = page.Partial
			if len(messages) == 0 {
				sections = append(sections, section)
				continue
			}
			transcript, trimmed := buildUnreadTranscript(team, names, target.id, messages)
			section.partial = section.partial || trimmed
			text, err := inference.Categorize(ctx, provider, target.label, transcript, section.partial)
			if err != nil {
				section.err = cleanLine(err.Error())
			} else {
				section.text = clean(text)
			}
			sections = append(sections, section)
		}
		return unreadsMsg{signature: sig, sections: sections, generated: time.Now()}
	}
}

// mergeUnreadMessages joins channel-level and thread unreads, oldest first, without duplicates.
func mergeUnreadMessages(channel, threads []slack.Message) []slack.Message {
	seen := map[string]bool{}
	var out []slack.Message
	for _, list := range [][]slack.Message{channel, threads} {
		for _, message := range list {
			if seen[message.TS] || message.User == "" && message.Text == "" {
				continue
			}
			seen[message.TS] = true
			out = append(out, message)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return slack.TimestampTime(out[i].TS).Before(slack.TimestampTime(out[j].TS))
	})
	return out
}

// buildUnreadTranscript formats one channel's unreads with source links, within budget.
func buildUnreadTranscript(team string, names map[string]string, channel string, messages []slack.Message) (string, bool) {
	var out strings.Builder
	for _, message := range messages {
		name := names[message.User]
		if name == "" {
			name = message.User
		}
		line := fmt.Sprintf("[%s] %s %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(name), messageSourceURL(team, channel, message.TS), clean(message.Text))
		if out.Len()+len(line) > unreadBudget {
			return out.String(), true
		}
		out.WriteString(line)
	}
	return out.String(), false
}

// applyUnreads stores a finished run. A failed section stays visible on its channel.
func (m *Model) applyUnreads(msg unreadsMsg) tea.Cmd {
	m.unreadBusy = false
	m.unreadSections = msg.sections
	m.unreadSig = msg.signature
	m.unreadStamp = msg.generated
	failed := 0
	for _, section := range msg.sections {
		if section.err != "" {
			failed++
		}
	}
	if failed > 0 {
		m.status = fmt.Sprintf("Unreads summarized with %d failed channel(s)", failed)
	} else {
		m.status = "Unreads summarized by channel"
	}
	if m.destination == unreadsDestination {
		m.renderDestination()
	}
	return nil
}

// unreadsKey handles the keys of the Unreads Summaries view. It reports whether
// the key was used here, so other keys keep their normal meaning.
func (m *Model) unreadsKey(key string) (tea.Cmd, bool) {
	targets := m.unreadTargets()
	switch key {
	case "n", "j", "down":
		if len(targets) > 0 {
			m.unreadIndex = (m.unreadIndex + 1) % len(targets)
			m.renderDestination()
		}
		return nil, true
	case "p", "k", "up":
		if len(targets) > 0 {
			m.unreadIndex = (m.unreadIndex + len(targets) - 1) % len(targets)
			m.renderDestination()
		}
		return nil, true
	case "x":
		return m.markBlockRead(targets), true
	}
	return nil, false
}

// markBlockRead marks the shown block's channel and its threads as read in Slack.
// The block leaves the view only after Slack confirms.
func (m *Model) markBlockRead(targets []unreadTarget) tea.Cmd {
	if len(targets) == 0 {
		return nil
	}
	index := min(m.unreadIndex, len(targets)-1)
	target := targets[index]
	if m.markingUnread == nil {
		m.markingUnread = map[string]bool{}
	}
	if m.markingUnread[target.id] {
		m.status = "Already marking this block"
		return nil
	}
	threads := m.unreadThreadRoots(target.id)
	if m.demo {
		m.dropUnreadBlock(markedBlockMsg{channel: target.id, latest: target.latest})
		m.status = "Marked read (demo, local only)"
		m.renderDestination()
		return nil
	}
	api, ok := m.api.(slack.ReadAPI)
	if !ok {
		m.status = "Marking read is unavailable for this backend"
		return nil
	}
	m.markingUnread[target.id] = true
	m.status = "Marking read…"
	ctx, channel, latest := m.ctx, target.id, target.latest
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		err := api.MarkRead(ctx, channel, "", latest)
		for _, thread := range threads {
			if err != nil {
				break
			}
			err = api.MarkRead(ctx, channel, thread.root, thread.latest)
		}
		roots := make([]string, 0, len(threads))
		for _, thread := range threads {
			roots = append(roots, thread.root)
		}
		return markedBlockMsg{channel: channel, latest: latest, threads: roots, err: err}
	}
}

type unreadThreadMark struct{ root, latest string }

// unreadThreadRoots lists followed threads in channel that have unread replies.
func (m Model) unreadThreadRoots(channel string) []unreadThreadMark {
	var marks []unreadThreadMark
	for _, thread := range m.feed {
		if thread.Root.Channel == channel && len(thread.UnreadReplies) > 0 {
			marks = append(marks, unreadThreadMark{root: thread.Root.TS, latest: thread.Latest()})
		}
	}
	return marks
}

// applyMarkedBlock finishes a mark-as-read. A failure keeps the block in place.
func (m *Model) applyMarkedBlock(msg markedBlockMsg) {
	delete(m.markingUnread, msg.channel)
	if msg.err != nil {
		m.setError(msg.err)
		m.renderDestination()
		return
	}
	m.dropUnreadBlock(msg)
	m.status = "Marked read · block removed"
	m.renderDestination()
}

// dropUnreadBlock removes a channel from the unread state, its summary, and its
// thread unreads, so the block disappears and does not come back on refresh.
func (m *Model) dropUnreadBlock(msg markedBlockMsg) {
	delete(m.unread, msg.channel)
	m.remoteRead[msg.channel] = laterTS(m.remoteRead[msg.channel], msg.latest)
	m.clearReadHighlights(msg.channel, msg.latest)
	kept := m.unreadSections[:0]
	for _, section := range m.unreadSections {
		if section.channel != msg.channel {
			kept = append(kept, section)
		}
	}
	m.unreadSections = kept
	for i := range m.feed {
		if m.feed[i].Root.Channel == msg.channel {
			m.feed[i].UnreadReplies = nil
		}
	}
	// The stored summaries still match what is left, so the view does not rerun.
	m.unreadSig = m.unreadSignature()
	if targets := m.unreadTargets(); len(targets) > 0 {
		m.unreadIndex = min(m.unreadIndex, len(targets)-1)
	} else {
		m.unreadIndex = 0
	}
}

// unreadsView renders one block at a time: the channel, its categorized summary,
// and the keys that move between blocks or mark the block read.
func (m Model) unreadsView() string {
	width := max(1, m.viewport.Width()-4)
	muted := lipgloss.NewStyle().Foreground(m.muted())
	bold := lipgloss.NewStyle().Bold(true)
	targets := m.unreadTargets()
	sections := map[string]unreadSection{}
	for _, section := range m.unreadSections {
		sections[section.channel] = section
	}
	var b strings.Builder
	b.WriteString(bold.Render(fmt.Sprintf("Unreads Summaries · %d channels", len(targets))) + "\n")
	switch {
	case m.demo:
		b.WriteString(muted.Render("Unread summaries are offline in the demo.") + "\n")
	case !m.provider.Ready():
		b.WriteString(muted.Render("Showing counts only. Enable AI in Settings (,) for categorized summaries.") + "\n")
	case m.unreadBusy:
		b.WriteString(muted.Render("Summarizing… this can take a minute for many channels.") + "\n")
	case !m.unreadStamp.IsZero():
		b.WriteString(muted.Render("Summarized "+m.unreadStamp.Local().Format("Jan 02 15:04")) + "\n")
	}
	if len(targets) == 0 {
		b.WriteString("\nAll caught up. No unread messages.")
		return b.String()
	}
	index := min(m.unreadIndex, len(targets)-1)
	target := targets[index]
	b.WriteString("\n" + bold.Render("# "+target.label) + "\n")
	b.WriteString(muted.Render(fmt.Sprintf("Block %d of %d · %d unread", index+1, len(targets), target.count)) + "\n\n")
	section, ok := sections[target.id]
	switch {
	case m.markingUnread[target.id]:
		b.WriteString(muted.Render("Marking this block read…") + "\n")
	case !ok && m.unreadBusy:
		b.WriteString(muted.Render("Waiting for summary…") + "\n")
	case !ok:
		b.WriteString(muted.Render("Not summarized yet. Press s to summarize.") + "\n")
	case section.err != "":
		b.WriteString("Could not summarize: " + section.err + "\n")
	case section.text == "":
		b.WriteString(muted.Render("No messages to categorize") + "\n")
	default:
		b.WriteString(renderMarkdown(section.text, width) + "\n")
	}
	if ok && section.partial {
		b.WriteString("\n" + muted.Render("Partial: some unread messages were not read.") + "\n")
	}
	b.WriteString("\n" + muted.Render("n/p next/previous block · x mark read · s refresh"))
	return b.String()
}
