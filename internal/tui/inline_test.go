package tui

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestInlineImageSequenceCarriesTheFileInCells(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\nfake")
	seq := inlineImageSequence(data, 60, 20)
	if !strings.HasPrefix(seq, "\x1b]1337;File=inline=1;") || !strings.HasSuffix(seq, "\a") {
		t.Fatalf("not an iTerm2 inline image sequence: %q", seq)
	}
	if !strings.Contains(seq, "width=60;height=20") || !strings.Contains(seq, base64.StdEncoding.EncodeToString(data)) {
		t.Fatalf("size or data missing: %q", seq)
	}
}

func TestInlineImagesOnlyForSupportingTerminals(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	t.Setenv("LC_TERMINAL", "")
	if !inlineImagesSupported() {
		t.Fatal("Warp not detected")
	}
	t.Setenv("TERM_PROGRAM", "tmux")
	if inlineImagesSupported() {
		t.Fatal("tmux treated as supporting inline images")
	}
}

// The picture is drawn only while the viewer is open with the image loaded.
func TestInlineImageDrawsOnlyWhileViewerIsOpen(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	m := Model{mode: normal}
	if m.inlineImageRaw() != nil {
		t.Fatal("drew a picture with the viewer closed")
	}
	m.mode = imageMode
	m.viewer = []imageRef{{url: "u"}}
	m.images = map[string]*imageEntry{"u": {raw: []byte("x")}}
	if m.inlineImageRaw() == nil {
		t.Fatal("did not draw the open viewer's picture")
	}
}
