// Package logs writes errors to a private log file so failures can be
// investigated after the fact. Secrets are redacted before anything is written.
package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxBytes = 5 << 20

var (
	mu   sync.Mutex
	file *os.File
)

// Open creates the log directory and file (dir/slackline.log) and sends every
// later Error call there. The previous log is kept as slackline.log.1 once the
// file grows past 5 MB.
func Open(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("cannot create log directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, "slackline.log")
	if info, err := os.Stat(path); err == nil && info.Size() > maxBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("cannot open log file %s: %w", path, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
	}
	file = f
	return path, nil
}

// Close flushes and closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
	}
}

// Error records an error under a scope such as "slack", "ai", or "ui".
func Error(scope string, err error) {
	if err == nil {
		return
	}
	write("ERROR", scope, err.Error())
}

// Info records a normal event, such as startup or shutdown.
func Info(scope, message string) {
	write("INFO", scope, message)
}

// Panic records a recovered panic together with its stack trace.
func Panic(scope string, value any, stack []byte) {
	write("PANIC", scope, fmt.Sprintf("%v\n%s", value, stack))
}

func write(level, scope, message string) {
	mu.Lock()
	defer mu.Unlock()
	if file == nil {
		return
	}
	line := fmt.Sprintf("%s %s [%s] %s\n", time.Now().Format(time.RFC3339), level, scope, redact(message))
	_, _ = file.WriteString(line)
}

var redactions = []struct {
	pattern *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`xox[a-z]-[A-Za-z0-9-]+`), "xox-REDACTED"},
	{regexp.MustCompile(`(?i)bearer\s+[^\s"'&,]+`), "Bearer REDACTED"},
	{regexp.MustCompile(`(?i)(token|cookie|api[_-]?key|password|secret)(["']?\s*[:=]\s*["']?)[^\s"'&,}]+`), "$1$2REDACTED"},
	{regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), "UUID-REDACTED"},
}

// redact removes credentials from text before it is written to the log.
func redact(text string) string {
	for _, r := range redactions {
		text = r.pattern.ReplaceAllString(text, r.replace)
	}
	return strings.ReplaceAll(text, "\r", "")
}
