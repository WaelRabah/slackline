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

func TestContextIncludesRepliesAndChecksExactWindowAndChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/search.messages" || !strings.Contains(r.Form.Get("query"), "in:C1 after:") {
			t.Error("wrong scoped search")
		}
		fmt.Fprint(w, `{"ok":true,"messages":{"paging":{"pages":1},"matches":[{"ts":"1792188000.000001","thread_ts":"1790000000.000001","text":"reply on old root","channel":{"id":"C1"}},{"ts":"1792188000.000002","text":"wrong channel","channel":{"id":"C2"}},{"ts":"1700000000.000001","text":"old","channel":{"id":"C1"}},{"ts":"1892188000.000001","text":"future","channel":{"id":"C1"}}]}}`)
	}))
	defer server.Close()
	c := testClientForContext(server)
	until := time.Unix(1792200000, 0)
	page, err := c.ConversationContext(context.Background(), "C1", until.Add(-14*24*time.Hour), until)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ThreadTS != "1790000000.000001" || page.Partial {
		t.Fatal("context scope or thread inclusion failed", err)
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
		fmt.Fprint(w, `{"ok":true,"messages":{"paging":{"pages":11},"matches":[{"ts":"1792188000.000001","text":"fixture","channel":{"id":"C1"}}]}}`)
	}))
	defer server.Close()
	until := time.Unix(1792200000, 0)
	client := testClientForContext(server)
	page, err := client.ConversationContext(context.Background(), "C1", until.Add(-14*24*time.Hour), until)
	if err != nil || calls != 10 || !page.Partial || len(page.Messages) != 1 {
		t.Fatal("pagination cap or dedup is not explicit")
	}
	calls = 0
	fail = true
	page, err = client.ConversationContext(context.Background(), "C1", until.Add(-14*24*time.Hour), until)
	if err == nil || len(page.Messages) != 0 {
		t.Fatal("presented failed partial fetch as complete")
	}
}
