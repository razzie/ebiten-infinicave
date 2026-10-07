package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestRockCrevicesDistinguishValleysFromFlatAndConvexJoins(t *testing.T) {
	grid := terrain.RockGrid{terrainRect(.4, .4, .04, .04), terrainRect(.44, .4, .04, .02)}
	for i := range grid {
		grid[i].Normal, grid[i].Z = geom.V3{Z: 1}, .04
		grid[i].Color = color.NRGBA{R: 120, G: 110, B: 100, A: 255}
	}
	if seams := rockCrevices(grid); len(seams) != 0 {
		t.Fatal("coplanar facets acquired an outline")
	}
	grid[0].Normal = (geom.V3{X: .8, Z: 1}).Norm()
	grid[1].Normal = (geom.V3{X: -.8, Z: 1}).Norm()
	seams := rockCrevices(grid)
	if len(seams) != 1 || seams[0].alpha == 0 {
		t.Fatal("recessed partial join has no contact crack")
	}
	if math.Abs(seams[0].a.X-.44) > 1e-10 || math.Abs(seams[0].b.X-.44) > 1e-10 ||
		seams[0].a.Y <= .4 || seams[0].b.Y >= .42 {
		t.Fatal("crack extends outside the shared edge or into exposed endpoints")
	}
	grid[0].Normal.X *= -1
	grid[1].Normal.X *= -1
	if seams := rockCrevices(grid); len(seams) != 0 {
		t.Fatal("convex ridge acquired a recessed crack")
	}
	grid[0].Normal, grid[1].Normal = geom.V3{Z: 1}, geom.V3{Z: 1}
	grid[1].Z += .02
	if seams := rockCrevices(grid); len(seams) != 1 {
		t.Fatal("depth step between flat faces lost its contact crack")
	}
}

func TestBackgroundCrevicesAreSelectiveAndRespectBlackPockets(t *testing.T) {
	a, b := terrainRect(.4, .4, .04, .04), terrainRect(.44, .4, .04, .04)
	for _, c := range []*terrain.RockCell{&a, &b} {
		c.Raised, c.Z, c.Normal = false, -.008, geom.V3{Z: 1}
		c.Color = color.NRGBA{R: 25, G: 24, B: 22, A: 255}
	}
	mid := geom.V{X: .44, Y: .42}
	if rockCreviceStrength(a, b, mid) != 0 {
		t.Fatal("flat background facets gained a crevice")
	}
	a.Normal, b.Normal = (geom.V3{X: .15, Z: 1}).Norm(), (geom.V3{X: -.15, Z: 1}).Norm()
	if strength := rockCreviceStrength(a, b, mid); strength <= 0 || strength > .65 {
		t.Fatal("shallow background valley has no subdued crevice")
	}
	b.Color = color.NRGBA{A: 255}
	if rockCreviceStrength(a, b, mid) != 0 {
		t.Fatal("crevice shading enters a black background pocket")
	}
}
