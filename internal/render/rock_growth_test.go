package render

import (
	"context"
	"image/color"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func growthTestCell(mode terrain.Orientation, x float64) terrain.RockCell {
	// The notch exercises clipping to a concave face rather than its bounds.
	poly := []geom.V{{X: x, Y: .4}, {X: x + .04, Y: .4}, {X: x + .04, Y: .44},
		{X: x + .025, Y: .44}, {X: x + .025, Y: .415}, {X: x + .015, Y: .415},
		{X: x + .015, Y: .44}, {X: x, Y: .44}}
	terrain.MapPoints(poly, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p) })
	up := terrain.InternalPoint(mode, geom.V{Y: -.6})
	return terrain.WithRockOrientation(terrain.RockCell{
		Center: geom.PolygonCenter(poly), Polygon: poly, Normal: geom.V3{X: up.X, Y: up.Y, Z: .8},
		Raised: true, Color: color.NRGBA{R: 145, G: 138, B: 112, A: 255}, Shadow: 1, Ambient: 1,
	}, mode)
}

func TestMossColoniesClipToSolidRockAndRebuildDeterministically(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		green, yellow, retained := false, false, 0
		for i := 0; i < 40; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.02)
			grid := terrain.RockGrid{cell}
			options := RockGrowthOptions{Moss: DefaultMossDensity}
			patches := prepareGrowthAttachments(grid, nil, options, false)
			if !reflect.DeepEqual(patches, prepareGrowthAttachments(grid, nil, options, false)) {
				t.Fatal("moss changed on rebuild")
			}
			for _, a := range patches {
				retained++
				wantYellow := terrain.VegetationGreen(terrain.RockMaterialPoint(cell, a.Anchor)) < .5
				if (a.Cell >= 32) != wantYellow {
					t.Fatal("moss atlas hue does not follow its world-space anchor")
				}
				green, yellow = green || a.Cell < 32, yellow || a.Cell >= 32
				if a.Kind != SurfaceMoss || len(a.Anchors) != 0 || a.Dead {
					t.Fatal("moss lost texture-only carving behavior")
				}
				checkMesh(t, a.Mesh)
				for j := 0; j < len(a.Mesh.Indices); j += 3 {
					p := geom.V{}
					for _, index := range a.Mesh.Indices[j : j+3] {
						p = p.Add(mossTestPoint(a.Mesh.Vertices[index]))
					}
					if !geom.InsidePolygon(p.Mul(1.0/3), cell.Polygon) {
						t.Fatal("moss bridges the concave notch")
					}
				}
			}
			for _, kind := range []string{"background", "transparent", "downward"} {
				ineligible := cell
				switch kind {
				case "background":
					ineligible.Raised = false
				case "transparent":
					ineligible.Color.A = 0
				case "downward":
					ineligible.Normal.X, ineligible.Normal.Y = -cell.Normal.X, -cell.Normal.Y
				}
				if patches := prepareGrowthAttachments(terrain.RockGrid{ineligible}, nil, options, false); len(patches) != 0 {
					t.Fatalf("unsupported colony on %s face", kind)
				}
			}
		}
		if retained == 0 || !green || !yellow {
			t.Fatalf("missing moss colonies or palettes in %s mode", mode)
		}
	}
}

func mossTestPoint(v ebiten.Vertex) geom.V {
	return geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
}

// Check actual colored atlas geometry as well as its clipped sprite, so empty
// padding cannot stand in for rim contact, a colony gap, or a seam crossing.
func mossTestPainted(a Attachment, p geom.V) bool {
	contains := func(mesh TriangleMesh, p geom.V) bool {
		for i := 0; i < len(mesh.Indices); i += 3 {
			tri := []geom.V{mossTestPoint(mesh.Vertices[mesh.Indices[i]]), mossTestPoint(mesh.Vertices[mesh.Indices[i+1]]), mossTestPoint(mesh.Vertices[mesh.Indices[i+2]])}
			if geom.InsidePolygon(p, tri) {
				return true
			}
		}
		return false
	}
	if !contains(a.Mesh, p) {
		return false
	}
	q := geom.Rotate(terrain.WorldPoint(a.Orientation, p.Sub(a.Anchor)), -a.Rotation).Mul(1 / a.Scale)
	return contains(AtlasRecipes(MossAtlas)[a.Cell], q)
}

