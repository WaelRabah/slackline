package tui

import (
	"context"
	"errors"
	"fmt"
	"github.com/WaelRabah/slackline/internal/logs"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
)

// Background jobs run private summaries and unread-block categorization off the
// UI loop. A few run at once; each reports back exactly once, even on failure, so
// nothing stays in "Gathering" or "Waiting for summary" indefinitely.
const (
	bgWorkers        = 3
	bgSummaryTimeout = 5 * time.Minute
	bgUnreadTimeout  = 4 * time.Minute
	// A failed job is not retried automatically until this passes, so a provider
	// outage or Slack rate limit cannot turn every refresh into another request.
	bgFailureBackoff = 10 * time.Minute
	bgMinRateBackoff = time.Minute
)

// backoffFor returns how long automatic runs wait after err. Slack asks for a
// specific delay on rate limits; everything else waits the standard interval.
func backoffFor(err error) time.Duration {
	var limited *slack.RateLimitError
	if errors.As(err, &limited) {
		return max(limited.RetryAfter, bgMinRateBackoff)
	}
	return bgFailureBackoff
}

// inBackoff reports whether automatic runs must skip this job after a recent failure.
// Manual requests (the s key) ignore it.
func (m Model) inBackoff(kind bgKind, channel string) bool {
	return time.Now().Before(m.bgRetryAt[jobKey(kind, channel)])
}

type bgKind int

const (
	bgSummary bgKind = iota
	bgUnread
)

// bgJob is one piece of background work for one conversation.
type bgJob struct {
	kind                   bgKind
	channel, label         string
	after, latest, covered string
	threads                []slack.Message
	names                  map[string]string
	team, self, sig        string
}

// bgResult is the single report a job sends back to the UI loop.
type bgResult struct {
	job     bgJob
	summary conversationSummary
	section unreadSection
	err     error
}

func jobKey(kind bgKind, channel string) string { return fmt.Sprintf("%d/%s", kind, channel) }

// enqueue adds a job unless the same job is already queued or running. It
// reports whether the job was added.
func (m *Model) enqueue(job bgJob) bool {
	if m.bgActive == nil {
		m.bgActive = map[string]bool{}
	}
	key := jobKey(job.kind, job.channel)
	if m.bgActive[key] {
		return false
	}
	m.bgActive[key] = true
	m.bgQueue = append(m.bgQueue, job)
	if job.kind == bgSummary {
		if m.summaryBusy == nil {
			m.summaryBusy = map[string]bool{}
		}
		m.summaryBusy[job.channel] = true
	}
	return true
}

// pump starts queued jobs while there is a free worker.
func (m *Model) pump() tea.Cmd {
	var cmds []tea.Cmd
	for m.bgRunning < bgWorkers && len(m.bgQueue) > 0 {
		job := m.bgQueue[0]
		m.bgQueue = m.bgQueue[1:]
		m.bgRunning++
		cmds = append(cmds, m.runJob(job))
	}
	return tea.Batch(cmds...)
}

func (m *Model) runJob(job bgJob) tea.Cmd {
	ctx, provider := m.ctx, m.provider
	switch job.kind {
	case bgSummary:
		api, ok := m.api.(slack.ContextAPI)
		if !ok {
			return func() tea.Msg {
				return bgResult{job: job, err: fmt.Errorf("conversation context is unavailable for this backend")}
			}
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, bgSummaryTimeout)
			defer cancel()
			summary, err := summarizeChannel(ctx, api, provider, job, time.Now())
			return bgResult{job: job, summary: summary, err: err}
		}
	default:
		api, ok := m.api.(slack.UnreadsAPI)
		if !ok {
			return func() tea.Msg {
				return bgResult{job: job, err: fmt.Errorf("unread messages are unavailable for this backend")}
			}
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, bgUnreadTimeout)
			defer cancel()
			section := categorizeChannel(ctx, api, provider, job)
			return bgResult{job: job, section: section}
		}
	}
}

