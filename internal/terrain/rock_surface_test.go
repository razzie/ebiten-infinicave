package terrain

import (
	"image/color"
	"math"
	"math/rand"
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

func TestRaisedRockCastsShadowAndOccludesAmbientLight(t *testing.T) {
	background := RockGrid{{Center: geom.V{X: 0.5, Y: 1.5}, Polygon: []geom.V{{X: 0, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0, Y: generationMaxY}}, Normal: geom.V3{Z: 1}}}
	block := RockGrid{{Center: geom.V{X: 0.5, Y: 0.5}, Polygon: []geom.V{{X: 0.47, Y: 0.47}, {X: 0.53, Y: 0.47}, {X: 0.53, Y: 0.53}, {X: 0.47, Y: 0.53}}, Z: .070, Normal: geom.V3{Z: 1}, Raised: true}}
	d := newRockDepth(background, block)
	away := (geom.V{X: -rockLight.X, Y: -rockLight.Y}).Norm()
	shadowed := geom.V{X: 0.5, Y: 0.5}.Add(away.Mul(.055))
	lit := geom.V{X: 0.5, Y: 0.5}.Sub(away.Mul(.055))
	if d.visibility(shadowed, 0) > .1 || d.visibility(lit, 0) < .99 {
		t.Fatal("raised rock does not cast a directional shadow on lower terrain")
	}
	if d.ambient(geom.V{X: 0.535, Y: 0.5}, 0) >= d.ambient(geom.V{X: 0.75, Y: 0.5}, 0) {
		t.Fatal("contact with raised rock does not darken ambient light")
	}
	if d.visibility(geom.V{X: 0.5, Y: 0.5}, .070) < .99 {
		t.Fatal("flat exposed top shadows itself")
	}
	block[0].Z = 0
	flat := newRockDepth(background, block)
	if flat.visibility(shadowed, 0) < .99 {
		t.Fatal("zero-height rock still casts the raised rock's shadow")
	}
}

func TestSpatialRockNeighborsMatchAllPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var sites []geom.V
	for i := 0; i < 160; i++ {
		sites = append(sites, geom.V{X: rng.Float64(), Y: GenerationMinY + rng.Float64()*GenerationHeight})
	}
	grid := newRockGrid(sites, func(geom.V) color.NRGBA { return color.NRGBA{A: 255} })
	want := make([][]int, len(grid))
	for i := range grid {
		for j := i + 1; j < len(grid); j++ {
			if mergeableBorder(grid[i].Polygon, grid[j].Polygon, nil) > mergeTolerance {
				want[i] = append(want[i], j)
				want[j] = append(want[j], i)
			}
		}
	}
	if got := RockNeighbors(grid); !reflect.DeepEqual(got, want) {
		t.Fatal("spatial search changed rock adjacency or neighbor order")
	}
}

func TestRockNormalsFollowNeighborHeights(t *testing.T) {
	// Unequal, non-axis-aligned spacing must still recover a planar slope.
	grid := RockGrid{{Center: geom.V{X: 0.05, Y: 0.05}}, {Center: geom.V{X: 0.023, Y: 0.042}}, {Center: geom.V{X: 0.061, Y: 0.017}}, {Center: geom.V{X: 0.085, Y: 0.061}}, {Center: geom.V{X: 0.039, Y: 0.079}}}
	for _, slope := range []geom.V{{}, {X: 0, Y: 1}, {X: 0, Y: -1}, {X: .7, Y: -.3}} {
		for i := range grid {
			grid[i].Z = .015 + grid[i].Center.Dot(slope)
		}
		got := rockNormal(grid, 0, []int{1, 2, 3, 4})
		want := (geom.V3{X: -slope.X, Y: -slope.Y, Z: 1}).Norm()
		if math.Abs(got.X-want.X)+math.Abs(got.Y-want.Y)+math.Abs(got.Z-want.Z) > 1e-12 {
			t.Fatalf("slope %v: got %v, want %v", slope, got, want)
		}
	}
	flat := SurfaceLight(geom.V3{Z: 1})
	up, down := SurfaceLight((geom.V3{X: 0, Y: -1, Z: 1}).Norm()), SurfaceLight((geom.V3{X: 0, Y: 1, Z: 1}).Norm())
	if up <= flat || flat <= down {
		t.Fatalf("lighting does not favor upward faces: up=%v flat=%v down=%v", up, flat, down)
	}
	if got := rockNormal(grid, 0, nil); got != (geom.V3{Z: 1}) {
		t.Fatalf("isolated cell has invalid normal: %v", got)
	}
	grid[1].Center, grid[1].Z = geom.V{X: 0.05, Y: 0.04}, grid[0].Z-.010
	if got := rockNormal(grid, 0, []int{1}); math.Abs(got.Y+math.Sqrt(.5)) > 1e-12 {
		t.Fatalf("single neighbor lost its slope: %v", got)
	}
}

