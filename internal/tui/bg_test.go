package tui

import "testing"

// The queue never runs more than bgWorkers jobs at once, and a job already queued
// or running is not added twice.
func TestBackgroundQueueLimitsWorkersAndDedupes(t *testing.T) {
	m := Model{summaryBusy: map[string]bool{}, summaries: map[string]conversationSummary{}}
	for _, channel := range []string{"C1", "C2", "C3", "C4", "C5"} {
		if !m.enqueue(bgJob{kind: bgSummary, channel: channel}) {
			t.Fatalf("job for %s not queued", channel)
		}
	}
	if m.enqueue(bgJob{kind: bgSummary, channel: "C1"}) {
		t.Fatal("duplicate job queued")
	}
	m.pump()
	if m.bgRunning != bgWorkers || len(m.bgQueue) != 5-bgWorkers {
		t.Fatalf("running=%d queued=%d, want running=%d queued=%d", m.bgRunning, len(m.bgQueue), bgWorkers, 5-bgWorkers)
	}
	if !m.summaryBusy["C5"] {
		t.Fatal("queued summary not shown as busy")
	}
}

// Without AI enabled, nothing is queued in the background.
func TestBackgroundRefreshDoesNothingWithoutAI(t *testing.T) {
	m := Model{}
	if cmd := m.backgroundRefresh(); cmd != nil || len(m.bgQueue) != 0 {
		t.Fatal("queued work without a provider")
	}
}
