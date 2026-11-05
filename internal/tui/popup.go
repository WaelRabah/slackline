package tui

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

const popupBodyRunes = 140

// popupScript builds the AppleScript for a macOS notification. Quotes and
// backslashes are escaped so message text cannot change the script.
func popupScript(title, body string) string {
	escape := func(value string) string {
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, `"`, `\"`)
		return strings.Join(strings.Fields(value), " ")
	}
	return `display notification "` + escape(body) + `" with title "` + escape(title) + `"`
}

// popupCmd shows a macOS notification for an incoming message. It follows the
// notification setting and skips the conversation that is already open.
func (m Model) popupCmd(conversation slack.Conversation, message slack.Message) tea.Cmd {
	if runtime.GOOS != "darwin" || m.demo || conversation.ID == m.active || m.mutedChannels[conversation.ID] {
		return nil
	}
	sender := m.users[message.User]
	if sender == "" {
		sender = message.Username
	}
	if sender == "" {
		sender = "Slack"
	}
	title := m.conversationLabel(conversation) + " · " + cleanLine(sender)
	body := []rune(cleanLine(m.messageText(message.DisplayText())))
	if len(body) > popupBodyRunes {
		body = append(body[:popupBodyRunes], '…')
	}
	script := popupScript(title, string(body))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).Run()
		return nil
	}
}
