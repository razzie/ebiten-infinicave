package render

import (
	"image/color"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCarveRemapsBranchesAndPreservesFamilyMaterials(t *testing.T) {
	trunk := terrain.Vine{Parent: -1, Foreground: true}
	for _, x := range []float64{.1, .3, .5, .7, .9} {
		trunk.Points = append(trunk.Points, terrain.VinePoint{P: geom.V{X: x, Y: .5}, Radius: .004})
	}
	source := []terrain.Vine{trunk}
	for joint := 1; joint <= 3; joint++ {
		source = append(source, terrain.Vine{
			Parent: 0, Joint: joint, Depth: 1, Foreground: true,
			Points: []terrain.VinePoint{{P: trunk.Points[joint].P, Radius: .002}, {P: trunk.Points[joint].P.Add(geom.V{Y: .3}), Radius: .001}},
		})
	}
	cut, _ := terrain.CutFromHole((terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .5, Y: -.5}, Radius: .1}))
	vines, changed := cut.Vines(source, -1)
	if !changed || len(vines) != 5 {
		t.Fatalf("branching cut: %d survivors, changed=%v", len(vines), changed)
	}
	for i, vine := range vines {
		if terrain.VineStyleFamily(vines, i) != 0 || vinePalette(vines, i) != vinePalette(source, 0) || !vine.Foreground {
			t.Fatal("split changed a family material")
		}
		if vine.Parent >= 0 {
			if vine.Parent >= i || vine.Joint >= len(vines[vine.Parent].Points) || vine.Points[0].P != vines[vine.Parent].Points[vine.Joint].P {
				t.Fatal("surviving branch has an invalid attachment")
			}
		}
	}
	if vines[2].Parent != 0 || vines[3].Parent != -1 || vines[4].Parent != 1 {
		t.Fatalf("branches did not attach to the correct surviving trunk: %+v", vines)
	}
	for _, mesh := range PrepareForegroundVines(vines) {
		checkMesh(t, mesh)
	}
	// Removing the first family must not recolor the next family by index.
	source = []terrain.Vine{
		{Parent: -1, Points: []terrain.VinePoint{{P: geom.V{X: .48, Y: .5}, Radius: .002}, {P: geom.V{X: .52, Y: .5}, Radius: .002}}},
		{Parent: -1, Points: []terrain.VinePoint{{P: geom.V{X: .1, Y: .7}, Radius: .002}, {P: geom.V{X: .9, Y: .7}, Radius: .002}}},
	}
	palette := vinePalette(source, 1)
	vines, _ = cut.Vines(source, -1)
	if len(vines) != 1 || vinePalette(vines, 0) != palette || terrain.VineStyleFamily(vines, 0) != 1 {
		t.Fatal("removing an earlier family recolored the next family")
	}
}

func TestForkShadingMeetsParent(t *testing.T) {
	parent := terrain.Vine{Parent: -1, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.08}, Radius: 0.008}, {P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.008}, {P: geom.V{X: 0.1, Y: 0.12}, Radius: 0.008}}}
	branch := terrain.Vine{Depth: 1, Parent: 0, Joint: 1, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.005}, {P: geom.V{X: 0.13, Y: 0.16}, Radius: 0}}}
	vines := []terrain.Vine{parent, branch}
	parentPalette := vinePalette(vines, 0)
	// Even the dark outer bands of the child inherit the parent material at
	// the join instead of drawing a black cut through its highlight.
	for _, x := range []float64{.096, .100, .104} {
		p := geom.V{X: x, Y: 0.1}
		want := vineBandColorFrom(parentPalette, (.100-x)/.008)
		got := vineJoinColor(vines, 1, p, vineColors[0])
		if got != want {
			t.Fatalf("fork seam at %v: got %v, want parent material %v", p, got, want)
		}
	}
	if got := vineJoinColor(vines, 1, geom.V{X: 0.13, Y: 0.16}, vineColors[3]); got != vineColors[3] {
		t.Fatal("parent shading extends beyond the fork")
	}
}

func TestVinePalettesVaryAndStayMuted(t *testing.T) {
	vines := make([]terrain.Vine, 13)
	for i := range 12 {
		vines[i].Parent = -1
	}
	vines[12] = terrain.Vine{Depth: 1, Parent: 0}
	var palettes [12][len(vineColors)]color.NRGBA
	seen := make(map[[len(vineColors)]color.NRGBA]bool, len(palettes))
	for i := range palettes {
		palettes[i] = vinePalette(vines, i)
		if seen[palettes[i]] {
			t.Fatalf("trunks 0 through 11 repeat a palette at trunk %d", i)
		}
		seen[palettes[i]] = true
	}
	if child := vinePalette(vines, 12); child != palettes[0] {
		t.Fatal("branches should retain their trunk's palette")
	}
	for _, palette := range palettes[8:10] {
		if int(palette[3].G)*100 < int(palette[3].R)*40 || int(palette[3].B)*100 < int(palette[3].R)*30 {
			t.Fatalf("palette should have a brown cast: %v", palette[3])
		}
	}
	for _, palette := range palettes[10:12] {
		if palette[3].R != palette[3].G || palette[3].G != palette[3].B {
			t.Fatalf("palette should be neutral gray: %v", palette[3])
		}
	}
	for _, palette := range palettes {
		for i, muted := range palette {
			original := vineColors[i]
			mutedTone := .30*float64(muted.R) + .59*float64(muted.G) + .11*float64(muted.B)
			originalTone := .30*float64(original.R) + .59*float64(original.G) + .11*float64(original.B)
			mutedRange := max(int(muted.R), int(muted.G), int(muted.B)) - min(int(muted.R), int(muted.G), int(muted.B))
			originalRange := max(int(original.R), int(original.G), int(original.B)) - min(int(original.R), int(original.G), int(original.B))
			if mutedTone >= originalTone || mutedRange >= originalRange {
				t.Fatalf("palette band %d is not dimmer and less saturated: got %v, original %v", i, muted, original)
			}
		}
		if max(int(palette[3].R), int(palette[3].G), int(palette[3].B)) > 55 {
			t.Fatalf("vine highlight is too bright for background growth: %v", palette[3])
		}
	}
	if meshes := PrepareVines(vines); len(meshes) != len(vines) {
		t.Fatalf("expected one matte mesh per vine, got %d for %d stems", len(meshes), len(vines))
	}
}

func TestVineWeatheringIsSubtleAndStable(t *testing.T) {
	base := color.NRGBA{R: 72, G: 18, B: 16, A: 255}
	first := weatherVineColor(base, geom.V{X: 0.12, Y: 0.34}, 2)
	if first != weatherVineColor(base, geom.V{X: 0.12, Y: 0.34}, 2) {
		t.Fatal("vine weathering is not deterministic")
	}
	second := weatherVineColor(base, geom.V{X: 0.64, Y: 0.81}, 2)
	if first == second {
		t.Fatal("vine surface has no spatial mottling")
	}
	for _, weathered := range []color.NRGBA{first, second} {
		if weathered.A != base.A || weathered.R > base.R || weathered.G > base.G || weathered.B > base.B ||
			float64(weathered.R) < float64(base.R)*.89 || float64(weathered.G) < float64(base.G)*.89 || float64(weathered.B) < float64(base.B)*.89 {
			t.Fatalf("vine weathering exceeded its subtle darkening range: %v", weathered)
		}
	}
}
