package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/charmbracelet/x/ansi"
)

const catchUpBudget = 150000 // bytes of unread transcript per catch-up

// catchUpState is the AI Summaries catch-up. It is regenerated when unread
// messages change, and by the manual s key.
type catchUpState struct {
	text, err string
	stamp     time.Time
	sig       string
	partial   bool
	busy      bool
}

type catchUpMsg struct {
	signature string
	text      string
	partial   bool
	err       error
	generated time.Time
}

// autoCatchUp runs when the view opens and reruns only if unread messages changed.
func (m *Model) autoCatchUp() tea.Cmd {
	if m.demo || !m.provider.Ready() || m.catchUp.busy {
		return nil
	}
	sig := m.unreadSignature()
	if sig == m.catchUp.sig {
		return nil
	}
	if sig == "" {
		m.catchUp = catchUpState{}
		return nil
	}
	return m.runCatchUp(sig)
}

// refreshCatchUp is the manual s key on the AI Summaries view.
func (m *Model) refreshCatchUp() tea.Cmd {
	if m.demo {
		m.status = "The catch-up is offline in the demo"
		return nil
	}
	if !m.provider.Ready() {
		m.status = "Enable AI and set a chat model in Settings (,) for the catch-up"
		return m.openSettings()
	}
	if m.catchUp.busy {
		m.status = "Already writing the catch-up"
		return nil
	}
	sig := m.unreadSignature()
	if sig == "" {
		m.catchUp = catchUpState{}
		m.status = "Nothing unread to catch up on"
		m.renderDestination()
		return nil
	}
	return m.runCatchUp(sig)
}

func (m *Model) runCatchUp(sig string) tea.Cmd {
	api, ok := m.api.(slack.UnreadsAPI)
	if !ok {
		m.status = "Unread messages are unavailable for this backend"
		return nil
	}
	items := m.unreadItems()
	if len(items) > unreadsChannels {
		items = items[:unreadsChannels]
	}
	ctx, provider, team, self := m.ctx, m.provider, m.session.TeamID, m.session.UserID
	names := map[string]string{}
	for id, name := range m.users {
		names[id] = name
	}
	m.catchUp.busy = true
	m.status = fmt.Sprintf("Writing your catch-up from %d conversations…", len(items))
	m.renderDestination()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		var out strings.Builder
		partial := false
		for _, item := range items {
			page, err := api.UnreadMessages(ctx, item.id, item.after, item.latest, self)
			if err != nil {
				partial = true
				continue
			}
			partial = partial || page.Partial
			messages := mergeUnreadMessages(page.Messages, item.threadReplies)
			if len(messages) == 0 {
				continue
			}
			header := fmt.Sprintf("\n=== %s (%d unread) ===\n", cleanLine(item.label), len(messages))
			transcript, trimmed := buildUnreadTranscript(team, names, item.id, messages)
			if out.Len()+len(header)+len(transcript) > catchUpBudget {
				partial = true
				break
			}
			out.WriteString(header)
			out.WriteString(transcript)
			partial = partial || trimmed
		}
		if out.Len() == 0 {
			return catchUpMsg{signature: sig, generated: time.Now(), partial: partial}
		}
		text, err := inference.CatchUp(ctx, provider, out.String(), partial)
		return catchUpMsg{signature: sig, text: clean(text), partial: partial, err: err, generated: time.Now()}
	}
}

// applyCatchUp stores a finished catch-up. A failure is kept as the error and
// retried the next time the view opens, not in a loop.
func (m *Model) applyCatchUp(msg catchUpMsg) {
	m.catchUp.busy = false
	if msg.err != nil {
		m.catchUp.err = cleanLine(msg.err.Error())
		m.setError(msg.err)
	} else {
		m.catchUp = catchUpState{text: msg.text, stamp: msg.generated, sig: msg.signature, err: ""}
		m.catchUp.partial = msg.partial
		m.status = "Catch-up ready"
	}
	if m.destination == aiSummaries {
		m.renderDestination()
	}
}

// catchUpView renders the catch-up for the AI Summaries destination.
func (m Model) catchUpView() string {
	width := max(1, m.viewport.Width()-4)
	muted := lipgloss.NewStyle().Foreground(m.muted())
	bold := lipgloss.NewStyle().Bold(true)
	var b strings.Builder
	b.WriteString(bold.Render("Your catch-up") + "\n")
	if !m.previousVisit.IsZero() {
		b.WriteString(muted.Render("Since your last visit: "+m.previousVisit.Local().Format("Jan 02, 2006 15:04")) + "\n")
	}
	if !m.catchUp.stamp.IsZero() {
		b.WriteString(muted.Render("Written "+m.catchUp.stamp.Local().Format("Jan 02 15:04")+" · s refreshes") + "\n")
	}
	b.WriteString("\n")
	switch {
	case m.catchUp.busy:
		b.WriteString(muted.Render("Reading your unread messages and writing the catch-up…"))
	case m.catchUp.err != "":
		b.WriteString("Could not write the catch-up: " + m.catchUp.err + "\n")
		b.WriteString(muted.Render("Press s to try again."))
	case m.catchUp.sig == "" && m.unreadSignature() == "":
		b.WriteString("All caught up. No unread messages.")
	case m.catchUp.text == "":
		b.WriteString(muted.Render("No readable unread messages to summarize. Press s to refresh."))
	default:
		b.WriteString(renderMarkdown(m.catchUp.text, width))
		if m.catchUp.partial {
			b.WriteString("\n\n" + muted.Render("Partial: some unread messages were not read."))
		}
	}
	return ansi.Hardwrap(b.String(), width, true)
}
