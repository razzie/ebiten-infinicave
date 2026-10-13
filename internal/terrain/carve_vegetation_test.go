package terrain

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestCarveVineTangencyAndRibbonOnlyOverlap(t *testing.T) {
	cut, _ := CutFromHole((Hole{Shape: HoleSegment, Start: geom.V{X: .3, Y: -.4}, End: geom.V{X: .7, Y: -.4}, Width: .2}))
	v := Vine{Parent: -1, Points: []VinePoint{{geom.V{X: .1, Y: .5}, .01}, {geom.V{X: .9, Y: .5}, .01}}}
	fragments := splitVine(v, cut.localPolygon(-1))
	if len(fragments) != 1 || !reflect.DeepEqual(fragments[0].points, v.Points) {
		t.Fatal("tangent centerline was removed")
	}
	plants, changed := cut.vegetation(Vegetation{Vines: []Vine{v}}, -1)
	if !changed || len(plants.Cuts) != 1 {
		t.Fatal("ribbon overlap without centerline intersection lost its render mask")
	}
}

func TestCarveMushroomsUsesGuideRootAndExactCut(t *testing.T) {
	const top = -3.0
	m := Mushroom{Anchor: geom.V{X: .5, Y: .4}, Stem: []geom.V{{X: .5, Y: .404}, {X: .5, Y: .38}}, CapCenter: geom.V{X: .5, Y: .38}}
	source := Vegetation{Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{m}}}}
	for _, tc := range []struct {
		name   string
		center geom.V
		radius float64
		hit    bool
	}{
		{"guide root", m.Anchor, .001, true},
		{"buried stalk only", m.Stem[0], .001, true},
		{"stalk end", m.CapCenter, .003, true},
		{"bounds overlap outside circle", m.Anchor.Add(geom.V{X: .009, Y: .009}), .01, false},
		{"root on cut boundary", m.Anchor.Add(geom.V{X: .01}), .01, false},
		{"distant cut", geom.V{X: .8, Y: .8}, .01, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cut, err := CutFromHole(Hole{Shape: HoleCircle, Center: tc.center.Add(geom.V{Y: top}), Radius: tc.radius})
			if err != nil {
				t.Fatal(err)
			}
			plants, changed := cut.vegetation(source, top)
			if changed != tc.hit || (len(plants.Mushrooms) == 0) != tc.hit {
				t.Fatalf("root damage: changed=%v mushrooms=%d, want hit=%v", changed, len(plants.Mushrooms), tc.hit)
			}
			if !tc.hit && !reflect.DeepEqual(plants, source) {
				t.Fatal("missed root changed vegetation")
			}
			if len(source.Mushrooms[0].Mushrooms) != 1 || !reflect.DeepEqual(source.Mushrooms[0].Mushrooms[0], m) {
				t.Fatal("carving mutated source mushroom")
			}
			if !tc.hit {
				if allocs := testing.AllocsPerRun(10, func() { cut.vegetation(source, top) }); allocs != 0 {
					t.Fatalf("missed root allocated %v times", allocs)
				}
			}
		})
	}
}
