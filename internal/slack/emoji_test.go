package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestEmojiDirectoryAndMessageReactionDecoding(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/reactions.get" {
			_ = r.ParseForm()
			if r.Form.Get("channel") != "C1" || r.Form.Get("timestamp") != "200.1" || r.Form.Get("full") != "true" {
				t.Error("missing reaction query")
			}
			fmt.Fprint(w, `{"ok":true,"message":{"reactions":[{"name":"wave","count":15,"users":["U1"]}]}}`)
			return
		}
		if r.URL.Path != "/emoji.list" {
			t.Error("wrong emoji endpoint")
		}
		fmt.Fprint(w, `{"ok":true,"emoji":{"shipit":"alias:rocket","custom":"https://emoji.slack-edge.com/custom.png"}}`)
	})
	values, err := client.Emojis(context.Background())
	if err != nil || values["shipit"] != "alias:rocket" {
		t.Fatal("emoji directory not decoded")
	}
	reactions, err := client.Reactions(context.Background(), "C1", "200.1")
	if err != nil || len(reactions) != 1 || reactions[0].Count != 15 {
		t.Fatal("authoritative counts lost")
	}
	var message Message
	err = json.Unmarshal([]byte(`{"ts":"200.1","reactions":[{"name":"wave","count":15,"users":["U1"]}]}`), &message)
	if err != nil || len(message.Reactions) != 1 || message.Reactions[0].Count != 15 {
		t.Fatal("server count was lost")
	}
	event, err := ParseLiveEvent([]byte(`{"type":"reaction_added","reaction":"wave","user":"U1","item":{"type":"message","channel":"C1","ts":"200.1"},"event_ts":"201.1"}`))
	if err != nil || event.Reaction == nil || event.Reaction.Channel != "C1" || event.Reaction.TS != "200.1" || event.Reaction.User != "U1" {
		t.Fatal("live reaction envelope lost")
	}
}
