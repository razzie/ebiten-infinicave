package terrain

import "testing"

func TestForegroundVineLengths(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		roots, length, longest := 0, 0.0, 0.0
		for _, seed := range []int64{7, 42} {
			for _, id := range []int64{0, -1} {
				data := NewSectionBuilder(seed, nil, mode).Build(id)
				for _, v := range data.ForegroundVines {
					if v.Parent >= 0 {
						continue
					}
					if !surfaceVineLongEnough(v) {
						t.Fatalf("mode %v seed %d section %d retained a short isolated vine", mode, seed, id)
					}
					n, _ := v.extent()
					roots++
					length += n
					longest = max(longest, n)
				}
			}
		}
		mean := length / float64(roots)
		t.Logf("mode %v: %d roots, mean length %.4f, longest %.4f", mode, roots, mean, longest)
		if roots < 8 || mean < .20 || longest < .35 {
			t.Fatalf("mode %v: top-cell avoidance shortened the main vines too much", mode)
		}
	}
}