func mossTestLedge(mode terrain.Orientation, columns int) (terrain.RockGrid, []terrain.RockEdge) {
	var grid terrain.RockGrid
	for row := 0; row < 2; row++ {
		for i := 0; i < columns; i++ {
			c := growthTestCell(mode, .1)
			x, y, width := .1+float64(i)*.8/float64(columns), .4+float64(row)*.02, .8/float64(columns)
			c.Polygon = []geom.V{{X: x, Y: y}, {X: x + width, Y: y}, {X: x + width, Y: y + .02}, {X: x, Y: y + .02}}
			terrain.MapPoints(c.Polygon, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p) })
			c.Center = geom.PolygonCenter(c.Polygon)
			if row == 1 {
				c.Normal.X, c.Normal.Y = -c.Normal.X, -c.Normal.Y
				c.Color.R = 65
			}
			grid = append(grid, c)
		}
	}
	outline := []geom.V{{X: .1, Y: .4}, {X: .9, Y: .4}, {X: .9, Y: .44}, {X: .1, Y: .44}}
	terrain.MapPoints(outline, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p) })
	var boundary []terrain.RockEdge
	for i, a := range outline {
		boundary = append(boundary, terrain.RockEdge{A: a, B: outline[(i+1)%len(outline)]})
	}
	return grid, boundary
}

func TestMossColoniesWrapFacetsWithSharedPaletteAndLocalLighting(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		grid, boundary := mossTestLedge(mode, 20)
		patches := prepareGrowthAttachments(grid, boundary, RockGrowthOptions{Moss: 1}, false)
		wrapping, shaded, contact, bare := false, false, false, false
		for _, a := range patches {
			columns, tints := make(map[int]bool), make(map[float32]bool)
			for _, v := range a.Mesh.Vertices {
				p := terrain.WorldPoint(mode, mossTestPoint(v))
				columns[int((p.X-.1)/.04)] = true
				tints[v.ColorR] = true
			}
			wrapping, shaded = wrapping || len(columns) > 1, shaded || len(tints) > 1
		}
		for x := .102; x < .9; x += .004 {
			p := terrain.InternalPoint(mode, geom.V{X: x, Y: .4005})
			painted := false
			for _, a := range patches {
				painted = painted || mossTestPainted(a, p)
			}
			contact, bare = contact || painted, bare || !painted
		}
		if !wrapping || !shaded || !contact || !bare {
			t.Fatalf("missing colony behavior in %s mode: wrapping=%v shading=%v rim=%v bare=%v", mode, wrapping, shaded, contact, bare)
		}
		// Collinear facet splits must not reset the field, palette, or shape.
		coarse, coarseBoundary := mossTestLedge(mode, 1)
		want, got := rockMossColonies(coarse, coarseBoundary, 1), rockMossColonies(grid, boundary, 1)
		if len(want) != len(got) {
			t.Fatal("facet subdivision changed colony count")
		}
		for i, c := range want {
			if c.anchor.Sub(got[i].anchor).Len() > 1e-12 || math.Abs(c.rotation-got[i].rotation) > 1e-12 || c.scale != got[i].scale || c.sample != got[i].sample {
				t.Fatal("facet subdivision restarted a colony")
			}
		}
	}
}

func TestMossColoniesFavorLowerPartsOfUpwardFacets(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		lower, rim, total := 0, 0, 0
		// Move the ledge through a full lattice spacing so the preference
		// must hold across different placements relative to the world field.
		for fixture := 0; fixture < 32; fixture++ {
			grid, boundary := mossTestLedge(mode, 20)
			shift := float64(fixture) * .0025
			move := terrain.InternalPoint(mode, geom.V{Y: shift})
			for i := range grid {
				for j := range grid[i].Polygon {
					grid[i].Polygon[j] = grid[i].Polygon[j].Add(move)
				}
				grid[i].Center = grid[i].Center.Add(move)
			}
			for i := range boundary {
				boundary[i].A, boundary[i].B = boundary[i].A.Add(move), boundary[i].B.Add(move)
			}
			for _, colony := range rockMossColonies(grid, boundary, 1) {
				y := terrain.WorldPoint(mode, colony.anchor).Y - shift
				total++
				if y > .41 && y < .42 {
					lower++
				} else if math.Abs(y-.4) < 1e-12 {
					rim++
				} else {
					t.Fatalf("colony center is outside its supporting ledge: y=%g", y)
				}
			}
		}
		if lower*100 < total*65 || rim == 0 {
			t.Fatalf("expected mostly lower-face colonies with occasional rim growth in %s mode: lower=%d rim=%d", mode, lower, rim)
		}
	}
}

