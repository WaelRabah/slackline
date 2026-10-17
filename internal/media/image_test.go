package media

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestImageColoursDimensionsAndTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	source.SetNRGBA(0, 1, color.NRGBA{0, 0, 255, 255})
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	result := Render(decoded, 2, 1)
	for _, fragment := range []string{"\x1b[38;2;255;0;0m", "\x1b[48;2;0;0;255m", "38;2;16;16;16m"} {
		if !strings.Contains(result, fragment) {
			t.Errorf("missing colour %q", fragment)
		}
	}
	if lipgloss.Width(result) != 2 || lipgloss.Height(result) != 1 || ansi.Strip(result) != "▀▀" {
		t.Fatal("preview dimensions changed")
	}
	for _, dimensions := range [][2]int{{1, 1}, {6, 3}, {80, 20}} {
		got := Render(decoded, dimensions[0], dimensions[1])
		if lipgloss.Width(got) > dimensions[0] || lipgloss.Height(got) > dimensions[1] {
			t.Fatal("preview exceeded its rectangle")
		}
	}
}
func TestDecodeRejectsOversizedAndNonImagesBeforeAllocation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var data bytes.Buffer
	_ = png.Encode(&data, source)
	huge := append([]byte{}, data.Bytes()...)
	binary.BigEndian.PutUint32(huge[16:20], 8000)
	binary.BigEndian.PutUint32(huge[20:24], 8000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	for _, input := range [][]byte{[]byte("<html>login</html>"), make([]byte, MaxBytes+1), huge} {
		if _, err := Decode(input); err == nil {
			t.Fatal("accepted invalid or excessive image")
		}
	}
}
