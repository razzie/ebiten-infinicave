package terrain

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBranchThicknessControlsAllStrategies(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}
	guides[0].Seed = 42
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			generate := func(lo, hi float64) []BranchSegment {
				shape := ShapeOptions{BranchingStrategy: strategy, BranchMinThickness: lo, BranchMaxThickness: hi}
				return generateBranchesWithOptions(guides, noise, rand.New(rand.NewSource(42)), shape)
			}
			for _, thickness := range []float64{.052, .072} {
				segs := generate(thickness, thickness)
				if len(segs) == 0 {
					t.Fatal("no branches")
				}
				if math.Abs(segs[0].WidthA*2-thickness) > 1e-12 {
					t.Fatalf("root thickness %g, want %g", segs[0].WidthA*2, thickness)
				}
				for _, seg := range segs {
					if seg.WidthA*2 > thickness+1e-12 || seg.WidthB*2 > thickness+1e-12 {
						t.Fatalf("thickness exceeds maximum: %+v", seg)
					}
					if strategy == BranchingLoops && (math.Abs(seg.WidthA*2-thickness) > 1e-12 || math.Abs(seg.WidthB*2-thickness) > 1e-12) {
						t.Fatal("fixed loop thickness changed")
					}
				}
			}
			segs := generate(.052, .072)
			if !reflect.DeepEqual(segs, generate(.052, .072)) {
				t.Fatal("thickness is nondeterministic")
			}
			minRoot, maxRoot := math.Inf(1), 0.0
			ends := map[geom.V]bool{}
			for _, seg := range segs {
				// Trunks/forks start at fresh A points or existing junctions. Loop
				// profiles have no tip taper, so all their samples must stay in range.
				if !ends[seg.A] || strategy == BranchingLoops {
					minRoot = math.Min(minRoot, seg.WidthA*2)
					maxRoot = math.Max(maxRoot, seg.WidthA*2)
					if seg.WidthA*2 < .052-1e-12 || seg.WidthA*2 > .072+1e-12 {
						t.Fatalf("nominal thickness out of range: %g", seg.WidthA*2)
					}
				}
				ends[seg.B] = true
			}
			if maxRoot-minRoot < .001 {
				t.Fatalf("no thickness variation: %g to %g", minRoot, maxRoot)
			}
		})
	}
}

func TestBranchNegativePointsSuppressShouldersAndPreserveConnections(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}
	guides[0].Seed = 42
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			segs := generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), strategy)
			plain, shaped := newBranchField(segs), newShapedBranchField(segs, strategy)
			if strategy == BranchingSimple {
				if shaped.negative != nil {
					t.Fatal("simple branches have negative points")
				}
				return
			}
			if len(shaped.negative.segs) == 0 {
				t.Fatal("missing negative points")
			}
			for _, seg := range segs {
				for _, u := range []float64{0, .5, 1} {
					p := geom.LerpVector(seg.A, seg.B, u)
					if reliefHeight(p, guides, noise, shaped) != reliefHeight(p, guides, noise, plain) {
						t.Fatalf("negative point weakened centerline %v", p)
					}
				}
			}
			removed := 0
			for y := .3; y < .9; y += .005 {
				for x := .1; x < .9; x += .005 {
					p := geom.V{X: x, Y: y}
					a, b := reliefHeight(p, guides, noise, plain), reliefHeight(p, guides, noise, shaped)
					if b > a+1e-12 {
						t.Fatal("negative point added rock")
					}
					if a > rockContourHeight && b < rockContourHeight {
						removed++
					}
				}
			}
			if removed < 20 {
				t.Fatalf("negative points did not create open space: %d removed samples", removed)
			}
		})
	}
}

func TestNegativePointInfluenceFallsNearBranches(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	p := geom.V{X: .5, Y: .5}
	field := &BranchField{negative: newBranchField([]BranchSegment{{A: p, B: p, WidthA: .04, WidthB: .04, LightA: .028, LightB: .028}})}
	previous := math.Inf(1)
	for _, positive := range []float64{0, .12, .25, .38, .5} {
		influence := branchNegativeBias(p, field, positive, 0, noise)
		if influence >= previous && positive < .5 {
			t.Fatalf("negative influence does not fade toward a branch: %g >= %g", influence, previous)
		}
		previous = influence
	}
	if previous != 0 {
		t.Fatal("branch core was not protected")
	}
	if branchNegativeBias(p, field, 0, .06, noise) != 0 {
		t.Fatal("guide crest was not protected")
	}
}

func TestNegativePointsKeepWorldCoordinatesAcrossWindows(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	guide.Seed = 42
	for _, strategy := range []BranchingStrategy{BranchingRecursive, BranchingLoops} {
		noise := NewPerlin(rand.New(rand.NewSource(42)))
		first := newShapedBranchField(generateBranchesWithStrategy([]Guide{guide}, noise, rand.New(rand.NewSource(42)), strategy), strategy)
		shifted := shiftedGuide(guide, 1)
		noise.OffsetY = -1
		second := newShapedBranchField(generateBranchesWithStrategy([]Guide{shifted}, noise, rand.New(rand.NewSource(42)), strategy), strategy)
		if len(first.negative.segs) != len(second.negative.segs) {
			t.Fatal("point count depends on window")
		}
		for i, point := range first.negative.segs {
			other := second.negative.segs[i]
			if point.A.Sub(other.A.Add(geom.V{Y: -1})).Len() > 1e-9 || math.Abs(point.WidthA-other.WidthA) > 1e-9 {
				t.Fatal("negative points changed across windows")
			}
		}
		for x := .1; x < .9; x += .017 {
			for y := .303; y < .9; y += .019 {
				p := geom.V{X: x, Y: y}
				noise.OffsetY = 0
				a := reliefHeight(p, []Guide{guide}, noise, first)
				noise.OffsetY = -1
				b := reliefHeight(p.Add(geom.V{Y: 1}), []Guide{shifted}, noise, second)
				if math.Abs(a-b) > 1e-9 {
					t.Fatalf("%v: negative relief changed across windows at %v: %g vs %g", strategy, p, a, b)
				}
			}
		}
	}
}