// summarizeChannel reads the last 14 days of one conversation and asks the model
// for the private summary. Every path returns, so the job always reports.
func summarizeChannel(ctx context.Context, api slack.ContextAPI, provider inference.Settings, job bgJob, until time.Time) (conversationSummary, error) {
	since := until.Add(-summaryWindow)
	page, err := api.ConversationContext(ctx, job.channel, since, until)
	if err != nil {
		return conversationSummary{}, err
	}
	sort.Slice(page.Messages, func(i, j int) bool {
		return slack.TimestampTime(page.Messages[i].TS).After(slack.TimestampTime(page.Messages[j].TS))
	})
	lines := []string{}
	size, count := 0, 0
	partial := page.Partial
	for _, message := range page.Messages {
		name := job.names[message.User]
		if name == "" {
			name = message.User
		}
		if name == "" {
			name = message.Username
		}
		line := fmt.Sprintf("[%s] %s (thread %s) %s\n%s\n", slack.TimestampTime(message.TS).UTC().Format(time.RFC3339), cleanLine(name), message.ThreadTS, messageSourceURL(job.team, job.channel, message.TS), clean(message.Text))
		if size+len(line) > 100000 {
			partial = true
			continue
		}
		lines = append(lines, line)
		size += len(line)
		count++
	}
	summary := conversationSummary{Generated: until, Since: since, Messages: count, Partial: partial, Newest: job.covered}
	if count == 0 {
		summary.Text = "No messages found in this conversation in the last 14 days. Slack retention may limit coverage."
		return summary, nil
	}
	// Restore chronological order after selecting the newest bounded context.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	transcript := fmt.Sprintf("Conversation: %s\nWindow: %s through %s\nMessages: %d. Partial coverage: %t. Slack retention may limit results.\n\n", cleanLine(job.label), since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339), count, partial) + strings.Join(lines, "\n")
	text, err := inference.Summarize(ctx, provider, transcript)
	if err != nil {
		return conversationSummary{}, err
	}
	summary.Text = clean(text)
	return summary, nil
}

// categorizeChannel sorts one channel's unread messages into the Unreads blocks.
func categorizeChannel(ctx context.Context, api slack.UnreadsAPI, provider inference.Settings, job bgJob) unreadSection {
	section := unreadSection{channel: job.channel, label: job.label, after: job.after, latest: job.latest}
	page, err := api.UnreadMessages(ctx, job.channel, job.after, job.latest, job.self)
	if err != nil {
		section.err = cleanLine(err.Error())
		return section
	}
	messages := mergeUnreadMessages(page.Messages, job.threads)
	section.count = len(messages)
	section.partial = page.Partial
	if len(messages) == 0 {
		return section
	}
	transcript, trimmed := buildUnreadTranscript(job.team, job.names, job.channel, messages)
	section.partial = section.partial || trimmed
	text, err := inference.Categorize(ctx, provider, job.label, transcript, section.partial)
	if err != nil {
		section.err = cleanLine(err.Error())
		return section
	}
	section.text = clean(text)
	return section
}

// applyBG stores one finished job and starts whatever is waiting.
func (m *Model) applyBG(res bgResult) tea.Cmd {
	m.bgRunning = max(0, m.bgRunning-1)
	delete(m.bgActive, jobKey(res.job.kind, res.job.channel))
	if res.err != nil || (res.job.kind == bgUnread && res.section.err != "") {
		if m.bgRetryAt == nil {
			m.bgRetryAt = map[string]time.Time{}
		}
		wait := bgFailureBackoff
		if res.err != nil {
			wait = backoffFor(res.err)
		}
		m.bgRetryAt[jobKey(res.job.kind, res.job.channel)] = time.Now().Add(wait)
	}
	var done tea.Cmd
	switch res.job.kind {
	case bgSummary:
		delete(m.summaryBusy, res.job.channel)
		if res.err != nil {
			logs.Error("summary", res.err)
			m.status = "Private summary failed for " + res.job.label + ": " + cleanLine(res.err.Error())
			break
		}
		res.summary.Digest = summaryDigest(res.summary)
		m.summaries[res.job.channel] = res.summary
		m.pruneSummaries()
		if err := m.SaveState(); err != nil {
			m.setError(err)
		}
		if res.job.channel == m.active {
			m.resize()
		}
	case bgUnread:
		m.unreadSections = mergeSections(m.unreadSections, []unreadSection{res.section})
		if m.unreadPending > 0 {
			m.unreadPending--
		}
		if res.err != nil || res.section.err != "" {
			logs.Error("unreads", fmt.Errorf("%s: %v%s", res.job.label, res.err, res.section.err))
		}
		if res.err == nil && res.section.err == "" {
			m.unreadOK = true
		}
		if m.unreadPending == 0 {
			m.unreadBusy = false
			m.unreadStamp = time.Now()
			m.unreadSig = m.unreadSignature()
			if m.unreadOK {
				m.noteSummaryReady("Unreads Summaries ready", m.now())
				done = m.summaryChime(true)
			}
			m.unreadOK = false
			m.status = "Unread summaries are ready"
		}
		if m.destination == unreadsDestination {
			m.renderDestination()
		}
	}
	if m.destination == "" && m.active != "" && res.job.channel == m.active && res.job.kind == bgSummary {
		m.resize()
	}
	return tea.Batch(done, m.pump())
}

