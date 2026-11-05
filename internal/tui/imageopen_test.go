package tui

import (
	"os"
	"strings"
	"testing"
)

func TestWriteImageFileUsesDetectedExtension(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("0", 64))
	path, err := writeImageFile(png)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(path[:strings.LastIndex(path, "/")])
	if !strings.HasSuffix(path, ".png") {
		t.Fatalf("wrong extension: %s", path)
	}
	if _, err := writeImageFile([]byte("<html>not an image</html>")); err == nil {
		t.Fatal("accepted a non-image")
	}
}
