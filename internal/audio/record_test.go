package audio

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRecordValidatesDurationAndReportsMissingFFmpeg(t *testing.T) {
	if _, err := Record(context.Background(), 0); err == nil {
		t.Fatal("accepted zero-second recording")
	}
	if _, err := Record(context.Background(), 61); err == nil {
		t.Fatal("accepted recording above the limit")
	}
	t.Setenv("PATH", t.TempDir())
	_, err := Record(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg not found") {
		t.Fatalf("expected missing ffmpeg error, got %v", err)
	}
}

func TestLimitWriterStopsOversizedCapture(t *testing.T) {
	w := &limitWriter{buf: new(bytes.Buffer), limit: 4}
	if _, err := w.Write([]byte("abcd")); err != nil {
		t.Fatal("write within limit failed")
	}
	if _, err := w.Write([]byte("e")); err == nil {
		t.Fatal("write beyond limit accepted")
	}
}
