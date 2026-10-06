package infinicave

import "testing"

func TestVisibleSectionsAtSeams(t *testing.T) {
	for _, tc := range []struct {
		y         float64
		height    float64
		low, high int64
	}{
		{-.8, .8, 0, 0}, {-1, 1, 0, 0}, {-1.001, .8, 0, 1}, {-2, 1, 1, 1}, {-10.8, .8, 10, 10}, {-1e6, .8, 999999, 999999},
		{-1.0001, .0002, 0, 1}, {-1.0001, .0001, 1, 1},
	} {
		viewport := Viewport{Y: tc.y, Height: tc.height}
		low, high := visibleSections(viewport.Y, viewport.Height)
		if low != tc.low || high != tc.high {
			t.Fatalf("viewport %v/%v: sections %d..%d, want %d..%d", tc.y, tc.height, low, high, tc.low, tc.high)
		}
	}
}
