package tui

import (
	"testing"
	"time"
)

func TestDailyCatchUpRunsOnceAfterElevenLocal(t *testing.T) {
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, time.Local)
	at := func(hour, minute int) time.Time {
		return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	today := day.Format("2006-01-02")

	if dailyDue(at(10, 59), "", true, false) {
		t.Fatal("ran before 11:00")
	}
	if !dailyDue(at(11, 0), "", true, false) {
		t.Fatal("did not run at 11:00")
	}
	if dailyDue(at(15, 0), today, true, false) {
		t.Fatal("ran twice on the same day")
	}
	if dailyDue(at(15, 0), "", false, false) {
		t.Fatal("ran without a provider")
	}
	if dailyDue(at(15, 0), "", true, true) {
		t.Fatal("ran while a catch-up was already busy")
	}
}

func TestSummaryChimeOnlyOnSuccessAndWhenEnabled(t *testing.T) {
	m := Model{soundEnabled: true}
	if m.summaryChime(false) != nil {
		t.Fatal("chimed for a failed summary")
	}
	m.soundEnabled = false
	if m.summaryChime(true) != nil {
		t.Fatal("chimed while sounds are off")
	}
}
