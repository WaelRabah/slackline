package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// MaxAudioBytes bounds one uploaded clip (about 10 minutes of 16 kHz mono PCM WAV).
const MaxAudioBytes = 20 << 20

// maxTranscriptRunes matches the composer limit so dictated text always fits.
const maxTranscriptRunes = 4000

// Transcribe sends one WAV clip to the OpenAI-compatible /audio/transcriptions
// endpoint configured in settings and returns the recognised text.
func Transcribe(ctx context.Context, settings Settings, audio []byte) (string, error) {
	if !settings.Enabled {
		return "", errors.New("enable AI features in Settings first")
	}
	if strings.TrimSpace(settings.TranscriptionModel) == "" {
		return "", errors.New("set ai.transcription_model in config.toml to enable speech to text")
	}
	if err := settings.validateEndpoint(); err != nil {
		return "", err
	}
	if len(audio) == 0 || len(audio) > MaxAudioBytes {
		return "", errors.New("recording is empty or too large to transcribe")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", settings.TranscriptionModel); err != nil {
		return "", errors.New("cannot build transcription request")
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", errors.New("cannot build transcription request")
	}
	part, err := writer.CreateFormFile("file", "speech.wav")
	if err != nil {
		return "", errors.New("cannot build transcription request")
	}
	if _, err := part.Write(audio); err != nil {
		return "", errors.New("cannot build transcription request")
	}
	if err := writer.Close(); err != nil {
		return "", errors.New("cannot build transcription request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(settings.BaseURL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", errors.New("cannot create transcription request")
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if settings.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+settings.APIKey)
	}
	// Redirects are refused so the bearer key is never sent to another host.
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("transcription request failed; check the provider connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcription provider returned HTTP %d; check Settings", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return "", errors.New("cannot read transcription response or response too large")
	}
	var result struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &result) != nil {
		return "", errors.New("transcription provider returned an unreadable response")
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return "", errors.New("no speech recognised")
	}
	if len([]rune(text)) > maxTranscriptRunes {
		return "", errors.New("transcript exceeded the composer limit")
	}
	return text, nil
}