func TestRockNeighborsIncludePartialEdgesButNotCorners(t *testing.T) {
	grid := RockGrid{
		{Polygon: []geom.V{{X: 0, Y: 0}, {X: 0.02, Y: 0}, {X: 0.02, Y: 0.02}, {X: 0, Y: 0.02}}},
		{Polygon: []geom.V{{X: 0.02, Y: 0}, {X: 0.03, Y: 0}, {X: 0.03, Y: 0.01}, {X: 0.02, Y: 0.01}}},
		{Polygon: []geom.V{{X: 0.02, Y: 0.02}, {X: 0.03, Y: 0.02}, {X: 0.03, Y: 0.03}, {X: 0.02, Y: 0.03}}},
	}
	neighbors := RockNeighbors(grid)
	if len(neighbors[0]) != 1 || neighbors[0][0] != 1 || len(neighbors[1]) != 1 || len(neighbors[2]) != 0 {
		t.Fatalf("incorrect boundary adjacency: %v", neighbors)
	}
}

func TestGuideFacingOverridesHeightsOnBothSides(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: -0.01, Y: 0.5}, {X: generationWidth + 0.01, Y: 0.5}}, 1)}
	grid := guideRockFaces([]geom.V{{X: 0.25, Y: 0.48}, {X: 0.75, Y: 0.48}, {X: 0.25, Y: 0.8}, {X: 0.75, Y: 0.8}}, guides)
	shapeRockGrid(grid, guides, NewPerlin(rand.New(rand.NewSource(42))))
	touching, distant := 0, 0
	for i, cell := range grid {
		contact := false
		for _, p := range cell.Polygon {
			contact = contact || math.Abs(p.Y-.500) < 1e-10
		}
		if contact {
			touching++
			if math.Abs(cell.Normal.X) > 1e-9 || cell.Normal.Y*(.500-cell.Center.Y) <= 0 {
				t.Fatalf("guide neighbor faces away: %v normal=%v", cell.Center, cell.Normal)
			}
		} else {
			distant++
			want := rockNormal(grid, i, RockNeighbors(grid)[i])
			if cell.Normal != want {
				t.Fatalf("guide overrode a non-neighbor: %v", cell.Center)
			}
		}
	}
	if touching != 4 || distant != 2 {
		t.Fatalf("unexpected guide contact counts: %d/%d", touching, distant)
	}
}

func TestRockLightingPreservesOccupancyAndDarkFlanks(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	guides := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	for y := float64(GenerationMinY); y < generationMaxY; y += 0.02 {
		for x := 0.0; x < generationWidth; x += 0.02 {
			p := geom.V{X: x, Y: y}
			darkForeground := guideSurfaceColor(p, guides, noise, nil, geom.V3{Y: 1})
			if darkForeground.A != 0 && (darkForeground.R <= 8 || darkForeground.R > 40) {
				t.Fatalf("foreground flank brightness is outside the raised palette: %v", darkForeground)
			}
			lit := guideSurfaceColor(p, guides, noise, nil, geom.V3{X: 0, Y: -.8, Z: .6})
			if lit.A != darkForeground.A {
				t.Fatal("surface lighting changed guide transparency")
			}
			if lit.A != 0 && lit.R <= darkForeground.R {
				t.Fatalf("guide-facing foreground is not brighter: lit=%v flank=%v", lit, darkForeground)
			}
		}
	}
}

func TestBackgroundContainsBlackAndDarkCells(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	tones := make(map[color.NRGBA]bool)
	for y := float64(GenerationMinY); y < generationMaxY; y += 0.02 {
		for x := 0.0; x < generationWidth; x += 0.02 {
			clr := backgroundCellColor(geom.V{X: x, Y: y}, noise)
			if clr.A != 255 || clr.R > 50 || clr.G > 51 || clr.B > 49 {
				t.Fatalf("background must stay opaque and dark: %v", clr)
			}
			tones[clr] = true
		}
	}
	if !tones[color.NRGBA{A: 255}] || len(tones) < 10 {
		t.Fatalf("expected exact black and varied charcoal cells, got %d tones", len(tones))
	}
	for y := float64(GenerationMinY); y < generationMaxY; y += 0.02 {
		for x := 0.0; x < generationWidth; x += 0.02 {
			p := geom.V{X: x, Y: y}
			lit := backgroundSurfaceColor(p, noise, geom.V3{X: 0, Y: -1, Z: 0})
			shadowed := backgroundSurfaceColor(p, noise, geom.V3{X: 0, Y: 1, Z: 0})
			if lit.R > shadowed.R {
				return
			}
		}
	}
	t.Fatal("background directional lighting has no visible contrast")
}