// pruneSummaries keeps the newest 128 private summaries.
func (m *Model) pruneSummaries() {
	for len(m.summaries) > 128 {
		oldest := ""
		for id, summary := range m.summaries {
			if oldest == "" || summary.Generated.Before(m.summaries[oldest].Generated) {
				oldest = id
			}
		}
		delete(m.summaries, oldest)
	}
}

// summaryJob builds a private-summary job for one conversation.
func (m *Model) summaryJob(channel string) bgJob {
	return bgJob{
		kind:    bgSummary,
		channel: channel,
		label:   m.conversationLabel(m.conversationFor(channel)),
		covered: m.latestSeen[channel],
		names:   m.nameSnapshot(),
		team:    m.session.TeamID,
	}
}

// unreadJob builds an unread-block job for one channel.
func (m *Model) unreadJob(target unreadTarget, sig string) bgJob {
	return bgJob{
		kind:    bgUnread,
		channel: target.id,
		label:   target.label,
		after:   target.after,
		latest:  target.latest,
		threads: target.threadReplies,
		names:   m.nameSnapshot(),
		team:    m.session.TeamID,
		self:    m.session.UserID,
		sig:     sig,
	}
}

func (m Model) nameSnapshot() map[string]string {
	names := make(map[string]string, len(m.users))
	for id, name := range m.users {
		names[id] = name
	}
	return names
}

// queueSummary queues a private summary now, even if one exists; the caller
// decides whether it is needed.
func (m *Model) queueSummary(channel string) tea.Cmd {
	if !m.enqueue(m.summaryJob(channel)) {
		return nil
	}
	m.status = "Summarizing the last 14 days in the background…"
	m.resize()
	return m.pump()
}

// runUnreads queues one job per unread channel. Blocks fill in as each finishes.
func (m *Model) runUnreads(sig string) tea.Cmd {
	queued := 0
	for _, target := range m.unreadItems() {
		if m.enqueue(m.unreadJob(target, sig)) {
			queued++
		}
	}
	if queued == 0 {
		return nil
	}
	m.unreadPending += queued
	m.unreadBusy = true
	m.status = fmt.Sprintf("Summarizing %d unread channels in the background", queued)
	m.renderDestination()
	return m.pump()
}

// backgroundRefresh keeps summaries current. It queues a block or summary only
// where the newest message is newer than what the stored work covers. Stored
// summaries are checked on every refresh, so a restart reruns the ones that have
// new messages.
func (m *Model) backgroundRefresh() tea.Cmd {
	if m.demo || !m.provider.Ready() {
		return nil
	}
	if m.latestSeen == nil {
		m.latestSeen = map[string]string{}
	}
	sig := m.unreadSignature()
	unread, queued := 0, 0
	for _, target := range m.unreadItems() {
		m.latestSeen[target.id] = laterTS(m.latestSeen[target.id], target.latest)
		if !m.blockCurrent(target) && !m.inBackoff(bgUnread, target.id) && m.enqueue(m.unreadJob(target, sig)) {
			unread++
		}
		if m.summaryStale(target.id) && !m.inBackoff(bgSummary, target.id) && m.enqueue(m.summaryJob(target.id)) {
			queued++
		}
	}
	for channel := range m.summaries {
		if m.summaryStale(channel) && !m.inBackoff(bgSummary, channel) && m.enqueue(m.summaryJob(channel)) {
			queued++
		}
	}
	if unread > 0 {
		m.unreadPending += unread
		m.unreadBusy = true
	}
	if unread+queued == 0 {
		return nil
	}
	return m.pump()
}

// blockCurrent reports whether the summary block already covers the newest unread message.
func (m Model) blockCurrent(target unreadTarget) bool {
	for _, section := range m.unreadSections {
		if section.channel == target.id {
			return !slack.TimestampTime(target.latest).After(slack.TimestampTime(section.latest))
		}
	}
	return false
}
