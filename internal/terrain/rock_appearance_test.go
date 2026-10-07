package terrain

import (
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func appearanceTestGrid() RockGrid {
	var grid RockGrid
	for row := range 4 {
		y := .4 + float64(row)*.025
		grid = append(grid, RockCell{
			Center:  geom.V{X: .5, Y: y + .0125},
			Polygon: []geom.V{{X: .475, Y: y}, {X: .525, Y: y}, {X: .525, Y: y + .025}, {X: .475, Y: y + .025}},
			Z:       .055 - float64(row)*.012, Normal: geom.V3{Z: 1}, Raised: true,
			Color: color.NRGBA{R: 100, G: 90, B: 80, A: 255}, Shadow: 1, Ambient: 1,
		})
	}
	return grid
}

func TestRockAppearancePreservesGenerationAndSolidFaces(t *testing.T) {
	grid := appearanceTestGrid()
	original := append(RockGrid(nil), grid...)
	boundary := ExposedRockEdges(grid)
	shaded := ShadeRockFaces(grid, boundary)
	if !reflect.DeepEqual(grid, original) {
		t.Fatal("render shading changed terrain used for vegetation or collision")
	}
	if shaded[0].Color.R <= shaded[len(shaded)-1].Color.R {
		t.Fatal("exposed upper crest is not brighter than the recessed flank")
	}
	for i, c := range shaded {
		if !reflect.DeepEqual(c.Polygon, original[i].Polygon) || c.Center != original[i].Center || c.Z != original[i].Z || c.Color.A != 255 {
			t.Fatal("appearance altered the formation footprint, height, or opacity")
		}
		length := math.Sqrt(c.Normal.X*c.Normal.X + c.Normal.Y*c.Normal.Y + c.Normal.Z*c.Normal.Z)
		if math.Abs(length-1) > 1e-12 || c.Normal.Z <= 0 || c.Shadow < 0 || c.Shadow > 1 || c.Ambient < 0 || c.Ambient > 1 {
			t.Fatal("invalid visual normal or illumination")
		}
	}
	if !reflect.DeepEqual(shaded, ShadeRockFaces(grid, boundary)) {
		t.Fatal("rebuilding the material changed its detail")
	}
}

func TestRockAppearanceMatchesOverlappingWindows(t *testing.T) {
	a, b := appearanceTestGrid(), appearanceTestGrid()
	for i := range b {
		b[i].worldTop = -1
		b[i].Center.Y += 1
		b[i].Polygon = append([]geom.V(nil), b[i].Polygon...)
		for j := range b[i].Polygon {
			b[i].Polygon[j].Y += 1
		}
	}
	left, right := ShadeRockFaces(a, ExposedRockEdges(a)), ShadeRockFaces(b, ExposedRockEdges(b))
	for i, c := range left {
		d := right[i]
		if c.Color != d.Color || math.Abs(c.Normal.X-d.Normal.X)+math.Abs(c.Normal.Y-d.Normal.Y)+math.Abs(c.Normal.Z-d.Normal.Z) > 1e-10 ||
			math.Abs(c.Shadow-d.Shadow)+math.Abs(c.Ambient-d.Ambient) > 1e-9 {
			t.Fatalf("face %d differs across windows: color %v/%v normal %v/%v shadow %v/%v ambient %v/%v", i, c.Color, d.Color, c.Normal, d.Normal, c.Shadow, d.Shadow, c.Ambient, d.Ambient)
		}
	}
}

func TestRockCrestBiasFollowsWorldUp(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		grid := appearanceTestGrid()
		for i := range grid {
			grid[i].orientation = orientation
		}
		// A single short boundary isolates top bias from the noise perturbation.
		center := grid[0].Center
		up := InternalPoint(orientation, geom.V{Y: -1})
		along := up.Perp().Mul(.02)
		mid := center.Add(up.Mul(.01))
		top := []RockEdge{{A: mid.Sub(along), B: mid.Add(along), Cell: 0}}
		mid = center.Sub(up.Mul(.01))
		bottom := []RockEdge{{A: mid.Add(along), B: mid.Sub(along), Cell: 0}}
		lit, underside := ShadeRockFaces(grid, top), ShadeRockFaces(grid, bottom)
		if SurfaceLight(lit[0].Normal, orientation) <= SurfaceLight(underside[0].Normal, orientation) {
			t.Fatalf("%s: underside acquired the upper crest's lighting bias", orientation)
		}
	}
}

func TestBackgroundRockAppearancePreservesDarkFieldAndMargins(t *testing.T) {
	grid := appearanceTestGrid()
	for i := range grid {
		grid[i].Raised = false
		grid[i].Z = -.01 + float64(i)*.001
		grid[i].Color = color.NRGBA{R: 22, G: 22, B: 20, A: 255}
		// Include a face outside the central cave. Its relief still participates
		// in neighbor occlusion; it must not disappear at a depth-buffer cutoff.
		grid[i].Center.X -= .7
		for j := range grid[i].Polygon {
			grid[i].Polygon[j].X -= .7
		}
	}
	grid[0].Color = color.NRGBA{A: 255}
	original := append(RockGrid(nil), grid...)
	shaded := ShadeRockFaces(grid, nil)
	if !reflect.DeepEqual(grid, original) {
		t.Fatal("background appearance changed the material used for vine growth")
	}
	if shaded[0].Color != original[0].Color {
		t.Fatal("background lighting filled an exact black pocket")
	}
	changed := false
	for i, c := range shaded {
		if !reflect.DeepEqual(c.Polygon, original[i].Polygon) || c.Z != original[i].Z || c.Center != original[i].Center || c.Color.A != 255 {
			t.Fatal("background appearance changed its coverage or relief")
		}
		if c.Color.R > 35 || c.Color.G > 35 || c.Color.B > 35 || c.Shadow != 1 || c.Ambient < .82 || c.Ambient > 1 {
			t.Fatalf("background gained foreground contrast or baked cast shadows: %+v", c)
		}
		changed = changed || c.Normal != original[i].Normal && c.Color != original[i].Color
	}
	if !changed || !reflect.DeepEqual(shaded, ShadeRockFaces(grid, nil)) {
		t.Fatal("background refinement is ineffective or changes on rebuilding")
	}
}

func TestBackgroundRockAppearanceMatchesOverlappingWindows(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		a, b := appearanceTestGrid(), appearanceTestGrid()
		for i := range a {
			a[i].Raised, b[i].Raised = false, false
			a[i].orientation, b[i].orientation = orientation, orientation
			a[i].Z, b[i].Z = -.008, -.008
			a[i].Color, b[i].Color = color.NRGBA{R: 25, G: 24, B: 22, A: 255}, color.NRGBA{R: 25, G: 24, B: 22, A: 255}
			b[i].worldTop = -1
			b[i].Center.Y += 1
			for j := range b[i].Polygon {
				b[i].Polygon[j].Y += 1
			}
		}
		left, right := ShadeRockFaces(a, nil), ShadeRockFaces(b, nil)
		for i, c := range left {
			d := right[i]
			if c.Color != d.Color || math.Abs(c.Normal.X-d.Normal.X)+math.Abs(c.Normal.Y-d.Normal.Y)+math.Abs(c.Normal.Z-d.Normal.Z) > 1e-10 || math.Abs(c.Ambient-d.Ambient) > 1e-10 {
				t.Fatalf("%s: background face %d differs across windows", orientation, i)
			}
		}
	}
}
