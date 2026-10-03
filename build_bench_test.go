package main

import (
	"math"
	"math/rand"
	"testing"
)

type sectionStats struct {
	bg, fg, vines int
	area          float64
}

func statsOf(s sectionData) sectionStats {
	st := sectionStats{bg: len(s.background), fg: len(s.foreground), vines: len(s.vines)}
	for _, c := range s.foreground {
		st.area += faceArea(c.Polygon)
	}
	return st
}

// Areas compare with a tolerance because contour clipping order can nudge
// polished contours slightly.
func TestSectionStatsStable(t *testing.T) {
	want := map[int64]sectionStats{
		1:  {4809, 2064, 24, 1098988.6594},
		42: {4910, 1948, 26, 993373.1121},
	}
	for seed, w := range want {
		got := statsOf(buildSection(seed, 0))
		if got.bg != w.bg || got.fg != w.fg || got.vines != w.vines || math.Abs(got.area-w.area) > w.area*1e-4 {
			t.Errorf("seed %d: got %+v, want %+v", seed, got, w)
		}
	}
}

func BenchmarkBuildSection(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buildSection(42, int64(i%3))
	}
}

func TestVoronoiCellsMatchReference(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	seeds := worldSeeds(42, -1000, noise)
	cells := voronoiCells(seeds)
	for i := range seeds {
		want := voronoiCell(i, seeds)
		if len(cells[i]) != len(want) {
			t.Fatalf("cell %d: %d vertices, want %d", i, len(cells[i]), len(want))
		}
		for k := range want {
			if cells[i][k].Sub(want[k]).Len() > 1e-6 {
				t.Fatalf("cell %d vertex %d: %v, want %v", i, k, cells[i][k], want[k])
			}
		}
	}
}
