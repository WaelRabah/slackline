package tui

import (
	"testing"
	"time"
)

// A saved summary whose text changed on disk fails its digest and is dropped;
// an intact one is kept.
func TestSummaryDigestDetectsTampering(t *testing.T) {
	good := conversationSummary{Text: "intact", Newest: "1", Generated: time.Unix(1700000000, 0)}
	good.Digest = summaryDigest(good)
	tampered := good
	tampered.Text = "changed on disk"
	summaries := map[string]conversationSummary{"ok": good, "bad": tampered, "legacy": {Text: "no digest"}}
	if discarded := verifySummaries(summaries); discarded != 2 {
		t.Fatalf("discarded %d, want 2", discarded)
	}
	if _, ok := summaries["ok"]; !ok {
		t.Fatal("intact summary dropped")
	}
	if _, ok := summaries["bad"]; ok {
		t.Fatal("tampered summary kept")
	}
}

// Summaries that are still current are not rerun on startup; newer messages make them stale.
func TestStartupRerunsOnlySummariesWithNewerMessages(t *testing.T) {
	m := Model{latestSeen: map[string]string{"C1": "1700000100.000001", "C2": "1700000200.000001"}, summaries: map[string]conversationSummary{}, summaryBusy: map[string]bool{}, bgActive: map[string]bool{}}
	m.summaries["C1"] = conversationSummary{Newest: "1700000100.000001"}
	m.summaries["C2"] = conversationSummary{Newest: "1700000100.000001"}
	if m.summaryStale("C1") {
		t.Fatal("current summary marked stale")
	}
	if !m.summaryStale("C2") {
		t.Fatal("summary with a newer message not marked stale")
	}
}
