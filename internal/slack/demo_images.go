package slack

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
)

// Image returns generated fixtures only: the offline demo never fetches URLs.
func (*Demo) Image(_ context.Context, raw string) ([]byte, error) {
	if raw != "demo://landscape" {
		return nil, errors.New("offline demo image is unavailable")
	}
	scene := image.NewNRGBA(image.Rect(0, 0, 96, 48))
	for y := range 48 {
		for x := range 96 {
			shade := color.NRGBA{uint8(245 - y*2), uint8(110 + y*2), uint8(90 + y*3), 255}
			if (x-70)*(x-70)+(y-14)*(y-14) < 64 {
				shade = color.NRGBA{255, 225, 140, 255}
			}
			ridge := 29 + (x%32-16)*(x%32-16)/32
			if y > ridge {
				shade = color.NRGBA{30, uint8(65 + y), 100, 255}
			}
			if y > 40 {
				shade = color.NRGBA{45, 150, uint8(150 + y), 255}
			}
			scene.SetNRGBA(x, y, shade)
		}
	}
	var data bytes.Buffer
	err := png.Encode(&data, scene)
	return data.Bytes(), err
}
