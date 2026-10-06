package infinicave

import (
	"math"
	"math/rand"
	"testing"
	"time"
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
		1:  {4809, 2064, 108, 1098988.6594},
		42: {4910, 1948, 110, 993373.1121},
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

func TestSection10GenerationCompletes(t *testing.T) {
	// This section used to spend over a minute repeatedly growing thin
	// fragments into sprawling polygons. Keep a generous deadline so a
	// regression fails without hanging the test suite.
	result := make(chan sectionData, 1)
	go func() { result <- buildSection(42, 10) }()
	select {
	case s := <-result:
		if len(s.background) == 0 || len(s.foreground) == 0 {
			t.Fatal("section 10 generated empty terrain")
		}
		for _, c := range s.foreground {
			if len(c.Polygon) < 3 || faceArea(c.Polygon) <= 0 || len(faceTriangles(c.Polygon)) != len(c.Polygon)-2 {
				t.Fatal("section 10 generated an invalid foreground face")
			}
		}
	case <-time.After(20 * time.Second):
		t.Fatal("section 10 generation stalled")
	}
}

func BenchmarkBuildSection10(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buildSection(42, 10)
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
