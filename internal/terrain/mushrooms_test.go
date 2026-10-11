package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestMushroomDensityPreservesSurvivingClusters(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		fullCount, sparseCount := 0, 0
		for seed := int64(1); seed <= 10; seed++ {
			guide := SplineGuide([]geom.V{{X: .3, Y: .9}, {X: .3, Y: .1}}, 1)
			guide.Seed = seed
			guides := []Guide{guide}
			ground := RockGrid{terrainRect(.3, .05, .4, .9)}
			full := MushroomsForGuides(guides, ground, 1, mode)
			fullCount += len(full)
			if len(full) < 2 {
				t.Fatal("fixture generated too few clusters to check density")
			}
			var previous []MushroomGroup
			for _, density := range []float64{0, .25, .5, .75, 1} {
				groups := MushroomsForGuides(guides, ground, density, mode)
				if density == .5 {
					sparseCount += len(groups)
				}
				if density == 0 && len(groups) != 0 {
					t.Fatal("zero density generated mushrooms")
				}
				if !reflect.DeepEqual(groups, MushroomsForGuides(guides, ground, density, mode)) {
					t.Fatal("density selection is not deterministic")
				}
				contains := func(haystack []MushroomGroup, needle MushroomGroup) bool {
					for _, group := range haystack {
						if reflect.DeepEqual(group, needle) {
							return true
						}
					}
					return false
				}
				for _, group := range groups {
					if !contains(full, group) {
						t.Fatal("density changed cluster geometry or placement")
					}
				}
				for _, group := range previous {
					if !contains(groups, group) {
						t.Fatal("increasing density removed a surviving cluster")
					}
				}
				previous = groups
			}
		}
		if sparseCount == 0 || sparseCount >= fullCount {
			t.Fatalf("mode %v: half density retained %d of %d clusters", mode, sparseCount, fullCount)
		}
	}
}

func TestMushroomSpacingRejectsCrossingStalksWithSeparateRoots(t *testing.T) {
	a := Mushroom{CapWidth: .01, Stem: []geom.V{{X: .2, Y: .5}, {X: .3, Y: .4}, {X: .4, Y: .3}}}
	b := Mushroom{CapWidth: .015, Stem: []geom.V{{X: .4, Y: .5}, {X: .3, Y: .4}, {X: .2, Y: .3}}}
	if !mushroomStemsOverlap(a, []Mushroom{b}) {
		t.Fatal("widely spaced roots allowed crossing stalks")
	}
	// Keep the roots separate but move one curve clear of the other.
	for i := range b.Stem {
		b.Stem[i].X += .3
	}
	if mushroomStemsOverlap(a, []Mushroom{b}) {
		t.Fatal("separate stalks were rejected")
	}
}

func TestMushroomStalksKeepClearanceAcrossGuides(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		for seed := int64(1); seed <= 30; seed++ {
			// Close parallel ledges force spacing checks across different guides.
			var guides []Guide
			var ground RockGrid
			for i := 0; i < 2; i++ {
				x := .3 + float64(i)*.001
				g := SplineGuide([]geom.V{{X: x, Y: .9}, {X: x, Y: .1}}, 1)
				g.Seed = seed + int64(i)
				guides = append(guides, g)
				ground = append(ground, terrainRect(x, .05, .4, .9))
			}
			groups := MushroomsForGuides(guides, ground, DefaultMushroomDensity, mode)
			var all []Mushroom
			for _, group := range groups {
				if len(group.Mushrooms) < 3 || len(group.Mushrooms) > 5 {
					t.Fatalf("seed %d: invalid cluster size %d", seed, len(group.Mushrooms))
				}
				all = append(all, group.Mushrooms...)
			}
			if len(all) < 6 {
				t.Fatalf("seed %d mode %v: spacing eliminated too many clusters (%d mushrooms)", seed, mode, len(all))
			}
			for i, a := range all {
				for _, b := range all[i+1:] {
					minDistance := math.Inf(1)
					for j := 1; j < len(a.Stem); j++ {
						for k := 1; k < len(b.Stem); k++ {
							minDistance = math.Min(minDistance, math.Sqrt(geom.SegmentDistanceSquared(a.Stem[j-1], a.Stem[j], b.Stem[k-1], b.Stem[k])))
						}
					}
					if gap := minDistance - (a.CapWidth+b.CapWidth)*.19/2; gap < .0006-1e-12 {
						t.Fatalf("seed %d mode %v: stalks overlap or crowd each other (gap %g)", seed, mode, gap)
					}
				}
			}
			if again := MushroomsForGuides(guides, ground, DefaultMushroomDensity, mode); !reflect.DeepEqual(groups, again) {
				t.Fatalf("seed %d mode %v: spaced mushrooms are not deterministic", seed, mode)
			}
		}
	}
}

