package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// summaryDigest fingerprints a stored summary: the newest message it covers, the
// time it was written, and its text. A stored summary whose digest does not match
// was changed or damaged on disk, so it is discarded and written again.
func summaryDigest(summary conversationSummary) string {
	sum := sha256.New()
	sum.Write([]byte(summary.Newest + "\x00" + summary.Generated.UTC().Format(time.RFC3339Nano) + "\x00" + summary.Text))
	return hex.EncodeToString(sum.Sum(nil))
}

// verifySummaries keeps only summaries whose digest matches. It returns the number discarded.
func verifySummaries(summaries map[string]conversationSummary) int {
	discarded := 0
	for channel, summary := range summaries {
		if summary.Digest == "" || summary.Digest != summaryDigest(summary) {
			delete(summaries, channel)
			discarded++
		}
	}
	return discarded
}
