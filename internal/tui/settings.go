package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/WaelRabah/slackline/internal/config"
	"github.com/WaelRabah/slackline/internal/inference"
	"github.com/charmbracelet/x/ansi"
)

type preferences struct {
	AI    inference.Settings
	Theme string
	Sound bool
	Days  int
}
type settingsState struct {
	values        preferences
	row           int
	editing, busy bool
	input         textinput.Model
	error         string
}
type preferencesSaved struct {
	values preferences
	err    error
}

var settingLabels = []string{"Conversation summaries", "Provider base URL", "API key", "Chat model", "Embedding model (future search)", "Theme", "Message sounds", "Recent conversation days"}

func (m Model) accent() color.Color {
	switch m.theme {
	case "ocean":
		return lipgloss.Color("#75BFFF")
	case "amber":
		return lipgloss.Color("#F4B860")
	}
	return lipgloss.Color("#65D6AD")
}
func (m Model) muted() color.Color { return lipgloss.Color("#8792A2") }
func (m *Model) openSettings() tea.Cmd {
	input := textinput.New()
	input.CharLimit = 2048
	input.SetWidth(max(1, m.width-12))
	m.settings = &settingsState{values: preferences{m.provider, m.theme, m.soundEnabled, m.recentDays}, input: input}
	m.mode = settingsMode
	return nil
}
func (m *Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := m.settings
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.settings = nil
			m.mode = normal
			if s.busy {
				m.status = "Saving settings in background…"
			}
			return *m, nil
		case "ctrl+s":
			if s.busy {
				return *m, nil
			}
			if s.editing {
				if !m.commitSetting() {
					return *m, nil
				}
			}
			values, path, demo := s.values, m.configPath, m.demo
			if values.AI.Enabled {
				if err := values.AI.Validate(); err != nil {
					s.error = err.Error()
					return *m, nil
				}
			}
			s.busy = true
			return *m, func() tea.Msg {
				var err error
				if !demo {
					if path == "" {
						err = fmt.Errorf("no settings file configured")
					} else {
						err = config.SavePreferences(path, values.AI, values.Theme, values.Sound, values.Days)
					}
				}
				return preferencesSaved{values, err}
			}
		}
		if !s.editing {
			switch key.String() {
			case "j", "down", "tab":
				s.row = (s.row + 1) % len(settingLabels)
			case "k", "up", "shift+tab":
				s.row = (s.row + len(settingLabels) - 1) % len(settingLabels)
			case "enter", "space", " ":
				switch s.row {
				case 0:
					s.values.AI.Enabled = !s.values.AI.Enabled
				case 5:
					themes := []string{"mint", "ocean", "amber"}
					for i, t := range themes {
						if t == s.values.Theme {
							s.values.Theme = themes[(i+1)%len(themes)]
							break
						}
					}
				case 6:
					s.values.Sound = !s.values.Sound
				default:
					s.editing = true
					s.input.EchoMode = textinput.EchoNormal
					value := ""
					switch s.row {
					case 1:
						value = s.values.AI.BaseURL
					case 2:
						value = s.values.AI.APIKey
						s.input.EchoMode = textinput.EchoPassword
					case 3:
						value = s.values.AI.ChatModel
					case 4:
						value = s.values.AI.EmbeddingModel
					case 7:
						value = strconv.Itoa(s.values.Days)
					}
					s.input.SetValue(value)
					s.input.CursorEnd()
					return *m, s.input.Focus()
				}
			}
			return *m, nil
		}
		if key.String() == "enter" {
			m.commitSetting()
			return *m, nil
		}
	}
	if s.editing && !s.busy {
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return *m, cmd
	}
	return *m, nil
}
func (m *Model) commitSetting() bool {
	s := m.settings
	value := strings.TrimSpace(s.input.Value())
	switch s.row {
	case 1:
		s.values.AI.BaseURL = value
	case 2:
		s.values.AI.APIKey = value
	case 3:
		s.values.AI.ChatModel = value
	case 4:
		s.values.AI.EmbeddingModel = value
	case 7:
		days, err := strconv.Atoi(value)
		if err != nil || days < 0 || days > 3650 {
			s.error = "Recent days must be 0–3650 (0 shows all)"
			return false
		}
		s.values.Days = days
	}
	s.editing = false
	s.input.Blur()
	s.input.Reset()
	s.error = ""
	return true
}
func (m Model) settingsView() string {
	s := m.settings
	on := func(value bool) string {
		if value {
			return "on"
		}
		return "off"
	}
	key := "not set"
	if s.values.AI.APIKey != "" {
		key = "•••••••• (hidden)"
	}
	values := []string{on(s.values.AI.Enabled), s.values.AI.BaseURL, key, s.values.AI.ChatModel, s.values.AI.EmbeddingModel, s.values.Theme, on(s.values.Sound), strconv.Itoa(s.values.Days)}
	lines := []string{"Enter edits/toggles · Ctrl+S saves · Esc cancels", "Summaries send this conversation’s 14-day text to your configured provider.", ""}
	count := max(1, m.mainHeight()-7)
	if m.mainHeight() < 8 {
		lines = nil
		count = max(1, m.mainHeight()-3)
	}
	start := max(0, s.row-count+1)
	for i := start; i < min(len(settingLabels), start+count); i++ {
		label := settingLabels[i]
		prefix := "  "
		if i == s.row {
			prefix = "▶ "
		}
		value := cleanLine(values[i])
		if value == "" {
			value = "not set"
		}
		if i == s.row && s.editing {
			value = s.input.View()
		}
		line := prefix + label + ": " + value
		if i == s.row {
			line = lipgloss.NewStyle().Foreground(m.accent()).Render(line)
		}
		lines = append(lines, ansi.Truncate(line, max(1, m.width-4), "…"))
	}
	if m.demo {
		lines = append(lines, "", "Demo preferences apply only to this process; inference stays offline.")
	}
	if s.error != "" {
		lines = append(lines, "", cleanLine(s.error))
	}
	if s.busy {
		lines = append(lines, "Saving…")
	}
	return m.paneFrame("SETTINGS", strings.Join(lines, "\n"), m.width, m.mainHeight(), true)
}
