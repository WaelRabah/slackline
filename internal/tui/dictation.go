package tui

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/audio"
	"github.com/WaelRabah/slackline/internal/inference"
)

const dictationSeconds = 10

// dictationMsg carries recognised text back to the draft it was dictated into.
type dictationMsg struct {
	key  string
	text string
	err  error
}

// dictate records a short clip from the microphone and transcribes it. The result
// is added to the draft that was active when the key was pressed.
func (m *Model) dictate() tea.Cmd {
	if m.demo {
		m.status = "Speech to text is offline in the demo; configure it in the signed-in app"
		return nil
	}
	if !m.provider.TranscriptionReady() {
		m.status = "Set ai.transcription_model in config.toml and enable AI to use speech to text"
		return nil
	}
	if m.dictating {
		m.status = "Already listening"
		return nil
	}
	key, provider, ctx := m.draftKey(), m.provider, m.ctx
	m.dictating = true
	m.status = "Listening for 10 seconds… speak now"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		clip, err := audio.Record(ctx, dictationSeconds)
		if err != nil {
			return dictationMsg{key: key, err: err}
		}
		text, err := inference.Transcribe(ctx, provider, clip)
		return dictationMsg{key: key, text: text, err: err}
	}
}

func (m Model) finishDictation(msg dictationMsg) Model {
	m.dictating = false
	if msg.err != nil {
		m.setError(msg.err)
		return m
	}
	if m.draftKey() == msg.key {
		next := m.input.Value()
		if next != "" && !strings.HasSuffix(next, " ") {
			next += " "
		}
		next += msg.text
		if utf8.RuneCountInString(next) > m.input.CharLimit {
			m.status = "Dictated text would exceed the message limit; shorten the draft or record again"
			return m
		}
		m.input.SetValue(next)
		m.input.CursorEnd()
		m.drafts[msg.key] = next
		m.status = "Dictation added · review, then Enter to send"
		return m
	}
	// The conversation changed while recording: keep the text in that conversation's draft.
	existing := m.drafts[msg.key]
	if existing != "" && !strings.HasSuffix(existing, " ") {
		existing += " "
	}
	m.drafts[msg.key] = existing + msg.text
	m.status = "Dictation saved to the draft of the conversation you started in"
	return m
}