func TestHorizontalMushroomsAndLighting(t *testing.T) {
	// In this section frame, a vertical guide maps to a horizontal ledge.
	guide := SplineGuide([]geom.V{{X: .5, Y: .1}, {X: .5, Y: .9}}, -1)
	guide.Seed = 42
	ground := RockGrid{terrainRect(.5, .05, .4, .9)}
	groups := MushroomsForGuides([]Guide{guide}, ground, DefaultMushroomDensity, Horizontal)
	count := 0
	for _, group := range groups {
		for _, m := range group.Mushrooms {
			count++
			root, cap := WorldPoint(Horizontal, m.Stem[0]), WorldPoint(Horizontal, m.CapCenter)
			if cap.Y >= root.Y || WorldPoint(Horizontal, m.RootDirection).Y > 0 || !MushroomWithinForegroundInset(m) {
				t.Fatal("horizontal mushroom does not grow upward inside the bounded strip")
			}
		}
	}
	if count == 0 {
		t.Fatal("horizontal ledge generated no upright mushrooms")
	}
	up, down := InternalPoint(Horizontal, geom.V{X: 0, Y: -1}), InternalPoint(Horizontal, geom.V{X: 0, Y: 1})
	if SurfaceLight(geom.V3{X: up.X, Y: up.Y, Z: 1}, Horizontal) <= SurfaceLight(geom.V3{X: down.X, Y: down.Y, Z: 1}, Horizontal) {
		t.Fatal("landscape lighting does not illuminate world-up faces")
	}
}

