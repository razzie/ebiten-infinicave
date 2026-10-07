package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestRidgedGuideHasBroadDeterministicFacets(t *testing.T) {
	original := SplineGuide([]geom.V{{X: 0.1, Y: 0.2}, {X: 0.5, Y: 0.2}}, 1)
	g := RidgedGuide(original, 42)
	if !reflect.DeepEqual(g, RidgedGuide(original, 42)) {
		t.Fatal("ridge changes when regenerated")
	}
	if len(g.Pts) >= len(original.Pts)/2 || len(g.Pts) < 15 {
		t.Fatalf("expected broad facets instead of dense curve samples, got %d", len(g.Pts))
	}
	if g.Pts[0] != original.Pts[0] || g.Pts[len(g.Pts)-1] != original.Pts[len(original.Pts)-1] {
		t.Fatal("roughness moved a guide endpoint")
	}
	minY, maxY := .200, .200
	for _, p := range g.Pts {
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		if math.Abs(p.Y-.200) > .0025 {
			t.Fatal("ridge no longer follows the original guide")
		}
	}
	if maxY-minY < .003 {
		t.Fatal("guide border is still smooth")
	}
	// Translating a regeneration window cannot change its geometry.
	original.translateY(-1)
	shifted := RidgedGuide(original, 42)
	for i, p := range g.Pts {
		if p.Sub(shifted.Pts[i].Add(geom.V{X: 0, Y: 1})).Len() > 1e-9 {
			t.Fatal("roughness depends on section position")
		}
	}
}
