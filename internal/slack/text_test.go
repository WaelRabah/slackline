package slack

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDisplayTextReadsAttachmentsAndBlocks(t *testing.T) {
	var message Message
	raw := `{"ts":"1","text":"","attachments":[{"pretext":"Grafana alert","title":"High CPU","text":"cpu 97% on api-1","fallback":"fallback copy"}],
		"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Deploy finished"}},{"type":"actions","elements":[{"type":"section","text":{"type":"mrkdwn","text":"Open dashboard"}}]}]}`
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		t.Fatal(err)
	}
	text := message.DisplayText()
	for _, want := range []string{"Grafana alert", "High CPU", "cpu 97% on api-1", "Deploy finished", "Open dashboard"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "fallback copy") {
		t.Fatal("fallback duplicated real attachment text")
	}
}

func TestDisplayTextKeepsPlainMessages(t *testing.T) {
	if got := (Message{Text: "hello"}).DisplayText(); got != "hello" {
		t.Fatalf("got %q", got)
	}
}
