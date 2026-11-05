package tui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/slack"
)

// imageOpenedMsg reports whether an image was handed to the system viewer.
type imageOpenedMsg struct{ err error }

// openImageExternally fetches the full-size image and opens it in the system
// viewer (Preview on macOS). Terminal previews are limited by cell size; the
// system viewer shows the picture at its real resolution.
func (m Model) openImageExternally() tea.Cmd {
	if len(m.viewer) == 0 {
		return nil
	}
	ref := m.viewer[m.imageIndex]
	api, ok := m.api.(slack.ImageAPI)
	if !ok || m.demo {
		return func() tea.Msg { return imageOpenedMsg{err: errors.New("images cannot be opened in this session")} }
	}
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		data, err := api.Image(ctx, ref.url)
		if err != nil {
			return imageOpenedMsg{err: err}
		}
		path, err := writeImageFile(data)
		if err != nil {
			return imageOpenedMsg{err: err}
		}
		if runtime.GOOS != "darwin" {
			return imageOpenedMsg{err: errors.New("opening images in an external viewer is only supported on macOS")}
		}
		return imageOpenedMsg{err: exec.Command("/usr/bin/open", path).Run()}
	}
}

// writeImageFile stores image bytes in a private temporary directory with an
// extension the system viewer recognises.
func writeImageFile(data []byte) (string, error) {
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[http.DetectContentType(data)]
	if ext == "" {
		return "", errors.New("the image format is not supported by the viewer")
	}
	dir, err := os.MkdirTemp("", "slackline-images-")
	if err != nil {
		return "", errors.New("cannot create a temporary folder for the image")
	}
	path := dir + "/image" + ext
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", errors.New("cannot write the image")
	}
	return path, nil
}
