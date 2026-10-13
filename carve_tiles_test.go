package infinicave

import (
	"image"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestDirtyTilesMergeDenseRegionsWithoutFillingGaps(t *testing.T) {
	dst := ebiten.NewImage(320, 256)
	defer dst.Deallocate()
	bounds := image.Rect(0, 320, 320, 640)
	region := func(r image.Rectangle) geom.Bounds {
		return geom.Bounds{Min: V{X: float64(r.Min.X) / 320, Y: float64(r.Min.Y) / 320}, Max: V{X: float64(r.Max.X) / 320, Y: float64(r.Max.Y) / 320}}
	}
	for _, tc := range []struct {
		name    string
		regions []geom.Bounds
		want    []image.Rectangle
	}{
		{"dense", []geom.Bounds{region(image.Rect(1, 1, 250, 190))}, []image.Rectangle{image.Rect(0, 0, 256, 192)}},
		{"gap", []geom.Bounds{region(image.Rect(1, 1, 63, 127)), region(image.Rect(193, 1, 255, 127))}, []image.Rectangle{image.Rect(0, 0, 64, 128), image.Rect(192, 0, 256, 128)}},
		{"step", []geom.Bounds{region(image.Rect(1, 1, 127, 127)), region(image.Rect(65, 129, 127, 190))}, []image.Rectangle{image.Rect(0, 0, 128, 128), image.Rect(64, 128, 128, 192)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dirtyTiles(dst, bounds, -1, 320, tc.regions)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d replay rectangles, want %d", len(got), len(tc.want))
			}
			for i, dirty := range got {
				if r := dirtyRaster(bounds, -1, 320, dirty, dst); r != tc.want[i] {
					t.Fatalf("replay rectangle %d = %v, want %v", i, r, tc.want[i])
				}
			}
		})
	}
}
