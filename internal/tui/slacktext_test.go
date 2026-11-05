package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
)

func TestSlackDateTokensShowTheirFallbackText(t *testing.T) {
	got := expandSlackDates("*Today*-<!date^1793142000^{date_long}|Wednesday, October 28, 2026>")
	if got != "*Today*-Wednesday, October 28, 2026" {
		t.Fatalf("date token not expanded: %q", got)
	}
	if strings.Contains(expandSlackDates("at <!date^1793142000^{time}>"), "<!date") {
		t.Fatal("bare date token left raw")
	}
}

// The Outlook Calendar messages in the screenshot have their content in blocks
// with an empty text field. They must render, not show as blank entries.
func TestBlockOnlyBotMessagesRenderTheirContent(t *testing.T) {
	raw := `{"ts":"1793833200.000100","username":"Outlook Calendar","text":"",
		"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*Today*-<!date^1793833200^{date_long}|Thursday, November 5, 2026>"}}]}`
	var message slack.Message
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		t.Fatal(err)
	}
	m := Model{users: map[string]string{}}
	body := m.messageText(message.DisplayText())
	if !strings.Contains(body, "Today") || !strings.Contains(body, "Thursday, November 5, 2026") || strings.Contains(body, "<!date") {
		t.Fatalf("block content not rendered: %q", body)
	}
}
