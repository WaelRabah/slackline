package tui

import (
	"context"
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
	unreadsChannels = 20    // channels summarized per run, most unread first
	unreadBudget    = 40000 // bytes of transcript per channel
)

// unreadSection is one channel block. Blocks stay until they are marked read;
// background changes to unread state never remove them.
type unreadSection struct {
	channel, label string
	count          int
	text, err      string
	partial        bool
	after, latest  string
}

// markedBlockMsg reports a finished mark-as-read for one channel block.
type markedBlockMsg struct {
	channel, latest string
	threads         []string
	err             error
}

// unreadTarget is one channel with unread messages, ready to summarize or mark read.
type unreadTarget struct {
	id, label, after, latest string
	count                    int
	threadReplies            []slack.Message
}

// unreadTargets lists channels that currently have unread state, most unread first.
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

// unreadItems is what the view shows: pinned blocks in their stored order, then
// channels that became unread since the last run.
func (m Model) unreadItems() []unreadTarget {
	live := map[string]unreadTarget{}
	for _, target := range m.unreadTargets() {
		live[target.id] = target
	}
	var items []unreadTarget
	seen := map[string]bool{}
	for _, section := range m.unreadSections {
		if seen[section.channel] {
			continue
		}
		seen[section.channel] = true
		item := unreadTarget{id: section.channel, label: section.label, after: section.after, latest: section.latest, count: max(1, section.count)}
		if fresh, ok := live[section.channel]; ok {
			item = fresh
			item.latest = laterTS(fresh.latest, section.latest)
		}
		items = append(items, item)
	}
	for _, target := range m.unreadTargets() {
		if !seen[target.id] {
			items = append(items, target)
		}
	}
	return items
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
	if sig == "" || sig == m.unreadSig {
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

// mergeUnreadMessages joins channel-level and thread unreads, oldest first, without duplicates.
func mergeUnreadMessages(channel, threads []slack.Message) []slack.Message {
	seen := map[string]bool{}
	var out []slack.Message
	for _, list := range [][]slack.Message{channel, threads} {
		for _, message := range list {
			if seen[message.TS] || message.DisplayText() == "" {
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

// senderName picks the most useful name for a message, including bot posts.
func senderName(message slack.Message, names map[string]string) string {
	if name := names[message.User]; name != "" {
		return name
	}
	if message.Username != "" {
		return message.Username
	}
	if message.User != "" {
		return message.User
	}
	if message.BotID != "" {
		return "bot " + message.BotID
	}
	return "unknown sender"
}

// buildUnreadTranscript formats one channel's unreads with source links, within budget.
func buildUnreadTranscript(team string, names map[string]string, channel string, messages []slack.Message) (string, bool) {
	var out strings.Builder
	for _, message := range messages {
		line := fmt.Sprintf("[%s] %s %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(senderName(message, names)), messageSourceURL(team, channel, message.TS), clean(message.DisplayText()))
		if out.Len()+len(line) > unreadBudget {
			return out.String(), true
		}
		out.WriteString(line)
	}
	return out.String(), false
}

// mergeSections updates blocks that exist and appends new ones, keeping order.
func mergeSections(existing, fresh []unreadSection) []unreadSection {
	out := append([]unreadSection{}, existing...)
	index := map[string]int{}
	for i, section := range out {
		index[section.channel] = i
	}
	for _, section := range fresh {
		if i, ok := index[section.channel]; ok {
			out[i] = section
			continue
		}
		index[section.channel] = len(out)
		out = append(out, section)
	}
	return out
}

// unreadsKey handles the keys of the Unreads Summaries view. It reports whether
// the key was used here, so other keys keep their normal meaning.
func (m *Model) unreadsKey(key string) (tea.Cmd, bool) {
	items := m.unreadItems()
	switch key {
	case "n", "j", "down":
		if len(items) > 0 {
			m.unreadIndex = (m.unreadIndex + 1) % len(items)
			m.renderDestination()
		}
		return nil, true
	case "p", "k", "up":
		if len(items) > 0 {
			m.unreadIndex = (m.unreadIndex + len(items) - 1) % len(items)
			m.renderDestination()
		}
		return nil, true
	case "x":
		return m.markBlockRead(items), true
	}
	return nil, false
}

// markBlockRead marks the shown block's channel and its threads as read in Slack.
// The block leaves the view only after Slack confirms.
func (m *Model) markBlockRead(items []unreadTarget) tea.Cmd {
	if len(items) == 0 {
		return nil
	}
	index := min(m.unreadIndex, len(items)-1)
	target := items[index]
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

// dropUnreadBlock removes a channel block and its unread state, so it does not
// come back on refresh.
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
	if items := m.unreadItems(); len(items) > 0 {
		m.unreadIndex = min(m.unreadIndex, len(items)-1)
	} else {
		m.unreadIndex = 0
	}
}

// unreadsView renders every block, each in its own box.
func (m Model) unreadsView() string {
	view, _ := m.unreadsLayout()
	return view
}

// unreadsLayout stacks one box per unread block. It also returns the first line of
// the selected block, so the view can scroll to it.
func (m Model) unreadsLayout() (string, int) {
	width := max(1, m.viewport.Width())
	muted := lipgloss.NewStyle().Foreground(m.muted())
	bold := lipgloss.NewStyle().Bold(true)
	items := m.unreadItems()
	sections := map[string]unreadSection{}
	for _, section := range m.unreadSections {
		sections[section.channel] = section
	}
	var b strings.Builder
	b.WriteString(bold.Render(fmt.Sprintf("Unreads Summaries · %d channels", len(items))) + "\n")
	switch {
	case m.demo:
		b.WriteString(muted.Render("Unread summaries are offline in the demo.") + "\n")
	case !m.provider.Ready():
		b.WriteString(muted.Render("Showing counts only. Enable AI in Settings (,) for categorized summaries.") + "\n")
	case m.unreadBusy:
		b.WriteString(muted.Render("Summarizing in the background; blocks fill in as they finish.") + "\n")
	case !m.unreadStamp.IsZero():
		b.WriteString(muted.Render("Summarized "+m.unreadStamp.Local().Format("Jan 02 15:04")) + "\n")
	}
	if len(items) == 0 {
		b.WriteString("\nAll caught up. No unread messages.")
		return ansi.Hardwrap(b.String(), width, true), 0
	}
	index := min(m.unreadIndex, len(items)-1)
	selectedLine := 0
	for i, target := range items {
		if i == index {
			selectedLine = strings.Count(b.String(), "\n")
		}
		section, ok := sections[target.id]
		b.WriteString("\n" + m.unreadBox(target, section, ok, i == index, width) + "\n")
	}
	b.WriteString("\n" + muted.Render("n/p next/previous block · x mark read · s refresh"))
	return b.String(), selectedLine
}

// unreadBox draws one block in its own box: channel and newest time on the top
// border, the unread count, then the summary or its status.
func (m Model) unreadBox(target unreadTarget, section unreadSection, summarized, selected bool, width int) string {
	inner := max(1, width-4)
	muted := lipgloss.NewStyle().Foreground(m.muted())
	accent := lipgloss.NewStyle().Bold(true).Foreground(m.accent())
	stamp := ""
	if at := slack.TimestampTime(target.latest); !at.IsZero() {
		stamp = at.Local().Format("Jan 02 15:04")
	}
	title := target.label
	if selected {
		title = "► " + title
	}
	left := ansi.Truncate(title, max(1, inner-lipgloss.Width(stamp)-1), "…")
	header := accent.Render(left)
	if gap := inner - lipgloss.Width(left) - lipgloss.Width(stamp); gap > 0 {
		header += strings.Repeat(" ", gap) + muted.Render(stamp)
	}
	var body strings.Builder
	body.WriteString(muted.Render(fmt.Sprintf("%d unread", target.count)) + "\n")
	switch {
	case m.markingUnread[target.id]:
		body.WriteString(muted.Render("Marking this block read…"))
	case !summarized && m.unreadBusy:
		body.WriteString(muted.Render("Waiting for summary…"))
	case !summarized:
		body.WriteString(muted.Render("Not summarized yet. Press s to summarize."))
	case section.err != "":
		body.WriteString("Could not summarize: " + section.err)
	case section.text == "":
		body.WriteString(muted.Render("No readable messages to categorize"))
	default:
		body.WriteString(renderMarkdown(section.text, inner))
	}
	if summarized && section.partial {
		body.WriteString("\n" + muted.Render("Partial: some unread messages were not read."))
	}
	border := m.muted()
	if selected {
		border = m.accent()
	}
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(header + "\n" + body.String())
}
