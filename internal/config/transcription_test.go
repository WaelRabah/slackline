package config

import (
	"path/filepath"
	"testing"

	"github.com/WaelRabah/slackline/internal/inference"
)

// The transcription model set in Settings is saved and loaded back.
func TestTranscriptionModelRoundTripsThroughSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	ai := inference.Settings{Enabled: true, BaseURL: "https://api.example.com/v1", ChatModel: "chat", TranscriptionModel: "whisper-test"}
	if err := SavePreferences(path, ai, "mint", true, 14); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.TranscriptionModel != "whisper-test" {
		t.Fatalf("transcription model not saved: %q", cfg.AI.TranscriptionModel)
	}
}
