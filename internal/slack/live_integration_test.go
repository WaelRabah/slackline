//go:build live

package slack_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/config"
	"github.com/WaelRabah/slackline/internal/slack"
)

// Opt-in, receive-only verification. Never log event contents or signed URLs.
func TestRealLiveConnection(t *testing.T) {
	if os.Getenv("SLACKLINE_LIVE_CHECK") != "1" {
		t.Skip("explicit live check required")
	}
	path, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := cfg.Credentials()
	if err != nil {
		t.Fatal(err)
	}
	duration := 15 * time.Second
	if os.Getenv("SLACKLINE_LIVE_HEARTBEAT_CHECK") == "1" {
		duration = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	connected := false
	messages, typing := 0, 0
	err = slack.NewClient(credentials).Listen(ctx, func(event slack.LiveEvent) {
		if event.Type == "_connection" && event.State == "live" {
			connected = true
		}
		if event.Type == "message" {
			messages++
		}
		if event.Type == "user_typing" {
			typing++
		}
	})
	if !connected {
		t.Fatalf("live handshake failed: %v", err)
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("live connection ended before observation completed: %v", err)
	}
	t.Logf("Live handshake verified; received message events=%d, typing events=%d; contents omitted", messages, typing)
}