func TestMossColoniesCoverMoreOfLitSupportingFaces(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		grid, boundary := mossTestLedge(mode, 20)
		for i := 0; i < 20; i++ {
			grid[i].Color.R = 180
		}
		patches := prepareGrowthAttachments(grid, boundary, RockGrowthOptions{Moss: 1}, false)
		lit, shaded := 0, 0
		for row := 0; row < 10; row++ {
			for column := 0; column < 200; column++ {
				p := terrain.InternalPoint(mode, geom.V{X: .102 + float64(column)*.004, Y: .402 + float64(row)*.004})
				painted := false
				for _, a := range patches {
					if p.X >= a.Min.X && p.X <= a.Max.X && p.Y >= a.Min.Y && p.Y <= a.Max.Y {
						painted = painted || mossTestPainted(a, p)
					}
				}
				if painted {
					if row < 5 {
						lit++
					} else {
						shaded++
					}
				}
			}
		}
		if lit*4 <= shaded*5 || shaded == 0 {
			t.Fatalf("expected more painted area on lit faces with some seam overlap in %s mode: lit=%d shaded=%d", mode, lit, shaded)
		}
	}
}

func TestMossGrowthFollowsNearbyHorizontalCellEdges(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for _, lean := range []float64{0, -.01, .01} {
			cell := growthTestCell(mode, .3)
			cell.Polygon = []geom.V{{X: .3, Y: .4}, {X: .36, Y: .4 + lean}, {X: .36, Y: .44 + lean}, {X: .3, Y: .44}}
			terrain.MapPoints(cell.Polygon, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p) })
			cell.Center = geom.PolygonCenter(cell.Polygon)
			colony := mossColony{anchor: terrain.InternalPoint(mode, geom.V{X: .33, Y: .438 + lean*.5}), scale: .65, sample: 0, rotation: .1}
			rock := newGrowthClearance(terrain.RockGrid{cell})
			angle := mossSeamRotation(colony, rock)
			direction := geom.V{X: math.Cos(angle), Y: math.Sin(angle)}
			edge := terrain.WorldPoint(mode, cell.Polygon[2].Sub(cell.Polygon[3])).Norm()
			if math.Abs(direction.Dot(edge)) < 1-1e-12 {
				t.Fatalf("moss fails to follow the horizontal edge in %s mode, lean=%g", mode, lean)
			}
			lo, hi := geom.V{X: math.Inf(1), Y: math.Inf(1)}, geom.V{X: math.Inf(-1), Y: math.Inf(-1)}
			for _, v := range AtlasRecipes(MossAtlas)[colony.sample].Vertices {
				p := geom.Rotate(mossTestPoint(v), angle).Mul(colony.scale)
				lo.X, lo.Y, hi.X, hi.Y = min(lo.X, p.X), min(lo.Y, p.Y), max(hi.X, p.X), max(hi.Y, p.Y)
			}
			if hi.X-lo.X <= hi.Y-lo.Y {
				t.Fatal("painted moss does not extend farther horizontally along the seam")
			}
			remote := colony
			remote.anchor = remote.anchor.Add(terrain.InternalPoint(mode, geom.V{Y: .1}))
			if mossSeamRotation(remote, rock) != remote.rotation {
				t.Fatal("a distant cell edge redirected the colony")
			}
			cell.Normal.X, cell.Normal.Y = -cell.Normal.X, -cell.Normal.Y
			if mossSeamRotation(colony, newGrowthClearance(terrain.RockGrid{cell})) != colony.rotation {
				t.Fatal("a downward-facing cell redirected the colony")
			}
		}
	}
}

