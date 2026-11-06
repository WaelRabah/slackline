package tui

import (
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
)

// Channels alert only on mentions; DMs always alert.
func TestChannelAlertsNeedAMentionDMsAlwaysDo(t *testing.T) {
	m := ready(t)
	m.session.UserID = "UME"
	m.conversations = append(m.conversations,
		slack.Conversation{ID: "C9", Name: "support_mlops"},
		slack.Conversation{ID: "D9", IsIM: true, User: "U2"},
	)
	chatter := slack.Message{TS: "1", User: "U2", Text: "isn't the same thing?"}
	if m.alertsUser("C9", chatter) {
		t.Fatal("channel message without a mention alerted the user")
	}
	mention := slack.Message{TS: "2", User: "U2", Text: "hey <@UME> take a look"}
	if !m.alertsUser("C9", mention) {
		t.Fatal("channel mention did not alert the user")
	}
	aliased := slack.Message{TS: "3", User: "U2", Text: "ping <@UME|wael>"}
	if !m.alertsUser("C9", aliased) {
		t.Fatal("mention with a label did not alert the user")
	}
	if !m.alertsUser("D9", chatter) {
		t.Fatal("direct message did not alert the user")
	}
	m.noteIncomingMessage("C9", chatter, "")
	if len(m.recent) != 0 {
		t.Fatal("non-mention channel message reached Latest")
	}
}
