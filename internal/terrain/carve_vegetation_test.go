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
	plants, changed := cut.vegetation(Vegetation{Vines: []Vine{v}}, nil, -1)
	if !changed || len(plants.Cuts) != 1 {
		t.Fatal("ribbon overlap without centerline intersection lost its render mask")
	}
}