func TestGuideLayerOccupancyIsOpaqueAndIndependentOfLight(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	guides := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	for _, p := range []geom.V{{X: 0.5, Y: 0.1}, {X: 0.5, Y: 0.39}, {X: 0.5, Y: 0.9}} {
		if clr := guideCellColor(p, guides, noise, nil); clr != (color.NRGBA{}) {
			t.Errorf("unlit guide cell at %v must be transparent, got %v", p, clr)
		}
	}
	if clr := guideCellColor(geom.V{X: 0.5, Y: 0.408}, nil, noise, nil); clr.A != 0 {
		t.Errorf("empty guide grid must be transparent, got %v", clr)
	}
	crest := guideCellColor(geom.V{X: 0.5, Y: 0.408}, guides, noise, nil)
	if crest.A != 255 || crest.R < 75 || crest.R > 175 {
		t.Errorf("crest must remain bright and opaque, got %v", crest)
	}
	solid, empty := 0, 0
	for y := 0.42; y < 0.68; y += 0.005 {
		clr := guideCellColor(geom.V{X: 0.5, Y: y}, guides, noise, nil)
		switch clr.A {
		case 0:
			empty++
		case 255:
			solid++
		default:
			t.Fatal("solid rock must not fade through the background")
		}
	}
	if solid == 0 || empty == 0 {
		t.Fatal("relief must have an opaque shoulder and a finite footprint")
	}

	// An isolated offshoot remains visible in the subdued flank palette.
	p := geom.V{X: 0.5, Y: 0.65}
	branches := newBranchField([]BranchSegment{{
		A: geom.V{X: 0.5, Y: 0.62}, B: geom.V{X: 0.5, Y: 0.68},
		WidthA: .030, WidthB: .020, LightA: .4, LightB: .3,
	}})
	if clr := guideCellColor(p, guides, noise, branches); clr.A != 255 || clr.R < 25 {
		t.Errorf("branch must reveal foreground faces beyond the guide band, got %v", clr)
	}
}

func TestBackgroundMaterialDoesNotBakeForegroundShadows(t *testing.T) {
	background := RockGrid{{Center: geom.V{X: .5, Y: .55}, Polygon: []geom.V{{X: 0, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 2}, {X: 0, Y: 2}}, Normal: geom.V3{Z: 1}}}
	foreground := RockGrid{{Center: geom.V{X: .5, Y: .5}, Polygon: []geom.V{{X: .45, Y: .45}, {X: .55, Y: .45}, {X: .55, Y: .53}, {X: .45, Y: .53}}, Z: .07, Normal: geom.V3{Z: 1}, Raised: true}}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	shadeRockGrids(background, foreground, noise)
	withRock := background[0]
	shadeRockGrids(background, nil, noise)
	if !reflect.DeepEqual(withRock, background[0]) {
		t.Fatal("foreground silhouette changed the cached background material")
	}
	if background[0].Color != backgroundSurfaceColor(background[0].Center, noise, background[0].Normal) {
		t.Fatal("background still contains baked cast-shadow darkening")
	}
}

func TestArtisticFacetSelectionMatchesOverlappingWindows(t *testing.T) {
	a := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	b := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 1.4}, {X: 0.9, Y: 1.4}}, 1)}
	var sitesA, sitesB []geom.V
	for y := 0.41; y < 0.65; y += 0.025 {
		for x := 0.125; x < 0.875; x += 0.025 {
			sitesA = append(sitesA, geom.V{X: x, Y: y})
			sitesB = append(sitesB, geom.V{X: x, Y: y + 1})
		}
	}
	left, right := artisticRockSeeds(sitesA, a, 42, 0), artisticRockSeeds(sitesB, b, 42, -1)
	if len(left) >= len(sitesA) || len(left) != len(right) {
		t.Fatal("flank facet selection is not stable or has no effect")
	}
	for i, p := range left {
		q := right[i]
		q.Y -= 1
		if p.Sub(q).Len() > 1e-9 {
			t.Fatal("facet placement changes across windows")
		}
	}
}

func TestReliefProfileHasBevelCrestAndBroadFlank(t *testing.T) {
	const width, height, bevel = .180, .072, .016
	if ridgeProfile(-0.001, width, height, bevel) != 0 || ridgeProfile(width, width, height, bevel) != 0 {
		t.Fatal("relief extends outside its footprint")
	}
	if ridgeProfile(0, width, height, bevel) <= 0 || ridgeProfile(bevel, width, height, bevel) != height {
		t.Fatal("exposed lip has no thickness or crest")
	}
	for sample := 1; sample < 180; sample++ {
		d := float64(sample) * .001
		change := ridgeProfile(d, width, height, bevel) - ridgeProfile(d-0.001, width, height, bevel)
		if (d <= bevel && change <= 0) || (d > bevel && change >= 0) {
			t.Fatalf("relief changes slope at the wrong place: d=%v", d)
		}
	}
}

