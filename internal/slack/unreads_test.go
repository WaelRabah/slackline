package slack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnreadMessagesKeepsOnlyUnreadOthersMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.history" {
			t.Errorf("unexpected call %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"ok":true,"messages":[
			{"ts":"1700000300.000001","user":"U2","text":"new from someone"},
			{"ts":"1700000250.000001","user":"ME","text":"my own"},
			{"ts":"1700000100.000001","user":"U2","text":"already read"},
			{"ts":"1700000280.000001","user":"U3","subtype":"channel_join","text":"joined"},
			{"ts":"1700000290.000001","user":"U3","subtype":"thread_broadcast","text":"broadcast"}],"has_more":false}`)
	}))
	defer server.Close()
	page, err := testClientForContext(server).UnreadMessages(context.Background(), "C1", "1700000200.000000", "1700000400.000000", "ME")
	if err != nil || page.Partial || len(page.Messages) != 2 {
		t.Fatalf("unexpected page %+v, %v", page, err)
	}
	if page.Messages[0].Channel != "C1" {
		t.Fatal("channel not set")
	}
}
