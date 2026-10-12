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
		seen[palettes[i]] = true
	}
	if len(seen) < 4 {
		t.Fatal("vine families need variations of both pastel palettes")
	}
	if child := vinePalette(vines, 12); child != palettes[0] {
		t.Fatal("branches should retain their trunk's palette")
	}
	for i, palette := range palettes {
		c := palette[3]
		switch i % 2 {
		case 0:
			if c.G <= c.R || c.R <= c.B {
				t.Fatalf("missing sage hue: %v", c)
			}
		case 1:
			if c.R <= c.G || c.G <= c.B || int(c.G)-int(c.B) < 7 {
				t.Fatalf("missing straw hue: %v", c)
			}

		}
		vines[i].Foreground = true
		foreground := vinePalette(vines, i)
		for band, muted := range palette {
			bright := foreground[band]
			if muted.R >= bright.R || muted.G >= bright.G || muted.B >= bright.B {
				t.Fatalf("background material lost its depth: %v vs %v", muted, bright)
			}
		}
		if max(c.R, c.G, c.B) > 45 {
			t.Fatalf("background highlight is too bright: %v", c)
		}
		if max(foreground[3].R, foreground[3].G, foreground[3].B)-min(foreground[3].R, foreground[3].G, foreground[3].B) > 85 {
			t.Fatalf("foreground material is too saturated: %v", foreground[3])
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

func TestVineTransitionAndForkUseLocalMaterial(t *testing.T) {
	parent := terrain.Vine{
		Parent: -1, Foreground: true,
		Style:  terrain.VineStyle{From: terrain.VineSage, To: terrain.VineStraw, Start: .1, End: .7},
		Points: []terrain.VinePoint{{P: geom.V{X: .2, Y: .1}, Radius: .004}, {P: geom.V{X: .2, Y: .5}, Radius: .004}, {P: geom.V{X: .2, Y: .9}, Radius: .004}},
		Along:  []float64{0, .4, .8},
	}
	child := terrain.Vine{Parent: 0, Joint: 1, Depth: 1, Foreground: true, Points: []terrain.VinePoint{{P: parent.Points[1].P, Radius: .002}, {P: geom.V{X: .4, Y: .6}, Radius: 0}}, Along: []float64{.4, .6}}
	vines := []terrain.Vine{parent, child}
	start, middle, end := vinePaletteAt(vines, 0, 0), vinePaletteAt(vines, 0, .4), vinePaletteAt(vines, 0, .8)
	if start[3].G <= start[3].R || end[3].R <= end[3].G || middle[3] == start[3] || middle[3] == end[3] {
		t.Fatal("missing sage-to-straw transition")
	}
	if vinePaletteAt(vines, 1, .4) != middle {
		t.Fatal("child does not inherit the local material")
	}
	p := parent.Points[1].P
	if got := vineJoinColor(vines, 1, p, start[0]); got != vineBandColorFrom(middle, 0) {
		t.Fatal("fork samples the parent's starting color rather than its local color")
	}
	meshes := PrepareVines(vines)
	for _, mesh := range meshes {
		checkMesh(t, mesh)
	}
	for i := range parent.Points {
		palette := vinePaletteAt(vines, 0, parent.Along[i])
		p := parent.Points[i].P.Add(geom.V{X: -vineBands[3] * parent.Points[i].Radius})
		want := vineVertex(p, weatherVineColor(palette[3], p, 0))
		if meshes[0].Vertices[i*len(vineBands)+3] != want {
			t.Fatal("stem mesh did not evaluate material along its length")
		}
	}
	cut, _ := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .2, Y: -.5}, Radius: .08})
	fragments, _ := cut.Vines(vines, -1)
	for i, vine := range fragments {
		for j, p := range vine.Points {
			if got, want := vinePaletteAt(fragments, i, terrain.VineDistance(vine, j)), vinePaletteAt(vines, 0, p.P.Y-.1); vine.Parent < 0 && p.P.X == .2 && got != want {
				t.Fatal("carving changed a surviving stem's color")
			}
		}
	}
}

func TestLeavesShareStemMeshAndForegroundShadows(t *testing.T) {
	p := geom.V{X: .3, Y: .4}
	vine := terrain.Vine{Parent: -1, Foreground: true, Points: []terrain.VinePoint{{P: p, Radius: .003}, {P: p.Add(geom.V{Y: .2}), Radius: 0}}, Leaves: []terrain.VineLeaf{{Anchor: p, Polygon: [4]geom.V{p, p.Add(geom.V{X: .02, Y: .01}), p.Add(geom.V{X: .04, Y: .04}), p.Add(geom.V{X: .01, Y: .025})}}}}
	meshes := PrepareForegroundVines([]terrain.Vine{vine})
	if len(meshes) != 2 || len(meshes[1].Vertices) != 2*len(vineBands)+6 {
		t.Fatal("leaves were not included in the stem's cached mesh")
	}
	for _, mesh := range meshes {
		checkMesh(t, mesh)
	}
	leafStart := 2 * len(vineBands)
	if meshes[1].Vertices[leafStart].ColorR == meshes[1].Vertices[leafStart+3].ColorR {
		t.Fatal("leaf halves need distinct flat tones")
	}
	for i := leafStart; i < len(meshes[0].Vertices); i++ {
		shadow, leaf := meshes[0].Vertices[i], meshes[1].Vertices[i]
		if shadow.ColorA != .38 || shadow.DstX-leaf.DstX != 1 || shadow.DstY-leaf.DstY != 1.5 {
			t.Fatal("leaf lost its contact shadow")
		}
	}
	batch := SectionMesh{ForegroundVines: meshes}
	FinishSectionMesh(&batch)
	for _, point := range vine.Leaves[0].Polygon {
		if int(point.X*RasterPixelsPerUnit) < batch.ForegroundVinesBounds.Min.X || int(point.X*RasterPixelsPerUnit) >= batch.ForegroundVinesBounds.Max.X {
			t.Fatal("cached layer bounds omit a leaf")
		}
	}
}

func TestRepeatedVineColorsStayDarkInBackground(t *testing.T) {
	style := terrain.VineStyle{From: terrain.VineSage, To: terrain.VineStraw, Start: .03, End: .10, Period: .18}
	vines := []terrain.Vine{{Parent: -1, Style: style}, {Parent: -1, Style: style, Foreground: true}}
	seen := map[[len(vineBands)]color.NRGBA]bool{}
	for cycle := range 6 {
		distance := style.End + float64(cycle)*style.Period + .001
		background, foreground := vinePaletteAt(vines, 0, distance), vinePaletteAt(vines, 1, distance)
		seen[foreground] = true
		for i, c := range background {
			bright := foreground[i]
			for _, pair := range [][2]uint8{{c.R, bright.R}, {c.G, bright.G}, {c.B, bright.B}} {
				if float64(pair[0]) > float64(pair[1])*.21+1 {
					t.Fatal("background material is too bright")
				}
			}
		}
	}
	if len(seen) != 2 {
		t.Fatal("renderer does not alternate green and yellow along a vine")
	}
}
