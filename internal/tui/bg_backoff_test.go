package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/WaelRabah/slackline/internal/slack"
)

func TestBackgroundFailuresBackOffAutomaticRuns(t *testing.T) {
	if got := backoffFor(&slack.RateLimitError{RetryAfter: 10 * time.Second}); got != bgMinRateBackoff {
		t.Fatalf("short rate limit backoff = %s, want %s", got, bgMinRateBackoff)
	}
	if got := backoffFor(&slack.RateLimitError{RetryAfter: 30 * time.Minute}); got != 30*time.Minute {
		t.Fatalf("Slack's own delay ignored: %s", got)
	}
	if got := backoffFor(errors.New("inference provider returned HTTP 402")); got != bgFailureBackoff {
		t.Fatalf("provider failure backoff = %s, want %s", got, bgFailureBackoff)
	}

	m := ready(t)
	m.summaryBusy = map[string]bool{}
	channel := m.active
	job := m.summaryJob(channel)
	m.bgActive = map[string]bool{jobKey(job.kind, job.channel): true}
	m.bgRunning = 1
	m.applyBG(bgResult{job: job, err: errors.New("inference provider returned HTTP 402")})
	if !m.inBackoff(bgSummary, channel) {
		t.Fatal("failed summary is not backing off, so every refresh would retry it")
	}
	if m.inBackoff(bgUnread, channel) {
		t.Fatal("summary failure must not block unread jobs for the same channel")
	}
}