func TestMushroomsFollowGuidesAndKeepCapsUpright(t *testing.T) {
	guides := []Guide{
		SplineGuide([]geom.V{{X: 0.1, Y: 0.3}, {X: 0.45, Y: 0.3}, {X: 0.85, Y: 0.3}}, 1),
		SplineGuide([]geom.V{{X: 0.15, Y: 0.75}, {X: 0.15, Y: 0.45}, {X: 0.15, Y: 0.18}}, 1),
	}
	for i := range guides {
		guides[i].Seed = int64(i + 1)
	}
	ground := RockGrid{
		{Center: geom.V{X: 0.5, Y: 0.45}, Polygon: []geom.V{{X: 0.018, Y: 0.3}, {X: 0.982, Y: 0.3}, {X: 0.982, Y: 0.6}, {X: 0.018, Y: 0.6}}},
		{Center: geom.V{X: 0.325, Y: 0.45}, Polygon: []geom.V{{X: 0.15, Y: 0.018}, {X: 0.5, Y: 0.018}, {X: 0.5, Y: 0.982}, {X: 0.15, Y: 0.982}}},
	}
	groups := MushroomsForGuides(guides, ground, DefaultMushroomDensity)
	if len(groups) < 4 {
		t.Fatalf("expected mushroom groups along both guides, got %d", len(groups))
	}
	var mushrooms []Mushroom
	for _, group := range groups {
		if len(group.Mushrooms) < 3 {
			t.Fatalf("small cluster has %d mushrooms", len(group.Mushrooms))
		}
		mushrooms = append(mushrooms, group.Mushrooms...)
	}
	widths := make(map[float64]bool)
	groundBounds := mushroomGroundFromCells(ground)
	for _, mushroom := range mushrooms {
		if len(mushroom.Stem) < 3 || mushroom.CapWidth <= 0 || mushroom.CapHeight <= 0 {
			t.Fatal("invalid mushroom geometry")
		}
		guideIndex, projection := nearestGuide(mushroom.Anchor, guides)
		rootSegment := mushroom.Stem[1].Sub(mushroom.Stem[0]).Norm()
		if guideIndex < 0 || projection.Dist > 1e-9 || mathAbs(mushroom.RootDirection.Dot(projection.T)) > 1e-6 || mathAbs(rootSegment.Dot(projection.T)) > .2 {
			t.Fatalf("stem root is not perpendicular to its guide: root=%+v dist=%.6g direction=%+v tangent=%+v segmentDot=%.6g", mushroom.Anchor, projection.Dist, mushroom.RootDirection, projection.T, rootSegment.Dot(projection.T))
		}
		if !mushroomOnGround(mushroom.Anchor, groundBounds) {
			t.Fatal("mushroom root is not supported by a foreground contour")
		}
		if !MushroomWithinForegroundInset(mushroom) {
			t.Fatal("mushroom extends beyond the visible foreground inset")
		}
		for _, p := range mushroom.Stem {
			if p.Sub(mushroom.Anchor).Dot(mushroom.RootDirection) < -MushroomSink-1e-9 {
				t.Fatal("mushroom stem crosses to the rock side of its guide")
			}
		}
		capBack := mushroom.CapWidth*.5*mathAbs(mushroom.RootDirection.X) + mushroom.CapHeight*(1.18*max(0, mushroom.RootDirection.Y)+.02*max(0, -mushroom.RootDirection.Y))
		capAir := mushroom.CapCenter.Sub(mushroom.Anchor).Dot(mushroom.RootDirection) - capBack
		if capAir <= 0 {
			t.Fatal("mushroom cap crosses to the rock side of its guide")
		}
		if mushroom.CapCenter.Y >= mushroom.Stem[len(mushroom.Stem)-1].Y || mushroom.CapWidth <= mushroom.CapHeight {
			t.Fatal("cap does not sit above the curved stem in an upright orientation")
		}
		widths[mushroom.CapWidth] = true
	}
	if len(widths) < 3 {
		t.Fatal("mushrooms do not vary in size")
	}
	if again := MushroomsForGuides(guides, ground, DefaultMushroomDensity); !reflect.DeepEqual(groups, again) {
		t.Fatal("mushroom groups are not deterministic")
	}
}

func TestMushroomsSkipUnsupportedAndScreenEdgeGuides(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: 0.004, Y: 0.3}, {X: 0.4, Y: 0.3}}, 1)
	guide.Seed = 42
	if groups := MushroomsForGuides([]Guide{guide}, nil, DefaultMushroomDensity); len(groups) != 0 {
		t.Fatal("mushrooms were generated without foreground support")
	}
	ground := RockGrid{{Center: geom.V{X: 0.2, Y: 0.3}, Polygon: []geom.V{{X: 0.018, Y: 0.28}, {X: 0.3, Y: 0.28}, {X: 0.3, Y: 0.32}, {X: 0.018, Y: 0.32}}}}
	if groups := MushroomsForGuides([]Guide{guide}, ground, DefaultMushroomDensity); len(groups) != 0 {
		t.Fatal("mushrooms were generated along an unsupported screen-edge section")
	}
	guide = SplineGuide([]geom.V{{X: 0.1, Y: 0.3}, {X: 0.5, Y: 0.3}, {X: 0.9, Y: 0.3}}, -1)
	guide.Seed = 43
	ground = RockGrid{{Center: geom.V{X: 0.5, Y: 0.3}, Polygon: []geom.V{{X: 0.018, Y: 0.28}, {X: 0.982, Y: 0.28}, {X: 0.982, Y: 0.32}, {X: 0.018, Y: 0.32}}}}
	if groups := MushroomsForGuides([]Guide{guide}, ground, DefaultMushroomDensity); len(groups) != 0 {
		t.Fatal("mushrooms were generated with their air side pointing down")
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
