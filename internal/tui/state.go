package tui

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/WaelRabah/slackline/internal/inference"
	"os"
	"path/filepath"
	"time"
)

type Options struct {
	ConfigPath, Theme string
	Provider          inference.Settings
	// Nil uses the defaults: sounds on, recent conversations from the last 14 days.
	SoundEnabled *bool
	RecentDays   *int
	StatePath    string
}
type sidebarState struct {
	Summaries     map[string]conversationSummary `json:"summaries,omitempty"`
	LastVisit     time.Time                      `json:"last_visit,omitempty"`
	ReadPositions map[string]string              `json:"read_positions,omitempty"`
	Interactions  map[string]time.Time           `json:"interactions"`
	Collapsed     map[string]bool                `json:"collapsed"`
	RecentOnly    *bool                          `json:"recent_only,omitempty"`
	SoundEnabled  *bool                          `json:"sound_enabled,omitempty"`
}

// State is scoped to both workspace and user; it contains no session credentials.
func StatePath(configPath, team, user string) string {
	identity := sha256.Sum256([]byte(team + "\x00" + user))
	return filepath.Join(filepath.Dir(configPath), fmt.Sprintf("state-%x.json", identity[:12]))
}
func (m *Model) loadState() {
	if m.statePath == "" {
		return
	}
	data, err := os.ReadFile(m.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		m.stateWarning = "Cannot read sidebar state"
		return
	}
	var state sidebarState
	if len(data) > 4<<20 || json.Unmarshal(data, &state) != nil {
		m.stateWarning = "Invalid sidebar state; using defaults"
		return
	}
	if state.Summaries != nil {
		m.summaries = state.Summaries
	}
	m.previousVisit = state.LastVisit
	if state.ReadPositions != nil {
		m.readPositions = state.ReadPositions
	}
	if state.Interactions != nil {
		m.interactions = state.Interactions
	}
	if state.Collapsed != nil {
		m.collapsed = state.Collapsed
	}
	if state.RecentOnly != nil {
		m.recentOnly = *state.RecentOnly && m.recentDays > 0
	}
	if state.SoundEnabled != nil {
		m.soundEnabled = *state.SoundEnabled
	}
	m.pruneInteractions()
}
func (m Model) SaveState() error {
	if m.statePath == "" {
		return nil
	}
	m.pruneInteractions()
	data, err := json.Marshal(sidebarState{Summaries: m.summaries, LastVisit: m.now(), ReadPositions: m.readPositions, Interactions: m.interactions, Collapsed: m.collapsed, RecentOnly: &m.recentOnly, SoundEnabled: &m.soundEnabled})
	if err != nil {
		return errors.New("cannot encode sidebar state")
	}
	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create sidebar state directory")
	}
	f, err := os.CreateTemp(dir, ".sidebar-*")
	if err != nil {
		return errors.New("cannot create sidebar state file")
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return errors.New("cannot write sidebar state")
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return errors.New("cannot sync sidebar state")
	}
	if err := f.Close(); err != nil {
		return errors.New("cannot close sidebar state")
	}
	if err := os.Rename(f.Name(), m.statePath); err != nil {
		return errors.New("cannot replace sidebar state")
	}
	return nil
}
