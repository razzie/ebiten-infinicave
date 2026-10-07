package terrain

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestHorizontalMushroomsAndLighting(t *testing.T) {
	// In this section frame, a vertical guide maps to a horizontal ledge.
	guide := SplineGuide([]geom.V{{X: .5, Y: .1}, {X: .5, Y: .9}}, -1)
	guide.Seed = 42
	ground := RockGrid{terrainRect(.5, .05, .4, .9)}
	groups := MushroomsForGuides([]Guide{guide}, ground, Horizontal)
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
	groups := MushroomsForGuides(guides, ground)
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
	if again := MushroomsForGuides(guides, ground); !reflect.DeepEqual(groups, again) {
		t.Fatal("mushroom groups are not deterministic")
	}
}

func TestMushroomsSkipUnsupportedAndScreenEdgeGuides(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: 0.004, Y: 0.3}, {X: 0.4, Y: 0.3}}, 1)
	guide.Seed = 42
	if groups := MushroomsForGuides([]Guide{guide}, nil); len(groups) != 0 {
		t.Fatal("mushrooms were generated without foreground support")
	}
	ground := RockGrid{{Center: geom.V{X: 0.2, Y: 0.3}, Polygon: []geom.V{{X: 0.018, Y: 0.28}, {X: 0.3, Y: 0.28}, {X: 0.3, Y: 0.32}, {X: 0.018, Y: 0.32}}}}
	if groups := MushroomsForGuides([]Guide{guide}, ground); len(groups) != 0 {
		t.Fatal("mushrooms were generated along an unsupported screen-edge section")
	}
	guide = SplineGuide([]geom.V{{X: 0.1, Y: 0.3}, {X: 0.5, Y: 0.3}, {X: 0.9, Y: 0.3}}, -1)
	guide.Seed = 43
	ground = RockGrid{{Center: geom.V{X: 0.5, Y: 0.3}, Polygon: []geom.V{{X: 0.018, Y: 0.28}, {X: 0.982, Y: 0.28}, {X: 0.982, Y: 0.32}, {X: 0.018, Y: 0.32}}}}
	if groups := MushroomsForGuides([]Guide{guide}, ground); len(groups) != 0 {
		t.Fatal("mushrooms were generated with their air side pointing down")
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
