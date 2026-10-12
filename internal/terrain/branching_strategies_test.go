package terrain

import (
	"context"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBranchingStrategiesDeterministicAndConnected(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}
	guides[0].Seed = 42
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			segs := generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), strategy)
			if len(segs) == 0 || !reflect.DeepEqual(segs, generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), strategy)) {
				t.Fatal("empty or nondeterministic branches")
			}
			field := newShapedBranchField(segs, strategy)
			ends := map[geom.V]bool{}
			for _, seg := range segs {
				if !ends[seg.A] && reliefHeight(seg.A, guides, noise, nil) <= rockContourHeight {
					t.Fatalf("detached root %v", seg.A)
				}
				for _, u := range []float64{0, .5, 1} {
					p := geom.LerpVector(seg.A, seg.B, u)
					if reliefHeight(p, guides, noise, field) <= rockContourHeight {
						t.Fatalf("invisible centerline %v", p)
					}
				}
				ends[seg.B] = true
			}
		})
	}
	simple := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	if !reflect.DeepEqual(simple, generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), BranchingSimple)) {
		t.Fatal("simple strategy changed")
	}
	recursive := generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), BranchingRecursive)
	furthest := func(segs []BranchSegment) float64 {
		d := 0.
		for _, s := range segs {
			d = math.Max(d, s.B.Y-.3)
		}
		return d
	}
	if furthest(recursive) < furthest(simple)+.08 {
		t.Fatalf("recursive limbs are not longer: simple %g recursive %g", furthest(simple), furthest(recursive))
	}
	// Junction ancestry counts successive generations, rather than merely
	// counting many forks attached independently to the original guide.
	generations := map[geom.V]int{}
	children := map[geom.V]int{}
	maxGeneration := 0
	for _, s := range recursive {
		children[s.A]++
	}
	for _, s := range recursive {
		generation := generations[s.A]
		if children[s.A] > 1 {
			generation++
		}
		generations[s.B] = generation
		maxGeneration = max(maxGeneration, generation)
	}
	if maxGeneration < 2 {
		t.Fatalf("no secondary branching: %d generations", maxGeneration)
	}
}

func TestLoopsProducesCollisionHoles(t *testing.T) {
	loader := func(id int64) SectionContent {
		if id != 0 {
			return SectionContent{}
		}
		return SectionContent{Guides: []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}}
	}
	builder := NewSectionBuilder(42, loader)
	builder.Shape = DefaultShapeOptions()
	builder.Shape.BranchingStrategy = BranchingLoops
	build := builder.Begin(context.Background(), 0)
	geometry := PrepareTerrainGeometry(build.Data(), 0).Collision
	// Collision can represent a degenerate union as disjoint faces. Flood
	// empty space using Contains so the check covers that representation too.
	const side = 200
	empty, visited := make([]bool, side*side), make([]bool, side*side)
	var queue []int
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			i := y*side + x
			empty[i] = !geometry.Contains(geom.V{X: (float64(x) + .5) / side, Y: geometry.Top + (float64(y)+.5)/side})
			if empty[i] && (x == 0 || y == 0 || x == side-1 || y == side-1) {
				queue = append(queue, i)
				visited[i] = true
			}
		}
	}
	for at := 0; at < len(queue); at++ {
		i := queue[at]
		for _, next := range []int{i - 1, i + 1, i - side, i + side} {
			if next >= 0 && next < len(empty) && empty[next] && !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	interior := 0
	for i := range empty {
		if empty[i] && !visited[i] {
			interior++
		}
	}
	if interior < 100 {
		t.Fatalf("loops did not leave a substantial enclosed collision opening: %d empty samples", interior)
	}
}

func TestLoopsBridgesStopAtGuideShadows(t *testing.T) {
	g := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	blocker := SplineGuide([]geom.V{{X: .1, Y: .45}, {X: .9, Y: .45}}, 1)
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	open := appendLoops(nil, &g, []Guide{g}, noise, rand.New(rand.NewSource(42)))
	blocked := appendLoops(nil, &g, []Guide{g, blocker}, noise, rand.New(rand.NewSource(42)))
	if len(open) == 0 || len(blocked) != 0 {
		t.Fatalf("bridges should commit only complete unmasked routes: open=%d blocked=%d", len(open), len(blocked))
	}
}

func TestBranchStrategiesKeepWorldCoordinatesAcrossWindows(t *testing.T) {
	original := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	original.Seed = 42
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		noise := NewPerlin(rand.New(rand.NewSource(42)))
		a := generateBranchesWithStrategy([]Guide{original}, noise, rand.New(rand.NewSource(42)), strategy)
		shifted := shiftedGuide(original, 1)
		noise.OffsetY = -1
		b := generateBranchesWithStrategy([]Guide{shifted}, noise, rand.New(rand.NewSource(42)), strategy)
		if len(a) != len(b) {
			t.Fatalf("%v: segment count depends on window", strategy)
		}
		for i, s := range a {
			if s.A.Sub(b[i].A.Add(geom.V{Y: -1})).Len() > 1e-9 || s.B.Sub(b[i].B.Add(geom.V{Y: -1})).Len() > 1e-9 || math.Abs(s.WidthA-b[i].WidthA) > 1e-9 {
				t.Fatalf("%v: branch changed between windows", strategy)
			}
		}
	}
}

func TestAllBranchStrategiesProduceValidRoughRockFaces(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
			builder := NewSectionBuilder(42, nil, orientation)
			builder.ForceGuidesUp = true
			builder.Shape = ShapeOptions{BranchingStrategy: strategy, SpikeDensity: 1, EdgeRoughness: 1}
			build := builder.Begin(context.Background(), 0)
			if len(build.Data().Foreground) == 0 {
				t.Fatalf("%v/%v generated empty terrain", orientation, strategy)
			}
			for _, c := range build.Data().Foreground {
				if geom.PolygonArea(c.Polygon) <= 0 || !simpleRockOutline(c.Polygon) || len(geom.Triangulate(c.Polygon)) != len(c.Polygon)-2 {
					t.Fatalf("%v/%v generated an invalid chipped face: area=%g triangles=%d points=%v", orientation, strategy, geom.PolygonArea(c.Polygon), len(geom.Triangulate(c.Polygon)), c.Polygon)
				}
				for _, p := range c.Polygon {
					if p.X < 0 || p.X > 1 || p.Y < GenerationMinY || p.Y > generationMaxY {
						t.Fatalf("%v/%v grew outside the window: %v", orientation, strategy, p)
					}
				}
			}
		}
	}
}
