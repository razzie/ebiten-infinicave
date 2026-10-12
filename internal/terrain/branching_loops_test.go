package terrain

import (
	"math"
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestLoopsCreateIrregularRoutesAndSecondaryCycles(t *testing.T) {
	g := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	secondary, bent := 0, 0
	for _, seed := range []int64{1, 2, 3, 4, 5, 42} {
		segs := appendLoops(nil, &g, []Guide{g}, noise, rand.New(rand.NewSource(seed)))
		ends := map[geom.V]bool{}
		positive, negative := 0., 0.
		for i, s := range segs {
			if ends[s.B] {
				secondary++
			}
			ends[s.B] = true
			if i > 0 && segs[i-1].B == s.A {
				u, v := segs[i-1].B.Sub(segs[i-1].A).Norm(), s.B.Sub(s.A).Norm()
				turn := math.Atan2(geom.Cross(u, v), u.Dot(v))
				positive += math.Max(0, turn)
				negative += math.Max(0, -turn)
			}
		}
		if positive > .5 && negative > .5 {
			bent++
		}
		t.Logf("seed %d: segments %d, turn +%g/-%g", seed, len(segs), positive, negative)
	}
	if secondary < 2 {
		t.Fatalf("expected reconnecting side loops, got %d", secondary)
	}
	if bent < 4 {
		t.Fatalf("routes still resemble convex arches: only %d irregular networks", bent)
	}
}

func TestCleanRockJunctionsPreservesLoopWallShape(t *testing.T) {
	poly := []geom.V{{X: .6301976352679215, Y: -.7095029094499972}, {X: .659314623305597, Y: -.7171629058550535}, {X: .6734556475925538, Y: -.6908202201826544}, {X: .6725814915714351, Y: -.6891895292662497}, {X: .6620817520839947, Y: -.6821830796557632}, {X: .6551733721967401, Y: -.6831930501293715}, {X: .6542811946690035, Y: -.6827121449679079}, {X: .6537851933758007, Y: -.6824448662393885}, {X: .6535870822295619, Y: -.6823381165317541}, {X: .6535855170155359, Y: -.6823372731357977}, {X: .653479718503579, Y: -.6822802649493381}}
	grid := RockGrid{{Polygon: poly}}
	area := geom.PolygonArea(poly)
	cleanRockJunctions(grid)
	if len(geom.Triangulate(grid[0].Polygon)) != len(grid[0].Polygon)-2 || math.Abs(area-geom.PolygonArea(grid[0].Polygon)) > 1e-13 {
		t.Fatal("loop wall was not repaired without changing its area")
	}
}
