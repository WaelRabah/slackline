// Package media decodes bounded image previews and renders them with text cells.
// Half-block cells work in Warp and other terminals without a graphics protocol.
package media

import (
	"bytes"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
)

const MaxBytes = 8 << 20
const maxPixels = 16 << 20

func Decode(data []byte) (image.Image, error) {
	if len(data) > MaxBytes {
		return nil, errors.New("image exceeds the 8 MiB preview limit")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, errors.New("image is unsupported or exceeds preview dimensions")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("image preview could not be decoded")
	}
	width, height := fit(cfg.Width, cfg.Height, 256, 256)
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	bounds := source.Bounds()
	for y := range height {
		for x := range width {
			result.Set(x, y, source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
		}
	}
	return result, nil
}

func fit(width, height, maxWidth, maxHeight int) (int, int) {
	scale := math.Min(float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	return max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
}

// Render uses two image pixels per cell and composites transparency over a dark
// background. It emits only generated SGR colours and fixed block characters.
func Render(source image.Image, columns, rows int) string {
	if source == nil || columns < 1 || rows < 1 {
		return ""
	}
	bounds := source.Bounds()
	if bounds.Empty() {
		return ""
	}
	width, height := fit(bounds.Dx(), bounds.Dy(), columns, rows*2)
	pixel := func(x, y int) color.RGBA {
		c := color.NRGBAModel.Convert(source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+min(y, height-1)*bounds.Dy()/height)).(color.NRGBA)
		blend := func(value uint8) uint8 { return uint8((uint32(value)*uint32(c.A) + 16*uint32(255-c.A)) / 255) }
		return color.RGBA{blend(c.R), blend(c.G), blend(c.B), 255}
	}
	var out strings.Builder
	for y := 0; y < height; y += 2 {
		if y > 0 {
			out.WriteByte('\n')
		}
		for x := range width {
			top, bottom := pixel(x, y), pixel(x, y+1)
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.R, top.G, top.B, bottom.R, bottom.G, bottom.B)
		}
		out.WriteString("\x1b[0m")
	}
	return out.String()
}
