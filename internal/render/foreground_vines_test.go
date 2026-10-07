package render

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestForegroundVinesUseCopperInheritedColor(t *testing.T) {
	vines := []terrain.Vine{
		{Parent: -1, Foreground: true, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.08}, Radius: 0.003}, {P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.003}, {P: geom.V{X: 0.1, Y: 0.12}, Radius: 0}}},
		{Parent: 0, Joint: 1, Depth: 1, Foreground: true, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.002}, {P: geom.V{X: 0.12, Y: 0.12}, Radius: 0}}},
	}
	palette := vinePalette(vines, 0)
	if palette[3].R <= palette[3].G || palette[3].G <= palette[3].B || palette[3].R > 200 || vinePalette(vines, 1) != palette {
		t.Fatal("foreground stems need copper highlights inherited by their branches")
	}
	if got := vineJoinColor(vines, 1, geom.V{X: 0.1, Y: 0.1}, palette[0]); got != vineBandColorFrom(palette, 0) {
		t.Fatal("foreground fork has a material discontinuity")
	}
	meshes := PrepareForegroundVines(vines)
	if len(meshes) != 2*len(vines) {
		t.Fatal("foreground vines lost their contact shadows")
	}
	for _, mesh := range meshes {
		checkMesh(t, mesh)
	}
	if meshes[0].Vertices[0].ColorA >= 1 || meshes[len(vines)].Vertices[0].ColorA != 1 {
		t.Fatal("contact shadows must precede the opaque stems")
	}
}