func TestMossColoniesShareWorldCoordinatesAcrossSectionWindows(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		builder := terrain.NewSectionBuilder(42, nil, mode)
		var previous []mossColony
		var previousOrigin geom.V
		for id := int64(0); id < 2; id++ {
			build := builder.Begin(context.Background(), id)
			if build == nil || len(build.Data().Foreground) == 0 {
				t.Fatal("missing section fixture")
			}
			// Retain the real private material origin, and put the same ledge
			// in both padded windows using each window's own local frame.
			c := build.Data().Foreground[0]
			origin := terrain.RockMaterialPoint(c, geom.V{})
			c.Polygon = []geom.V{{X: .2, Y: -.99}, {X: .8, Y: -.99}, {X: .8, Y: -.95}, {X: .2, Y: -.95}}
			terrain.MapPoints(c.Polygon, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p.Sub(origin)) })
			c.Center = geom.PolygonCenter(c.Polygon)
			c.Color, c.Raised = color.NRGBA{R: 145, A: 255}, true
			up := terrain.InternalPoint(mode, geom.V{Y: -.6})
			c.Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
			var boundary []terrain.RockEdge
			for i, p := range c.Polygon {
				boundary = append(boundary, terrain.RockEdge{A: p, B: c.Polygon[(i+1)%len(c.Polygon)]})
			}
			colonies := rockMossColonies(terrain.RockGrid{c}, boundary, .72)
			if len(colonies) == 0 {
				t.Fatal("fixture has no colonies")
			}
			if id > 0 {
				if len(previous) != len(colonies) {
					t.Fatal("section window changed colony count")
				}
				for i, a := range previous {
					b := colonies[i]
					worldA := terrain.WorldPoint(mode, a.anchor).Add(previousOrigin)
					worldB := terrain.WorldPoint(mode, b.anchor).Add(origin)
					if worldA.Sub(worldB).Len() > 1e-12 || a.sample != b.sample || a.scale != b.scale || math.Abs(a.rotation-b.rotation) > 1e-12 {
						t.Fatal("neighboring section windows shifted or recolored moss")
					}
				}
			}
			previous, previousOrigin = colonies, origin
		}
	}
}

func TestMossDensityPreservesWholeColonies(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		grid, boundary := mossTestLedge(mode, 20)
		full := prepareGrowthAttachments(grid, boundary, RockGrowthOptions{Moss: 1}, false)
		thin := prepareGrowthAttachments(grid, boundary, RockGrowthOptions{Moss: .25}, false)
		if len(thin) == 0 || len(thin) >= len(full) {
			t.Fatal("density did not thin colonies")
		}
		for _, a := range thin {
			found := false
			for _, b := range full {
				found = found || reflect.DeepEqual(a, b)
			}
			if !found {
				t.Fatal("density moved, reshaped, or recolored a surviving colony")
			}
		}
		if len(prepareGrowthAttachments(grid, boundary, RockGrowthOptions{}, false)) != 0 {
			t.Fatal("zero density retained moss")
		}
	}
}

