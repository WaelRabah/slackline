// Package audio records short microphone clips by delegating capture to ffmpeg.
package audio

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"

	"github.com/WaelRabah/slackline/internal/inference"
)

// Record captures mono 16 kHz WAV audio from the default input device for the
// given number of seconds. Capture uses the terminal's microphone permission on macOS.
func Record(ctx context.Context, seconds int) ([]byte, error) {
	if seconds < 1 || seconds > 60 {
		return nil, errors.New("recording length must be between 1 and 60 seconds")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("ffmpeg not found; install it with `brew install ffmpeg`")
	}
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "avfoundation", "-i", ":default",
		"-t", strconv.Itoa(seconds), "-ac", "1", "-ar", "16000",
		"-f", "wav", "pipe:1",
	)
	var out bytes.Buffer
	// Cap the buffer so a misbehaving capture cannot grow without bound.
	cmd.Stdout = &limitWriter{buf: &out, limit: inference.MaxAudioBytes}
	// ffmpeg stderr can name devices and paths; keep it out of the UI.
	if err := cmd.Run(); err != nil {
		if errors.Is(err, errLimit) {
			return nil, errors.New("recording too large to transcribe; record a shorter clip")
		}
		return nil, errors.New("microphone capture failed; allow microphone access for your terminal in System Settings > Privacy & Security")
	}
	if out.Len() == 0 {
		return nil, errors.New("microphone capture produced no audio")
	}
	return out.Bytes(), nil
}

var errLimit = errors.New("output limit exceeded")

type limitWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		return 0, errLimit
	}
	return w.buf.Write(p)
}
