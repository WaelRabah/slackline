package media

import (
	"image"
	"image/color"
	"testing"
)

func TestBoxScaleAveragesDetailInsteadOfSkippingIt(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, value := range []uint8{0, 255, 0, 255} {
		source.SetNRGBA(x, 0, color.NRGBA{value, value, value, 255})
	}
	got := boxScale(source, 1, 1).NRGBAAt(0, 0)
	if got.R < 126 || got.R > 128 || got.A != 255 {
		t.Fatalf("expected mid grey from alternating stripes, got %+v", got)
	}
}

func TestBoxScaleWeightsColourByAlpha(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(1, 0, color.NRGBA{255, 255, 255, 255})
	got := boxScale(source, 1, 1).NRGBAAt(0, 0)
	if got.R != 255 || got.G != 255 || got.B != 255 {
		t.Fatalf("transparent pixel darkened the average: %+v", got)
	}
}

func TestBoxScaleKeepsFullyTransparentPixelsEmpty(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	got := boxScale(source, 1, 1).NRGBAAt(0, 0)
	if got.A != 0 {
		t.Fatalf("expected transparent output, got %+v", got)
	}
}