func TestHangingGrowthFollowsGravityAndNeedsAir(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		count := 0
		for i := 0; i < 60; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.013)
			// Sloping ceilings must not tilt the stalk away from gravity.
			terrain.MapPoints(cell.Polygon, func(p geom.V) geom.V {
				world := terrain.WorldPoint(mode, p)
				world.Y += float64(i%3-1) * .3 * world.X
				return terrain.InternalPoint(mode, world)
			})
			// The bottom edge on the left arm faces downward in world axes.
			edge := terrain.RockEdge{A: cell.Polygon[6], B: cell.Polygon[7], Cell: 0}
			growth, ok := hangingGrowthForEdge(cell, edge, DefaultHangingGrowthDensity)
			if !ok {
				continue
			}
			count++
			if len(growth.lobes) == 0 {
				t.Fatal("plain bulb retained instead of leafy lichen")
			}
			for j := 1; j < len(growth.stem); j++ {
				delta := terrain.WorldPoint(mode, growth.stem[j].Sub(growth.stem[j-1]))
				if math.Abs(delta.X) > 1e-12 || delta.Y <= 0 {
					t.Fatalf("stalk is not straight down in %s mode: %+v", mode, delta)
				}
			}
			delta := terrain.WorldPoint(mode, growth.center.Sub(growth.stem[0]))
			if math.Abs(delta.X) > 1e-12 || delta.Y <= .01 {
				t.Fatalf("bulb is not directly below its root in %s mode: %+v", mode, delta)
			}
			rock := mushroomRockSilhouette(&terrain.RockTopology{Grid: terrain.RockGrid{cell}, Boundary: []terrain.RockEdge{edge}})
			if !hangingGrowthClear(growth, rock) {
				t.Fatal("growth rejected its own supporting rock")
			}
			p := growth.stem[3]
			blocker := []geom.V{{X: p.X - .001, Y: p.Y - .001}, {X: p.X + .001, Y: p.Y - .001}, {X: p.X + .001, Y: p.Y + .001}, {X: p.X - .001, Y: p.Y + .001}}
			lo, hi := geom.PolygonBounds(blocker)
			if hangingGrowthClear(growth, append(rock, mushroomRockFace{blocker, lo, hi})) {
				t.Fatal("growth crosses a narrow obstruction between stalk samples")
			}
			// Check the very bottom of the bulb too: its droplet extends
			// farther than a circular radius around the stalk endpoint.
			tip := growth.body[0]
			for _, point := range growth.body[1:] {
				if terrain.WorldPoint(mode, point).Y > terrain.WorldPoint(mode, tip).Y {
					tip = point
				}
			}
			blocker = []geom.V{{X: tip.X - .0001, Y: tip.Y - .0001}, {X: tip.X + .0001, Y: tip.Y - .0001}, {X: tip.X + .0001, Y: tip.Y + .0001}, {X: tip.X - .0001, Y: tip.Y + .0001}}
			lo, hi = geom.PolygonBounds(blocker)
			if hangingGrowthClear(growth, append(rock, mushroomRockFace{blocker, lo, hi})) {
				t.Fatal("the bottom of the bulb intersects rock")
			}
			// Reversing the exposed side must prohibit hanging growth.
			edge.A, edge.B = edge.B, edge.A
			if _, ok := hangingGrowthForEdge(cell, edge, DefaultHangingGrowthDensity); ok {
				t.Fatal("hanging growth selected an upward-facing edge")
			}
		}
		if count == 0 {
			t.Fatalf("missing hanging growth in %s mode", mode)
		}
	}
}

func TestHangingGrowthDensityPreservesSurvivors(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		hangingCount := 0
		for i := 0; i < 80; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.01)
			edge := terrain.RockEdge{A: cell.Polygon[6], B: cell.Polygon[7]}
			growth, ok := hangingGrowthForEdge(cell, edge, .25)
			fullGrowth, fullOK := hangingGrowthForEdge(cell, edge, 1)
			if _, ok := hangingGrowthForEdge(cell, edge, 0); ok {
				t.Fatal("zero density retained hanging growth")
			}
			if !fullOK {
				t.Fatal("maximum density skipped an eligible underside")
			}
			if ok {
				hangingCount++
				if !reflect.DeepEqual(growth, fullGrowth) {
					t.Fatal("changing density moved or reshaped surviving hanging growth")
				}
			}
		}
		if hangingCount == 0 || hangingCount == 80 {
			t.Fatalf("density failed to thin hanging growth: %d", hangingCount)
		}
	}
}

