package terrain

import (
	"context"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestShapeDifferenceHolesContactsAndSplits(t *testing.T) {
	outer := terrainRect(.1, .1, .8, .8).Polygon
	hole := terrainRect(.3, .3, .4, .4).Polygon
	for i, j := 0, len(hole)-1; i < j; i, j = i+1, j-1 {
		hole[i], hole[j] = hole[j], hole[i]
	}
	island := terrainRect(.4, .4, .2, .2).Polygon
	for _, loops := range [][][]geom.V{{outer}, {outer, hole, island}, {
		{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .9, Y: .9}, {X: .1, Y: .9}, {X: .1, Y: .7}, {X: .7, Y: .7}, {X: .7, Y: .3}, {X: .1, Y: .3}},
	}} {
		shapes := SplitLoops(loops)
		original := CollisionGeometry{Polygons: loops}
		commands := []Hole{
			{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: .08},
			{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: .08},
			{Shape: HoleSegment, Start: geom.V{X: .5, Y: 0}, End: geom.V{X: .5, Y: 1}, Width: .03},
			{Shape: HoleSegment, Start: geom.V{X: .95, Y: .1}, End: geom.V{X: .95, Y: .9}, Width: .10},
			{Shape: HoleCircle, Center: geom.V{X: .3, Y: .3}, Radius: .1},
			{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: 2},
		}
		var cuts []RockCut
		for _, h := range commands {
			cut, _ := CutFromHole(h)
			cuts = append(cuts, cut)
			var next []RockShape
			for _, s := range shapes {
				parts, _ := DifferenceShape(s, cut.Poly)
				next = append(next, parts...)
			}
			shapes = next
			for y := .017; y < 1; y += .019 {
				for x := .013; x < 1; x += .023 {
					p := geom.V{X: x, Y: y}
					want := original.Contains(p)
					for _, c := range cuts {
						if (CollisionGeometry{Polygons: [][]geom.V{c.Poly}}).Contains(p) {
							want = false
						}
					}
					got := false
					for _, s := range shapes {
						got = got || s.Contains(p)
					}
					if got != want {
						t.Fatalf("shape occupancy at %v after %+v: %v/%v", p, h, got, want)
					}
				}
			}
		}
	}
}

func TestShapeDifferenceRandomizedDamage(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	data := SectionData{Foreground: RockGrid{terrainRect(.08, .1, .3, .75), terrainRect(.38, .1, .5, .22), terrainRect(.38, .65, .5, .2)}}
	g := PrepareTerrainGeometry(data, 0)
	original := g.Collision
	var cuts []RockCut
	for i := 0; i < 40; i++ {
		h := Hole{Shape: HoleCircle, Center: geom.V{X: rng.Float64(), Y: rng.Float64() - 1}, Radius: .018 + rng.Float64()*.12}
		if i%3 == 0 {
			h = Hole{Shape: HoleSegment, Start: geom.V{X: rng.Float64(), Y: rng.Float64() - 1}, End: geom.V{X: rng.Float64(), Y: rng.Float64() - 1}, Width: .012 + rng.Float64()*.04}
		}
		cut, err := CutFromHole(h)
		if err != nil {
			t.Fatal(err)
		}
		cuts = append(cuts, cut)
		g, _, _ = cut.Geometry(0, g, 0)
		for j := 0; j < 300; j++ {
			p := geom.V{X: rng.Float64(), Y: rng.Float64() - 1}
			want := original.Contains(p)
			for _, c := range cuts {
				if geom.InsidePolygon(p, c.Poly) {
					want = false
					break
				}
			}
			if g.Collision.Contains(p) != want {
				t.Fatalf("edit %d at %v changed the polygon difference", i, p)
			}
		}
	}
}

func TestFineEdgeSettingsDoNotChangeStructuralPolygons(t *testing.T) {
	a, b := NewSectionBuilder(42, nil), NewSectionBuilder(42, nil)
	a.Shape, b.Shape = DefaultShapeOptions(), DefaultShapeOptions()
	a.Shape.SpikeDensity, a.Shape.EdgeRoughness = 0, 0
	aData, bData := a.Begin(context.Background(), 0).Data(), b.Begin(context.Background(), 0).Data()
	if !reflect.DeepEqual(aData.ForegroundTopology.Grid, bData.ForegroundTopology.Grid) {
		t.Fatal("decorative edge settings changed structural polygons")
	}
}

func BenchmarkBoundaryCarve(b *testing.B) {
	s := newRockShape([][]geom.V{terrainRect(.1, .1, .8, .8).Polygon})
	cut, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: .08})
	b.ReportAllocs()
	for b.Loop() {
		parts, changed := DifferenceShape(s, cut.Poly)
		if !changed || len(parts) != 1 || len(parts[0].Loops) != 2 {
			b.Fatal("enclosed cut lost its hole")
		}
	}
}

func TestStructuralAreaUsesSignedHoles(t *testing.T) {
	s := newRockShape([][]geom.V{terrainRect(0, 0, 1, 1).Polygon})
	cut, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: .1})
	parts, changed := DifferenceShape(s, cut.Poly)
	if !changed || len(parts) != 1 {
		t.Fatal("circle failed")
	}
	area := 0.0
	for _, p := range parts[0].Loops {
		area += geom.PolygonArea(p)
	}
	if math.Abs(area-(1-geom.PolygonArea(cut.Poly))) > 1e-12 {
		t.Fatal("cut filled or inverted a hole")
	}
}
