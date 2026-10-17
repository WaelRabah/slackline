package slack

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestReadMarkersUseCorrectConversationAndThreadEndpoints(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if r.Form.Get("channel") != "C1" || r.Form.Get("ts") != "100.000002" {
			t.Error("wrong read position")
		}
		if calls == 1 && (r.URL.Path != "/conversations.mark" || r.Form.Get("thread_ts") != "") {
			t.Error("channel mark used thread endpoint")
		}
		if calls == 2 && (r.URL.Path != "/subscriptions.thread.mark" || r.Form.Get("thread_ts") != "100.000001") {
			t.Error("thread mark lost root")
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	for _, thread := range []string{"", "100.000001"} {
		if err := client.MarkRead(context.Background(), "C1", thread, "100.000002"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("wrong mark call count")
	}
	if err := client.MarkRead(context.Background(), "C1", "", "invalid"); err == nil {
		t.Fatal("invalid mark was accepted")
	}
}
func TestUnreadCountingPagesAndDeduplicatesWithinSnapshot(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if r.Form.Get("oldest") != "100.000001" || r.Form.Get("latest") != "105.000001" || r.Form.Get("inclusive") != "true" {
			t.Error("count boundary lost")
		}
		if calls == 1 {
			fmt.Fprint(w, `{"ok":true,"has_more":true,"messages":[{"ts":"105.000001","user":"OTHER"},{"ts":"106.000001","user":"OTHER"},{"ts":"104.000001","user":"SELF"}],"response_metadata":{"next_cursor":"next"}}`)
			return
		}
		if r.Form.Get("cursor") != "next" {
			t.Error("count cursor lost")
		}
		fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"105.000001","user":"OTHER"},{"ts":"102.000001","user":"OTHER"},{"ts":"100.000001","user":"OTHER"}]}`)
	})
	count, err := client.CountUnread(context.Background(), "C1", "100.000001", "105.000001", "SELF")
	if err != nil || !count.Exact || count.Count != 2 || calls != 2 {
		t.Fatal("unread count included boundary/own/duplicate/out-of-snapshot message")
	}
}
func TestUnreadCountingDoesNotClaimExactWhenTruncated(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"has_more":true,"messages":[{"ts":"102.000001","user":"OTHER"}]}`)
	})
	count, err := client.CountUnread(context.Background(), "C1", "100.000001", "105.000001", "SELF")
	if err != nil || count.Exact || count.Count != 1 {
		t.Fatal("truncated backlog must be a lower bound")
	}
}
func TestThreadFeedUsesActivityCursorAndKeepsReplyMetadata(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if r.URL.Path != "/subscriptions.thread.getView" {
			t.Error("wrong threads endpoint")
		}
		if calls == 2 && r.Form.Get("current_ts") != "200.000001" {
			t.Error("wrong thread pagination cursor")
		}
		if calls == 1 {
			fmt.Fprint(w, `{"ok":true,"total_unread_replies":2,"has_more":true,"threads":[{"root_msg":{"channel":"C1","ts":"100.000001","last_read":"100.000001","reply_count":2,"latest_reply":"200.000001","text":"root"},"unread_replies":[{"ts":"200.000001","text":"new"}],"latest_replies":[{"ts":"200.000001","text":"new"}]}]}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"has_more":false,"threads":[]}`)
	})
	page, err := client.Threads(context.Background(), "300.000001")
	if err != nil || page.Next != "200.000001" || page.TotalUnread != 2 || page.Threads[0].Root.Channel != "C1" || len(page.Threads[0].UnreadReplies) != 1 {
		t.Fatal("thread metadata/cursor lost")
	}
	next, err := client.Threads(context.Background(), page.Next)
	if err != nil || next.HasMore || next.Next != "" {
		t.Fatal("thread pagination did not finish")
	}
}
func TestThreadFeedRejectsNonAdvancingPagination(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"has_more":true,"threads":[{"root_msg":{"channel":"C1","ts":"300.000001"}}]}`)
	})
	if _, err := client.Threads(context.Background(), "300.000001"); err == nil {
		t.Fatal("repeated thread page accepted")
	}
}