func TestRockGrowthDensityReachesAllMeshPreparationPaths(t *testing.T) {
	cell := growthTestCell(terrain.Vertical, .3)
	topology := terrain.NewRockTopology(terrain.RockGrid{cell})
	topology.Appearance = topology.Grid
	guides := grassTestGuides(cell)
	data := terrain.SectionData{Foreground: topology.Grid, ForegroundTopology: topology, Guides: guides}
	for _, density := range []RockGrowthOptions{{Moss: 1}, {Hanging: 1}, {Grass: 1}, {Moss: 1, Hanging: 1, Grass: 1}} {
		density.Guides = guides
		full := PrepareGridWithTopology(nil, ViewShaded, topology, density)
		if len(full.Growth) == 0 {
			t.Fatalf("configured growth missing: %+v", density)
		}
		if !reflect.DeepEqual(PrepareSection(data, ViewShaded, density).Foreground, full) {
			t.Fatal("complete section ignored density")
		}
		owned := PrepareOwnedGridWithTopology(nil, ViewShaded, topology, density)
		if !reflect.DeepEqual(PrepareForeground(data, ViewShaded, density).Foreground, owned) ||
			!reflect.DeepEqual(PrepareTerrain(data, ViewShaded, density).Foreground, owned) {
			t.Fatal("owned terrain ignored density")
		}
		if !reflect.DeepEqual(PrepareGridWithTopology(nil, ViewClay, topology, density), PrepareGridWithTopology(nil, ViewClay, topology, RockGrowthOptions{})) {
			t.Fatal("growth density affected diagnostic terrain")
		}
	}
	defaults := RockGrowthOptions{Moss: DefaultMossDensity, Hanging: DefaultHangingGrowthDensity, Grass: DefaultGrassDensity}
	if !reflect.DeepEqual(PrepareGridWithTopology(nil, ViewShaded, topology), PrepareGridWithTopology(nil, ViewShaded, topology, defaults)) {
		t.Fatal("default density changed the original appearance")
	}
}

func TestGuideBulbsHangOverFlanksAndPreserveSupportedPlacement(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		grid, boundary := mossTestLedge(mode, 20)
		coarse, _ := mossTestLedge(mode, 1)
		options := RockGrowthOptions{Hanging: 1, Guides: grassTestGuides(coarse[0])}
		full := prepareGuideBulbs(grid, boundary, options, false)
		if len(full) == 0 {
			t.Fatalf("no bulbs on guide-cell bottoms in %s mode", mode)
		}
		if !reflect.DeepEqual(full, prepareGuideBulbs(grid, boundary, options, false)) {
			t.Fatal("rebuilding moved or reshaped bulbs")
		}
		var surfaceBulbs []Attachment
		for _, a := range prepareGrowthAttachments(grid, boundary, options, false) {
			if a.Kind == HangingAttachment && !a.Underlay {
				surfaceBulbs = append(surfaceBulbs, a)
			}
		}
		if !reflect.DeepEqual(full, surfaceBulbs) {
			t.Fatal("growth preparation omitted guide bulbs")
		}
		for _, a := range full {
			if a.Underlay || a.Kind != HangingAttachment || a.Mesh.Texture != HangingAtlas {
				t.Fatal("bulb must draw over the darker flank")
			}
			root := terrain.WorldPoint(mode, a.Anchor)
			if math.Abs(root.Y-.42) > 1e-12 {
				t.Fatal("bulb is not rooted at the bottom of its guide cell")
			}
			if len(a.Anchors) != 1 || !slices.ContainsFunc(grid[:20], func(c terrain.RockCell) bool { return geom.InsidePolygon(a.Anchors[0], c.Polygon) }) {
				t.Fatal("bulb has no inset top-cell support")
			}
			rock, edge := hangingAtlasSource(a.Cell)
			if g, ok := hangingGrowthForEdge(rock, edge, 1); !ok || len(g.lobes) == 0 {
				t.Fatal("interior guide edge received a plain bulb instead of leafy lichen")
			}
			cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: a.Anchors[0], Radius: .001})
			if err != nil {
				t.Fatal(err)
			}
			if !a.Hit(cut, 0) {
				t.Fatal("cutting the support retained its bulb")
			}
		}
		options.Hanging = .25
		thin := prepareGuideBulbs(grid, boundary, options, false)
		if len(thin) == 0 || len(thin) >= len(full) {
			t.Fatal("density failed to thin bulbs")
		}
		for _, a := range thin {
			if !slices.ContainsFunc(full, func(b Attachment) bool { return reflect.DeepEqual(a, b) }) {
				t.Fatal("density changed a surviving bulb")
			}
		}
		options.Hanging = 0
		if len(prepareGuideBulbs(grid, boundary, options, false)) != 0 {
			t.Fatal("zero hanging density retained bulbs")
		}
		options.Hanging, options.Guides = 1, nil
		if len(prepareGuideBulbs(grid, boundary, options, false)) != 0 {
			t.Fatal("bulbs grew without an adjacent guide")
		}
	}
}

