package tui

import (
	"strings"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
)

func TestPopupScriptEscapesMessageText(t *testing.T) {
	script := popupScript(`#ops · "Ana"`, "hi \" end \\ \"quoted\" line\nnext")
	// Only the four delimiting quotes may be unescaped.
	if n := strings.Count(strings.ReplaceAll(strings.ReplaceAll(script, `\\`, ""), `\"`, ""), `"`); n != 4 {
		t.Fatalf("unescaped quotes: %d in %s", n, script)
	}
	if !strings.Contains(script, `\"quoted\"`) || !strings.Contains(script, `\\`) || strings.Contains(script, "\n") {
		t.Fatalf("message text not escaped: %s", script)
	}
}

func TestPopupSkipsOpenConversationAndDemo(t *testing.T) {
	conv := slack.Conversation{ID: "C1", Name: "ops"}
	msg := slack.Message{TS: "1", User: "U2", Text: "hi"}
	if (Model{active: "C1"}).popupCmd(conv, msg) != nil {
		t.Fatal("popup for the open conversation")
	}
	if (Model{demo: true}).popupCmd(conv, msg) != nil {
		t.Fatal("popup in demo")
	}
}
