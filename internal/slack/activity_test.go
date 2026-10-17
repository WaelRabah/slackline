package slack

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestActivityMergesConversationTypes(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client.counts" {
			t.Error("unexpected activity endpoint")
		}
		fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","last_read":"1792230000.000001","latest":"1792230001.000001","has_unreads":true}],"groups":[{"id":"P1"}],"ims":[{"id":"D1"}],"mpims":[{"id":"G1"}]}`)
	})
	rows, err := c.Activity(context.Background())
	if err != nil || len(rows) != 4 || !rows[0].HasUnreads || rows[0].LastRead != "1792230000.000001" || rows[3].ID != "G1" {
		t.Fatal("activity metadata lost")
	}
}
func TestRecentSentPaginationAndRollingCutoff(t *testing.T) {
	cutoff := TimestampTime("1792230000.000002")
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if r.URL.Path != "/search.messages" || !strings.HasPrefix(r.Form.Get("query"), "from:me after:") || r.Form.Get("sort_dir") != "desc" || r.Form.Get("page") != fmt.Sprint(calls) {
			t.Error("bad recent search parameters")
		}
		if calls == 1 {
			fmt.Fprint(w, `{"ok":true,"messages":{"paging":{"pages":2,"total":4},"matches":[{"ts":"1792230002.000001","channel":{"id":"C1"}},{"ts":"1792230000.000002","thread_ts":"100.1","channel":{"id":"D1"}}]}}`)
		} else {
			fmt.Fprint(w, `{"ok":true,"messages":{"paging":{"pages":2,"total":4},"matches":[{"ts":"1792230000.000001","channel":{"id":"OLD"}},{"ts":"1792230001.000001","channel":{"id":"C1"}}]}}`)
		}
	})
	recent, err := c.RecentSent(context.Background(), cutoff)
	if err != nil || calls != 2 || len(recent) != 2 || !recent["D1"].Equal(cutoff) || !recent["C1"].Equal(TimestampTime("1792230002.000001")) {
		t.Fatal("pagination, thread replies, deduplication or cutoff failed")
	}
}
func TestRecentSentFailureDoesNotReturnPartialFilter(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"ok":true,"messages":{"paging":{"pages":2},"matches":[{"ts":"1792230002.000001","channel":{"id":"C1"}}]}}`)
			return
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	recent, err := c.RecentSent(context.Background(), time.Unix(1792230000, 0))
	if err == nil || recent != nil || calls != 2 {
		t.Fatal("partial search must not hide other conversations")
	}
}
func TestTimestampTimePreservesMicrosecondsAndRejectsInvalid(t *testing.T) {
	a, b := TimestampTime("1792230000.000001"), TimestampTime("1792230000.000002")
	if b.Sub(a) != time.Microsecond {
		t.Fatal("timestamp lost precision")
	}
	for _, value := range []string{"", "garbage", "0", "1.bad", "1.1234567890"} {
		if !TimestampTime(value).IsZero() {
			t.Fatalf("accepted invalid timestamp %q", value)
		}
	}
}
