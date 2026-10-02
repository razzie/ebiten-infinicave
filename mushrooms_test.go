package main

import (
	"reflect"
	"testing"
)

func TestMushroomsFollowGuidesAndKeepCapsUpright(t *testing.T) {
	guides := []Guide{
		splineGuide([]V{{100, 300}, {450, 300}, {850, 300}}, 1),
		splineGuide([]V{{150, 750}, {150, 450}, {150, 180}}, 1),
	}
	for i := range guides {
		guides[i].Seed = int64(i + 1)
	}
	ground := RockGrid{
		{Center: V{500, 450}, Polygon: []V{{18, 300}, {982, 300}, {982, 600}, {18, 600}}},
		{Center: V{325, 450}, Polygon: []V{{150, 18}, {500, 18}, {500, 982}, {150, 982}}},
	}
	groups := mushroomsForGuides(guides, ground)
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
		if guideIndex < 0 || projection.Dist > 1e-6 || mathAbs(mushroom.RootDirection.Dot(projection.T)) > 1e-6 || mathAbs(rootSegment.Dot(projection.T)) > .2 {
			t.Fatalf("stem root is not perpendicular to its guide: root=%+v dist=%.6g direction=%+v tangent=%+v segmentDot=%.6g", mushroom.Anchor, projection.Dist, mushroom.RootDirection, projection.T, rootSegment.Dot(projection.T))
		}
		if !mushroomOnGround(mushroom.Anchor, groundBounds) {
			t.Fatal("mushroom root is not supported by a foreground contour")
		}
		if !mushroomWithinForegroundInset(mushroom) {
			t.Fatal("mushroom extends beyond the visible foreground inset")
		}
		for _, p := range mushroom.Stem {
			if p.Sub(mushroom.Anchor).Dot(mushroom.RootDirection) < -mushroomSink-1e-6 {
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
	if again := mushroomsForGuides(guides, ground); !reflect.DeepEqual(groups, again) {
		t.Fatal("mushroom groups are not deterministic")
	}
}

func TestMushroomsSkipUnsupportedAndScreenEdgeGuides(t *testing.T) {
	guide := splineGuide([]V{{4, 300}, {400, 300}}, 1)
	guide.Seed = 42
	if groups := mushroomsForGuides([]Guide{guide}, nil); len(groups) != 0 {
		t.Fatal("mushrooms were generated without foreground support")
	}
	ground := RockGrid{{Center: V{200, 300}, Polygon: []V{{18, 280}, {300, 280}, {300, 320}, {18, 320}}}}
	if groups := mushroomsForGuides([]Guide{guide}, ground); len(groups) != 0 {
		t.Fatal("mushrooms were generated along an unsupported screen-edge section")
	}
	guide = splineGuide([]V{{100, 300}, {500, 300}, {900, 300}}, -1)
	guide.Seed = 43
	ground = RockGrid{{Center: V{500, 300}, Polygon: []V{{18, 280}, {982, 280}, {982, 320}, {18, 320}}}}
	if groups := mushroomsForGuides([]Guide{guide}, ground); len(groups) != 0 {
		t.Fatal("mushrooms were generated with their air side pointing down")
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