func TestForcedGuideReliefContinuousAcrossVerticalBend(t *testing.T) {
	// At the middle knot the curve turns through a vertical tangent. Flipping
	// its rock side there introduces a height step away from the guide itself,
	// where no face was split to accommodate an exposed edge.
	for _, mode := range []Orientation{Vertical, Horizontal} {
		knots := []geom.V{{X: .25, Y: .25}, {X: .55, Y: .3}, {X: .6, Y: .5}, {X: .55, Y: .7}}
		MapPoints(knots, mode.internalLocal)
		g := SplineGuide(knots, 1)
		g.faceUp(mode)
		g.prepareProjection()
		noise := NewPerlin(rand.New(rand.NewSource(42)))
		before := reliefHeight(mode.internalLocal(geom.V{X: .57, Y: .49}), []Guide{g}, noise, nil)
		after := reliefHeight(mode.internalLocal(geom.V{X: .57, Y: .51}), []Guide{g}, noise, nil)
		if before <= rockContourHeight || after <= rockContourHeight || math.Abs(before-after) > .005 {
			t.Fatalf("%v: vertical bend tears the rock surface: height %g / %g", mode, before, after)
		}
	}
}

func TestReliefHeightAndSiteDeformationUseWorldCoordinates(t *testing.T) {
	a := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.5, Y: 0.35}, {X: 0.9, Y: 0.4}}, 1)}
	b := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.5, Y: 0.35}, {X: 0.9, Y: 0.4}}, 1)}
	b[0].translateY(1)
	n1, n2 := NewPerlin(rand.New(rand.NewSource(42))), NewPerlin(rand.New(rand.NewSource(42)))
	n2.OffsetY = -1
	for y := 0.35; y < 0.7; y += 0.013 {
		p, q := geom.V{X: 0.45, Y: y}, geom.V{X: 0.45, Y: y + 1}
		if math.Abs(reliefHeight(p, a, n1, nil)-reliefHeight(q, b, n2, nil)) > 1e-9 {
			t.Fatal("relief changes between generation windows")
		}
		x, z := reliefSeeds([]geom.V{p}, a)[0], reliefSeeds([]geom.V{q}, b)[0]
		z.Y -= 1
		if x.Sub(z).Len() > 1e-9 {
			t.Fatal("facet deformation changes between generation windows")
		}
	}
}

func TestReliefFadesBeforeHorizontalScreenInset(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: -0.1, Y: 0.5}, {X: 0.9, Y: 0.5}}, 1)}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	atEdge := reliefHeight(geom.V{X: 0, Y: 0.5}, guides, noise, nil)
	atInset := reliefHeight(geom.V{X: foregroundScreenInset, Y: 0.5}, guides, noise, nil)
	inset := reliefHeight(geom.V{X: 0.09, Y: 0.5}, guides, noise, nil)
	if atEdge != 0 || atInset >= rockContourHeight {
		t.Fatalf("foreground relief survives at the screen edge: edge=%v inset=%v", atEdge, atInset)
	}
	if inset <= rockContourHeight {
		t.Fatalf("edge fade erased interior foreground relief: %v", inset)
	}
}

func TestReliefTapersBeforeOpenGuideTips(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .2, Y: .5}, {X: .8, Y: .5}}, 1)
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, x := range []float64{.19, .2, .8, .81} {
		if height := reliefHeight(geom.V{X: x, Y: .51}, []Guide{guide}, noise, nil); height != 0 {
			t.Fatalf("open tip extends a raised cap beyond the guide at x=%v: height %v", x, height)
		}
	}
	for _, x := range []float64{.25, .5, .75} {
		if height := reliefHeight(geom.V{X: x, Y: .51}, []Guide{guide}, noise, nil); height <= rockContourHeight {
			t.Fatalf("tip taper erased the full-height ridge at x=%v: height %v", x, height)
		}
	}
}

func TestLightingSeparatesShapeFromMaterialAndVisibility(t *testing.T) {
	up := RockSurfaceColor(rockLight, 1, 1)
	front := RockSurfaceColor(geom.V3{Z: 1}, 1, 1)
	down := RockSurfaceColor((geom.V3{X: 0, Y: 1, Z: .3}).Norm(), 1, 1)
	shadow := RockSurfaceColor(rockLight, 0, .5)
	if up.R <= front.R || front.R <= down.R || shadow.R >= front.R || down.R <= 8 || down.R > 40 || up.R-shadow.R > 160 {
		t.Fatalf("lighting does not describe the relief: up=%v front=%v down=%v shadow=%v", up, front, down, shadow)
	}
	for _, c := range []color.NRGBA{up, front, down, shadow} {
		if c.A != 255 {
			t.Fatal("lighting makes solid rock transparent")
		}
	}
}
