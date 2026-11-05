package slack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContextReadsHistoryAndThreadRepliesInWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/conversations.history":
			if r.Form.Get("channel") != "C1" || r.Form.Get("oldest") == "" || r.Form.Get("latest") == "" {
				t.Error("history call missing channel or window")
			}
			fmt.Fprint(w, `{"ok":true,"messages":[
				{"ts":"1792188000.000001","text":"root with replies","reply_count":1},
				{"ts":"1792188000.000002","text":"plain"},
				{"ts":"1700000000.000001","text":"too old"},
				{"ts":"1892188000.000001","text":"future"}],"has_more":false}`)
		case "/conversations.replies":
			if r.Form.Get("ts") != "1792188000.000001" {
				t.Error("replies requested for the wrong thread")
			}
			fmt.Fprint(w, `{"ok":true,"messages":[
				{"ts":"1792188000.000001","text":"root with replies"},
				{"ts":"1792188100.000001","thread_ts":"1792188000.000001","text":"reply"}],"has_more":false}`)
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	}))
	defer server.Close()
	until := time.Unix(1792200000, 0)
	page, err := testClientForContext(server).ConversationContext(context.Background(), "C1", until.Add(-14*24*time.Hour), until)
	if err != nil || page.Partial || len(page.Messages) != 3 {
		t.Fatalf("unexpected page %+v, %v", page, err)
	}
	for _, message := range page.Messages {
		if message.Channel != "C1" {
			t.Fatal("message lost its channel")
		}
	}
}
func testClientForContext(server *httptest.Server) *Client {
	creds, _ := NewCredentials("xoxc-test", "xoxd-test")
	c := NewClient(creds)
	c.baseURL = server.URL + "/"
	c.http = server.Client()
	return c
}
func TestOpenDMDoesNotPostMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/conversations.open" || r.Form.Get("users") != "U2" {
			t.Error("unexpected mutation")
		}
		fmt.Fprint(w, `{"ok":true,"channel":{"id":"D2"}}`)
	}))
	defer server.Close()
	c, err := testClientForContext(server).OpenDM(context.Background(), "U2")
	if err != nil || c.ID != "D2" || !c.IsIM || c.User != "U2" {
		t.Fatal("DM open failed", err)
	}
}

func TestContextPaginationLimitIsExplicitAndErrorsDiscardPartial(t *testing.T) {
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail && calls == 2 {
			fmt.Fprint(w, `{"ok":false,"error":"ratelimited"}`)
			return
		}
		// Always report more pages so the cap is what stops the read.
		fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"1792188000.000001","text":"fixture"}],"has_more":true,"response_metadata":{"next_cursor":"next"}}`)
	}))
	defer server.Close()
	until := time.Unix(1792200000, 0)
	client := testClientForContext(server)
	page, err := client.ConversationContext(context.Background(), "C1", until.Add(-14*24*time.Hour), until)
	if err != nil || calls != contextHistoryPages || !page.Partial || len(page.Messages) != 1 {
		t.Fatalf("pagination cap or dedup is not explicit: calls=%d page=%+v err=%v", calls, page, err)
	}
	calls = 0
	fail = true
	page, err = client.ConversationContext(context.Background(), "C1", until.Add(-14*24*time.Hour), until)
	if err == nil || len(page.Messages) != 0 || !strings.Contains(err.Error(), "ratelimited") {
		t.Fatal("presented failed partial fetch as complete")
	}
}
