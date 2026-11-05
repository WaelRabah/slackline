package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
)

// messageSoundName is the notification sound set in the Slack app (Preferences >
// Notifications > Sound). Slack ships the file inside its app bundle, so the
// sound is read from the locally installed Slack at runtime and never copied
// into this repository.
const messageSoundName = "knock_brush.mp3"

// systemFallbackSound is used when the Slack app is not installed.
const systemFallbackSound = "/System/Library/Sounds/Glass.aiff"

// slackSoundDirs are the places the Slack app bundle may be installed.
var slackSoundDirs = func() []string {
	dirs := []string{"/Applications/Slack.app/Contents/Resources"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications/Slack.app/Contents/Resources"))
	}
	return dirs
}()

// slackSoundPath returns the Slack sound file under dir, if it exists.
func slackSoundPath(dir, name string) (string, bool) {
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

// chimeSource reports which sound file the chime plays and where it comes from.
func chimeSource() (path, source string) {
	for _, dir := range slackSoundDirs {
		if path, ok := slackSoundPath(dir, messageSoundName); ok {
			return path, "Slack " + messageSoundName
		}
	}
	return systemFallbackSound, "macOS Glass (Slack not installed)"
}

// playSound plays a sound file with afplay. Off macOS it rings the terminal bell.
func playSound(ctx context.Context, path string) tea.Cmd {
	if runtime.GOOS != "darwin" {
		return tea.Raw("\a")
	}
	return func() tea.Msg {
		soundCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		err := exec.CommandContext(soundCtx, "/usr/bin/afplay", path).Run()
		return soundDoneMsg{fallback: err != nil && ctx.Err() == nil}
	}
}
