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
	return boxScale(source, width, height), nil
}

// boxScale averages every source pixel that lands in each destination pixel.
// Point sampling would skip most of a large image and turn detail into noise.
// Colours are weighted by alpha so transparent pixels do not darken edges.
func boxScale(source image.Image, width, height int) *image.NRGBA {
	bounds := source.Bounds()
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		y0 := bounds.Min.Y + y*bounds.Dy()/height
		y1 := max(bounds.Min.Y+(y+1)*bounds.Dy()/height, y0+1)
		for x := range width {
			x0 := bounds.Min.X + x*bounds.Dx()/width
			x1 := max(bounds.Min.X+(x+1)*bounds.Dx()/width, x0+1)
			var r, g, b, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(source.At(sx, sy)).(color.NRGBA)
					alpha := uint64(c.A)
					r += uint64(c.R) * alpha
					g += uint64(c.G) * alpha
					b += uint64(c.B) * alpha
					a += alpha
					n++
				}
			}
			if a == 0 {
				continue
			}
			result.SetNRGBA(x, y, color.NRGBA{R: uint8(r / a), G: uint8(g / a), B: uint8(b / a), A: uint8(a / n)})
		}
	}
	return result
}

// CellAspect is the height-to-width ratio of one terminal cell. Half-block
// rendering packs two image rows into each cell, so the ratio decides how many
// rows the image needs to keep its proportions. Most terminals are about 2.0.
var CellAspect = 2.0

// fitCells scales an image into cols x rows cells while keeping its proportions
// for cells with the given height-to-width ratio. It returns the size of the
// sampling grid: width columns and height half-cell rows.
func fitCells(srcW, srcH, cols, rows int, aspect float64) (int, int) {
	if aspect <= 0 {
		aspect = 2
	}
	maxHeight := float64(rows * 2)
	scale := math.Min(float64(cols)/float64(srcW), maxHeight*aspect/(2*float64(srcH)))
	return max(1, int(math.Round(float64(srcW)*scale))), max(1, int(math.Round(float64(srcH)*scale*2/aspect)))
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
	width, height := fitCells(bounds.Dx(), bounds.Dy(), columns, rows, CellAspect)
	grid := boxScale(source, width, height)
	pixel := func(x, y int) color.RGBA {
		c := grid.NRGBAAt(x, min(y, height-1))
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
