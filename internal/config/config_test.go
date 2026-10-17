package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/pelletier/go-toml/v2"
)

func TestSaveSessionPreservesSettingsAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "[ai]\napi_key = 'private-ai-key'\nchat_model = 'chat'\nembedding_model = 'embed'\n[custom]\noption = true\n[slack]\nextra = 'keep'\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	creds, _ := slack.NewCredentials("xoxc-test", "xoxd-test%2F")
	if err := SaveSession(path, creds); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config not owner-only")
	}
	data, _ := os.ReadFile(path)
	var values map[string]any
	if err := toml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	if values["ai"].(map[string]any)["api_key"] != "private-ai-key" || values["custom"].(map[string]any)["option"] != true || values["slack"].(map[string]any)["extra"] != "keep" {
		t.Fatal("lost existing settings")
	}
}

func TestLoadOverridesAndRedaction(t *testing.T) {
	t.Setenv("SLACK_TOKEN", "xoxc-environment")
	t.Setenv("SLACK_COOKIE", "d=xoxd-environment%2F")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	creds, err := cfg.Credentials()
	if err != nil || creds.Cookie() != "xoxd-environment%2F" {
		t.Fatal("environment credentials not loaded")
	}
	if strings.Contains(fmt.Sprintf("%v %#v", cfg, cfg), "xox") {
		t.Fatal("config formatting leaks credentials")
	}
	t.Setenv("SLACK_TOKEN", "")
	cfg, _ = Load(filepath.Join(t.TempDir(), "missing.toml"))
	if _, err := cfg.Credentials(); err == nil {
		t.Fatal("explicit empty override ignored")
	}
}

func TestInvalidTOMLDoesNotLeakSecretsOrOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := []byte("[slack]\ntoken = xoxc-secret-without-quotes\n")
	os.WriteFile(path, data, 0600)
	if _, err := Load(path); err == nil || strings.Contains(err.Error(), "xoxc-secret") {
		t.Fatal("unsafe parser error")
	}
	creds, _ := slack.NewCredentials("xoxc-test", "xoxd-test")
	if err := SaveSession(path, creds); err == nil {
		t.Fatal("overwrote malformed config")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("modified malformed config")
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/test-config")
	path, err := DefaultPath()
	if err != nil || path != "/tmp/test-config/slackline/config.toml" {
		t.Fatal("XDG config path ignored")
	}
}

func TestNotificationAndRecentSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[notifications]\nenabled = false\n[sidebar]\nrecent_days = 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Notifications.Enabled == nil || *cfg.Notifications.Enabled || cfg.Sidebar.RecentDays == nil || *cfg.Sidebar.RecentDays != 0 {
		t.Fatal("explicit false/zero settings were not loaded")
	}
	if err := os.WriteFile(path, []byte("[sidebar]\nrecent_days = -1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("negative recent window accepted")
	}
}

func TestDefaultPathPreservesLegacySessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	legacy := filepath.Join(dir, "slack_in_term", "config.toml")
	os.MkdirAll(filepath.Dir(legacy), 0700)
	os.WriteFile(legacy, []byte("# existing session"), 0600)
	path, err := DefaultPath()
	if err != nil || path != legacy {
		t.Fatal("existing session path lost during rename")
	}
	current := filepath.Join(dir, "slackline", "config.toml")
	os.MkdirAll(filepath.Dir(current), 0700)
	os.WriteFile(current, []byte("# new session"), 0600)
	path, err = DefaultPath()
	if err != nil || path != current {
		t.Fatal("Slackline config should take priority")
	}
}
