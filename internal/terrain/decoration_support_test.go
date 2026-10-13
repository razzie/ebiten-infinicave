package terrain

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestInsetRockAnchorHandlesThinRockAndConcaveCorners(t *testing.T) {
	for _, tc := range []struct {
		name      string
		poly      []geom.V
		p, inward geom.V
	}{
		{"thin wall", []geom.V{{}, {X: .01}, {X: .01, Y: .00001}, {Y: .00001}}, geom.V{X: .005}, geom.V{Y: 1}},
		{"convex corner", []geom.V{{}, {X: .01}, {X: .01, Y: .01}, {Y: .01}}, geom.V{}, geom.V{}},
		{"concave wall", []geom.V{{}, {X: .01}, {X: .01, Y: .01}, {X: .005, Y: .005}, {Y: .01}}, geom.V{X: .005, Y: .005}, geom.V{Y: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := InsetRockAnchor(tc.poly, tc.p, tc.inward)
			if !ok || !geom.InsidePolygon(p, tc.poly) || p.Sub(tc.p).Len() > .001 {
				t.Fatalf("anchor is not just inside its support: %+v, ok=%v", p, ok)
			}
		})
	}
	if _, ok := InsetRockAnchor([]geom.V{{}, {X: .01}, {X: .01, Y: .01}, {Y: .01}}, geom.V{X: .1}, geom.V{}); ok {
		t.Fatal("distant rock acquired an anchor")
	}
}

func TestGeneratedDecorationSupportsStayInsideRock(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		data := NewSectionBuilder(42, nil, mode).Build(0)
		inside := func(p geom.V, grid RockGrid) bool {
			for _, c := range grid {
				if geom.InsidePolygon(p, c.Polygon) {
					return true
				}
			}
			return false
		}
		for _, group := range data.Mushrooms {
			for _, m := range group.Mushrooms {
				if !m.supportSet || !inside(MushroomSupport(m), data.ForegroundBake.Grid) {
					t.Fatalf("%s mushroom has no rock-interior support", mode)
				}
			}
		}
		for _, web := range data.ForegroundTopology.SpiderWebs {
			if len(web.supports) != web.Network.Anchors {
				t.Fatal("web lost a rock attachment")
			}
			for _, p := range SpiderWebSupports(web) {
				if !inside(p, data.ForegroundTopology.Appearance) {
					t.Fatal("web support is outside its supporting rock")
				}
			}
		}
	}
}
