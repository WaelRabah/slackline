package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

// inlineImagesSupported reports whether the terminal draws the iTerm2 inline
// image protocol. Warp and iTerm2 do; other terminals keep the block preview.
func inlineImagesSupported() bool {
	return os.Getenv("TERM_PROGRAM") == "WarpTerminal" || os.Getenv("TERM_PROGRAM") == "iTerm.app" || os.Getenv("LC_TERMINAL") == "iTerm2"
}

// inlineImageSequence returns the iTerm2 inline-image escape for file bytes,
// sized in terminal cells.
func inlineImageSequence(data []byte, cols, rows int) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=1:%s\a", len(data), cols, rows, encoded)
}

// Screen position of the picture inside the image viewer: the frame's top border
// is row 1, the title row 2, the file name row 3, a blank row 4; the picture
// starts on row 5, after the frame's left border and padding in column 3.
const (
	inlineImageRow = 5
	inlineImageCol = 3
)

// inlineDrawMsg asks for the picture to be drawn. It is sent after a short delay,
// so the frame that reserves the picture's blank rows is already on screen.
type inlineDrawMsg struct{}

// inlineImageCmd schedules the picture to be drawn after the next frame.
func (m Model) inlineImageCmd() tea.Cmd {
	if !inlineImagesSupported() || len(m.viewer) == 0 {
		return nil
	}
	return tea.Tick(40*time.Millisecond, func(time.Time) tea.Msg { return inlineDrawMsg{} })
}

// inlineImageRaw draws the current image at its position in the viewer. It does
// nothing once the viewer is closed. The cursor is restored afterwards.
func (m Model) inlineImageRaw() tea.Cmd {
	if !inlineImagesSupported() || m.mode != imageMode || len(m.viewer) == 0 {
		return nil
	}
	entry := m.images[m.viewer[m.imageIndex].url]
	if entry == nil || len(entry.raw) == 0 {
		return nil
	}
	cols, rows := max(1, m.width-6), max(1, m.mainHeight()-7)
	return tea.Raw(fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", inlineImageRow, inlineImageCol, inlineImageSequence(entry.raw, cols, rows)))
}
