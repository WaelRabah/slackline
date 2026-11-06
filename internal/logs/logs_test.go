package logs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestErrorsAreWrittenPrivatelyAndRedacted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".slackline")
	path, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log file permissions %v, %v", info.Mode().Perm(), err)
	}
	Error("slack", errors.New("failed with token xoxc-123-secret and Bearer abc.def and key 11111111-2222-3333-4444-555555555555"))
	Info("app", "started")
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, leak := range []string{"xoxc-123", "abc.def", "11111111-2222"} {
		if strings.Contains(text, leak) {
			t.Fatalf("secret %q written to log: %s", leak, text)
		}
	}
	if !strings.Contains(text, "ERROR [slack]") || !strings.Contains(text, "INFO [app] started") {
		t.Fatalf("entries missing: %s", text)
	}
}

func TestLogRotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slackline.log")
	if err := os.WriteFile(path, make([]byte, maxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal("large log not rotated")
	}
}
