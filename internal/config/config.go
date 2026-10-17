package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Notifications struct {
		Enabled *bool `toml:"enabled"`
	} `toml:"notifications"`
	Sidebar struct {
		RecentDays *int `toml:"recent_days"`
	} `toml:"sidebar"`
	Slack struct {
		Token  string `toml:"token"`
		Cookie string `toml:"cookie"`
	} `toml:"slack"`
	AI inference.Settings `toml:"ai"`
	UI struct {
		Theme string `toml:"theme"`
	} `toml:"ui"`
}

// Deliberately use ~/.config on macOS too, as agreed for this application.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot determine config directory")
		}
		dir = filepath.Join(home, ".config")
	}
	path := filepath.Join(dir, "slackline", "config.toml")
	// Existing installations retain their login, preferences and adjacent state.
	// New installs use Slackline; once its config exists it always takes priority.
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		legacy := filepath.Join(dir, "slack_in_term", "config.toml")
		if info, err := os.Stat(legacy); err == nil && !info.IsDir() {
			return legacy, nil
		}
	}
	return path, nil
}

func Load(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, errors.New("cannot read config file")
	}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			// TOML decoder errors can contain the original credential-bearing line.
			return cfg, errors.New("invalid config TOML; check the file locally")
		}
	}
	if cfg.Sidebar.RecentDays != nil && (*cfg.Sidebar.RecentDays < 0 || *cfg.Sidebar.RecentDays > 3650) {
		return cfg, errors.New("sidebar.recent_days must be between 0 and 3650")
	}
	if token, ok := os.LookupEnv("SLACK_TOKEN"); ok {
		cfg.Slack.Token = token
	}
	if cookie, ok := os.LookupEnv("SLACK_COOKIE"); ok {
		cfg.Slack.Cookie = cookie
	}
	return cfg, nil
}

func (c Config) Credentials() (slack.Credentials, error) {
	return slack.NewCredentials(c.Slack.Token, c.Slack.Cookie)
}

// SaveSession preserves other settings, including unknown keys and AI settings.
// Comments and formatting are normalized by the TOML encoder.
func SaveSession(path string, creds slack.Credentials) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot read existing config")
	}
	values := map[string]any{}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &values); err != nil {
			return errors.New("invalid existing config; login was not saved")
		}
	}
	section, exists := values["slack"]
	slackValues, ok := section.(map[string]any)
	if exists && !ok {
		return errors.New("config slack setting must be a TOML table")
	}
	if !exists {
		slackValues = map[string]any{}
	}
	slackValues["token"] = creds.Token()
	slackValues["cookie"] = creds.Cookie()
	values["slack"] = slackValues
	if _, ok := values["ai"]; !ok {
		values["ai"] = map[string]any{"base_url": "https://api.compactif.ai/v1", "api_key": "", "chat_model": "", "embedding_model": ""}
	}
	return saveValues(path, values)
}

func saveValues(path string, values map[string]any) error {
	data, err := toml.Marshal(values)
	if err != nil {
		return errors.New("cannot encode config")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create config directory")
	}
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return errors.New("cannot create private config file")
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return errors.New("cannot secure config file")
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return errors.New("cannot write config file")
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return errors.New("cannot sync config file")
	}
	if err := f.Close(); err != nil {
		return errors.New("cannot close config file")
	}
	if err := os.Rename(name, path); err != nil {
		return errors.New("cannot replace config file")
	}
	return nil
}

// String and GoString prevent accidental credential exposure in formatted logs.
func (c Config) String() string   { return "config [credentials redacted]" }
func (c Config) GoString() string { return fmt.Sprint(c) }

// SavePreferences changes only editable preferences; session and unknown keys survive.
func SavePreferences(path string, ai inference.Settings, theme string, sound bool, days int) error {
	if days < 0 || days > 3650 {
		return errors.New("recent days must be between 0 and 3650")
	}
	if ai.Enabled {
		if err := ai.Validate(); err != nil {
			return err
		}
	}
	if theme != "mint" && theme != "ocean" && theme != "amber" {
		return errors.New("unknown theme")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot read existing config")
	}
	values := map[string]any{}
	if len(data) > 0 && toml.Unmarshal(data, &values) != nil {
		return errors.New("invalid existing config; preferences were not saved")
	}
	set := func(section, key string, value any) error {
		table, ok := values[section].(map[string]any)
		if values[section] != nil && !ok {
			return errors.New("config preference must be a TOML table")
		}
		if table == nil {
			table = map[string]any{}
			values[section] = table
		}
		table[key] = value
		return nil
	}
	entries := []struct {
		section, key string
		value        any
	}{
		{"ai", "enabled", ai.Enabled}, {"ai", "base_url", ai.BaseURL}, {"ai", "api_key", ai.APIKey}, {"ai", "chat_model", ai.ChatModel}, {"ai", "embedding_model", ai.EmbeddingModel},
		{"ui", "theme", theme}, {"notifications", "enabled", sound}, {"sidebar", "recent_days", days},
	}
	for _, entry := range entries {
		if err := set(entry.section, entry.key, entry.value); err != nil {
			return err
		}
	}
	return saveValues(path, values)
}
