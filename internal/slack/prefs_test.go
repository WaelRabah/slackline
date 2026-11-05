package slack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutedChannelsReadsUserPrefs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.prefs.get" {
			t.Errorf("unexpected call %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"ok":true,"prefs":{"muted_channels":"C1, C2,,"}}`)
	}))
	defer server.Close()
	muted, err := testClientForContext(server).MutedChannels(context.Background())
	if err != nil || !muted["C1"] || !muted["C2"] || len(muted) != 2 {
		t.Fatalf("unexpected muted set %v, %v", muted, err)
	}
}
