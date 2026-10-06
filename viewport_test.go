package infinicave

import "testing"

func TestVisibleSectionsAtSeams(t *testing.T) {
	for _, tc := range []struct {
		y         float64
		height    int
		low, high int64
	}{
		{-800, 800, 0, 0}, {-1000, 1000, 0, 0}, {-1001, 800, 0, 1}, {-2000, 1000, 1, 1}, {-10800, 800, 10, 10}, {-1e9, 800, 999999, 999999},
	} {
		low, high := visibleSections(tc.y, tc.height)
		if low != tc.low || high != tc.high {
			t.Fatalf("viewport %v/%d: sections %d..%d, want %d..%d", tc.y, tc.height, low, high, tc.low, tc.high)
		}
	}
}