func TestGuideBulbsRejectOtherTopCellsAcrossStalkAndBody(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		grid, boundary := mossTestLedge(mode, 1)
		options := RockGrowthOptions{Hanging: 1, Guides: grassTestGuides(grid[0])}
		bulbs := prepareGuideBulbs(grid, boundary, options, false)
		if len(bulbs) != 1 {
			t.Fatalf("expected one guide bulb, got %d", len(bulbs))
		}
		a := bulbs[0]
		recipe := AtlasRecipes(HangingAtlas)[a.Cell]
		// Test triangle interiors along the root, stalk, and bulb. A tiny
		// blocker inside a triangle has no bulb vertices inside it.
		for triangle := 0; triangle < len(recipe.Indices); triangle += 3 {
			center := geom.V{}
			for _, index := range recipe.Indices[triangle : triangle+3] {
				p := mossTestPoint(recipe.Vertices[index])
				center = center.Add(a.Anchor.Add(terrain.InternalPoint(mode, p.Mul(a.Scale))).Mul(1.0 / 3))
			}
			blocker := grid[0]
			const radius = .00001
			blocker.Polygon = []geom.V{{X: center.X - radius, Y: center.Y - radius}, {X: center.X + radius, Y: center.Y - radius}, {X: center.X + radius, Y: center.Y + radius}, {X: center.X - radius, Y: center.Y + radius}}
			blocker.Center = center
			blocked := append(append(terrain.RockGrid(nil), grid...), blocker)
			if got := prepareGuideBulbs(blocked, boundary, options, false); slices.ContainsFunc(got, func(b Attachment) bool { return b.Anchor.Sub(a.Anchor).Len() < 1e-12 }) {
				t.Fatalf("bulb overlaps another top at triangle %d in %s mode", triangle/3, mode)
			}
			// Darker flanks and invisible cells must not obstruct surface bulbs.
			for _, transparent := range []bool{false, true} {
				blocked[len(grid)] = blocker
				if transparent {
					blocked[len(grid)].Color.A = 0
				} else {
					blocked[len(grid)].Normal = geom.V3{Z: 1}
				}
				if got := prepareGuideBulbs(blocked, boundary, options, false); !slices.ContainsFunc(got, func(b Attachment) bool { return reflect.DeepEqual(a, b) }) {
					t.Fatal("flank or transparent cell obstructed a bulb")
				}
			}
		}
	}
}

func TestGuideBulbsShareWorldPlacementAcrossSectionWindows(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		builder := terrain.NewSectionBuilder(42, nil, mode)
		var previous Attachment
		var previousOrigin geom.V
		for id := int64(0); id < 2; id++ {
			build := builder.Begin(context.Background(), id)
			if build == nil || len(build.Data().Foreground) == 0 {
				t.Fatal("missing section fixture")
			}
			c := build.Data().Foreground[0]
			origin := terrain.RockMaterialPoint(c, geom.V{})
			c.Polygon = []geom.V{{X: .2, Y: -.99}, {X: .8, Y: -.99}, {X: .8, Y: -.95}, {X: .2, Y: -.95}}
			terrain.MapPoints(c.Polygon, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p.Sub(origin)) })
			c.Center = geom.PolygonCenter(c.Polygon)
			c.Color, c.Raised = color.NRGBA{R: 145, A: 255}, true
			up := terrain.InternalPoint(mode, geom.V{Y: -.6})
			c.Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
			bulbs := prepareGuideBulbs(terrain.RockGrid{c}, nil, RockGrowthOptions{Hanging: 1, Guides: grassTestGuides(c)}, false)
			if len(bulbs) != 1 {
				t.Fatalf("expected one bulb in %s section %d, got %d", mode, id, len(bulbs))
			}
			a := bulbs[0]
			if id > 0 {
				worldA := terrain.WorldPoint(mode, previous.Anchor).Add(previousOrigin)
				worldB := terrain.WorldPoint(mode, a.Anchor).Add(origin)
				if worldA.Sub(worldB).Len() > 1e-12 || previous.Cell != a.Cell || previous.Scale != a.Scale {
					t.Fatal("section window moved or reshaped a guide bulb")
				}
			}
			previous, previousOrigin = a, origin
		}
	}
}
