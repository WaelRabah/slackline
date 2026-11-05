package media

import (
	"math"
	"testing"
)

// A square image must stay square on screen for any cell shape: physical width
// is columns * cellWidth, physical height is (rows * 2 half-cells) * cellHeight / 2.
func TestFitCellsKeepsProportionsForCellShape(t *testing.T) {
	for _, aspect := range []float64{1.8, 2.0, 2.4} {
		for _, dims := range [][2]int{{100, 100}, {400, 100}, {100, 400}} {
			srcW, srcH := dims[0], dims[1]
			cols, rows := 60, 20
			w, h := fitCells(srcW, srcH, cols, rows, aspect)
			physicalW := float64(w)
			physicalH := float64(h) / 2 * aspect
			want := float64(srcW) / float64(srcH)
			got := physicalW / physicalH
			if math.Abs(got-want)/want > 0.06 {
				t.Fatalf("aspect %.1f source %v: proportion %.3f, want %.3f (grid %dx%d)", aspect, dims, got, want, w, h)
			}
			if w > cols || h > rows*2 {
				t.Fatalf("grid %dx%d exceeds %dx%d cells", w, h, cols, rows)
			}
		}
	}
}
