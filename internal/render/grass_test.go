package render

import (
	"context"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func grassTestGuides(cell terrain.RockCell) []terrain.Guide {
	mode := terrain.RockOrientation(cell)
	world := append([]geom.V(nil), cell.Polygon...)
	terrain.MapPoints(world, func(p geom.V) geom.V { return terrain.WorldPoint(mode, p) })
	lo, hi := geom.PolygonBounds(world)
	return []terrain.Guide{terrain.SplineGuide([]geom.V{
		terrain.InternalPoint(mode, geom.V{X: lo.X - .01, Y: lo.Y}),
		terrain.InternalPoint(mode, geom.V{X: hi.X + .01, Y: lo.Y}),
	}, 1)}
}

func TestGrassTuftsFollowGravityAndPreserveDensitySurvivors(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		retained, green, yellow := 0, false, false
		for i := 0; i < 80; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.01)
			guides := grassTestGuides(cell)
			full := grassForCell(cell, guides, 1)
			sparse := grassForCell(cell, guides, .25)
			if len(full.blades) == 0 {
				t.Fatal("maximum density omitted a supported tuft")
			}
			green = green || full.color.G > full.color.R
			yellow = yellow || full.color.R > full.color.G
			if len(sparse.blades) > 0 {
				retained++
				if !reflect.DeepEqual(sparse, full) {
					t.Fatal("density changed a surviving tuft's roots, shape or palette")
				}
			}
			for _, blade := range full.blades {
				if !geom.InsidePolygon(blade.root, cell.Polygon) {
					t.Fatal("grass rooted in the concave notch")
				}
				_, distance := guides[0].NearestPoint(blade.root)
				if distance > grassRootGuideDistance+1e-12 {
					t.Fatal("grass roots stray away from their guide")
				}
				grounded := false
				for _, ground := range full.ground {
					grounded = grounded || geom.InsidePolygon(blade.root, ground)
				}
				if !grounded {
					t.Fatal("grass is growing out of bare stone")
				}
				if geom.PolygonArea(blade.polygon) <= 0 || len(geom.Triangulate(blade.polygon)) == 0 {
					t.Fatal("grass blade cannot be rendered")
				}
				tip := terrain.WorldPoint(mode, blade.polygon[3].Sub(blade.root))
				if tip.Y >= -.005 {
					t.Fatalf("grass grows against world-up in %s mode: %+v", mode, tip)
				}
			}
			for _, ground := range full.ground {
				if !geom.InsidePolygon(geom.PolygonCenter(ground), cell.Polygon) {
					t.Fatal("grass ground patch paints across the concave notch")
				}
			}
			if len(grassForCell(cell, guides, 0).blades) != 0 {
				t.Fatal("zero density retained grass")
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
				if len(grassForCell(ineligible, guides, 1).blades) != 0 {
					t.Fatalf("grass on %s cell in %s mode", kind, mode)
				}
			}
		}
		if retained == 0 || retained == 80 || !green || !yellow {
			t.Fatalf("missing sparse grass or palette variation in %s mode: retained=%d green=%v yellow=%v", mode, retained, green, yellow)
		}
	}
}

func TestGrassRequiresAdjacentGuidesAndSurvivingCrest(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		cell := growthTestCell(mode, .3)
		guides := grassTestGuides(cell)
		if len(grassForCell(cell, guides, 1).blades) == 0 {
			t.Fatal("fixture has no guide-side grass")
		}
		if len(grassForCell(cell, nil, 1).blades) != 0 {
			t.Fatal("grass grows without guides")
		}
		shift := terrain.InternalPoint(mode, geom.V{Y: -.02})
		remote := grassTestGuides(cell)
		for i := range remote[0].Pts {
			remote[0].Pts[i] = remote[0].Pts[i].Add(shift)
		}
		if len(grassForCell(cell, remote, 1).blades) != 0 {
			t.Fatal("grass grows on an interior face away from the guide")
		}
		// Remove the guide-side strip while retaining the upward-facing cell.
		up := terrain.InternalPoint(mode, geom.V{Y: -1})
		crest := guides[0].Pts[0].Dot(up) - .006
		cut := cell
		cut.Polygon = geom.ClipHalfPlane(cell.Polygon, up, crest)
		cut.Center = geom.PolygonCenter(cut.Polygon)
		if len(grassForCell(cut, guides, 1).blades) != 0 {
			t.Fatal("grass reappeared on the cut below a removed guide crest")
		}
		if len(prepareGrowthAttachments(terrain.RockGrid{cell}, nil, RockGrowthOptions{Moss: 1}, false)) == 0 {
			t.Fatal("grass's guide restriction affected moss")
		}
	}
}

func TestGrassAtlasAndGroundSampleWorldSpaceHue(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		green, yellow := false, false
		for i := range 80 {
			cell := growthTestCell(mode, .05+float64(i)*.01)
			patch, blades, ok := prepareGrassAttachments(cell, RockGrowthOptions{Grass: 1, Guides: grassTestGuides(cell)})
			if !ok {
				t.Fatal("fixture needs supported grass")
			}
			wantYellow := terrain.VegetationGreen(terrain.RockMaterialPoint(cell, patch.Anchor)) < .5
			green, yellow = green || !wantYellow, yellow || wantYellow
			for _, v := range patch.Mesh.Vertices {
				if (v.ColorR > v.ColorG) != wantYellow {
					t.Fatal("grass ground ignores world-space hue")
				}
			}
			for _, v := range blades.Mesh.Vertices {
				spec := specForAtlas(GrassAtlas)
				sample := int(v.SrcX)/spec.pixels + int(v.SrcY)/spec.pixels*spec.columns
				atlasYellow := terrain.SiteRandom(0x677261737361746c, int64(sample), 0, 0) < .48
				if atlasYellow != wantYellow {
					t.Fatal("grass blade atlas hue differs from its ground")
				}
			}
		}
		if !green || !yellow {
			t.Fatal("grass fixture must exercise both coordinate-based hues")
		}
	}
}

func TestOwnedGrassIncludesTuftsCrossingSectionSeam(t *testing.T) {
	cell := growthTestCell(terrain.Vertical, .3)
	cell.Center.Y += .605
	for i := range cell.Polygon {
		cell.Polygon[i].Y += .605
	}
	if inTerrainBand(cell.Polygon...) {
		t.Fatal("fixture rock overlaps the owned band")
	}
	grass := prepareRockGrass(terrain.RockGrid{cell}, grassTestGuides(cell), true, 1)
	if len(grass.Indices) == 0 {
		t.Fatal("grass growing into the owned band was clipped at the section seam")
	}
	cell.Center.Y += 1
	for i := range cell.Polygon {
		cell.Polygon[i].Y += 1
	}
	if grass := prepareRockGrass(terrain.RockGrid{cell}, grassTestGuides(cell), true, 1); len(grass.Indices) != 0 {
		t.Fatal("owned mesh retained distant grass")
	}
}

func TestGrassLayerIncludesGroundAndPreservesOwnedSeams(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for i := 0; i < 20; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.025)
			guides := grassTestGuides(cell)
			grass := prepareRockGrass(terrain.RockGrid{cell}, guides, false, 1)
			checkMesh(t, grass)
			ground, blades := false, false
			for _, v := range grass.Vertices {
				ground = ground || v.Custom2 != 0 || v.Custom3 != 0
				blades = blades || (v.Custom2 == 0 && v.Custom3 == 0)
			}
			if !ground || !blades {
				t.Fatal("grass layer must include both ground patches and blades")
			}
			topology := terrain.NewRockTopology(terrain.RockGrid{cell})
			topology.Appearance = topology.Grid
			bare := PrepareGridWithTopology(nil, ViewShaded, topology, RockGrowthOptions{})
			full := PrepareGridWithTopology(nil, ViewShaded, topology, RockGrowthOptions{Grass: 1, Guides: guides})
			if !reflect.DeepEqual(bare.Faces, full.Faces) || !reflect.DeepEqual(bare.Outlines, full.Outlines) {
				t.Fatal("grass ground patches or blades remain baked into the rock layer")
			}
			owned := prepareRockGrass(terrain.RockGrid{cell}, guides, true, 1)
			for _, pixels := range []int{1, 501, 1000} {
				a := ScaleGridMesh(GridMesh{Grass: grass}, pixels, 0)
				b := ScaleGridMesh(GridMesh{Grass: owned}, pixels, 0)
				if !reflect.DeepEqual(bandTriangles(a.Grass, pixels), bandTriangles(b.Grass, pixels)) {
					t.Fatal("grass layer lost visible ground or blades at a section boundary")
				}
			}
			sparse := prepareRockGrass(terrain.RockGrid{cell}, guides, false, .25)
			if len(sparse.Indices) > 0 && !reflect.DeepEqual(sparse, grass) {
				t.Fatal("density changed a surviving tuft's ground or blades")
			}
		}
	}
}

func TestGrassPatchFollowsSurfaceAndBladesGrowUp(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		angle := .65
		anchor := geom.V{X: .5, Y: .5}
		point := func(p geom.V) geom.V { return anchor.Add(terrain.InternalPoint(mode, geom.Rotate(p, angle))) }
		poly := []geom.V{point(geom.V{X: -.06}), point(geom.V{X: .06}), point(geom.V{X: .06, Y: .06}), point(geom.V{X: -.06, Y: .06})}
		n := terrain.InternalPoint(mode, geom.Rotate(geom.V{Y: -.8}, angle+.30))
		c := terrain.WithRockOrientation(terrain.RockCell{Polygon: poly, Center: geom.PolygonCenter(poly), Normal: geom.V3{X: n.X, Y: n.Y, Z: .6}, Color: color.NRGBA{R: 140, A: 255}, Raised: true}, mode)
		guide := terrain.Guide{Pts: []geom.V{poly[0], poly[1]}}
		patch, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
		if !ok || len(patch.Mesh.Indices) == 0 || len(blades.Mesh.Indices) == 0 {
			t.Fatal("missing sloped grass")
		}
		if math.Abs(patch.Rotation-angle) > 1e-9 {
			t.Fatal("patch ignores the geometric edge")
		}
		// Lighting can change independently of the geometric supporting edge.
		changed := c
		n = terrain.InternalPoint(mode, geom.Rotate(geom.V{Y: -.8}, angle-.25))
		changed.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		other, _, ok := prepareGrassAttachments(changed, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
		if !ok || !reflect.DeepEqual(patch.Anchors, other.Anchors) || !reflect.DeepEqual(patch.Footprint, other.Footprint) {
			t.Fatal("lighting rotated or relocated the grass base")
		}
		for _, root := range patch.Anchors {
			inside := false
			for _, piece := range patch.Footprint {
				if geom.InsidePolygon(root, piece) {
					inside = true
					break
				}
			}
			if !inside {
				t.Fatal("blade root lies outside the ground strip")
			}
		}
		for _, v := range patch.Mesh.Vertices {
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			local := geom.Rotate(terrain.WorldPoint(mode, p.Sub(anchor)), -angle)
			if local.Y < -grassEdgeOverlap-1e-6 || local.Y > .010 {
				t.Fatal("ground patch floats away from its facet")
			}
		}
		for i := 0; i < len(blades.Mesh.Vertices); i += 4 {
			a, b := blades.Mesh.Vertices[i], blades.Mesh.Vertices[i+1]
			d := terrain.WorldPoint(mode, geom.V{X: float64(b.DstX - a.DstX), Y: float64(b.DstY - a.DstY)})
			if math.Abs(math.Atan2(d.Y, d.X)-angle*.5) > .65 {
				t.Fatal("blades lost their partial upward lean from the surface")
			}
		}
		// Body-only damage is handled by texture erasure, with no deletion.
		var tip geom.V
		bestY := math.Inf(1)
		for _, v := range blades.Mesh.Vertices {
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			if y := terrain.WorldPoint(mode, p).Y; y < bestY {
				tip, bestY = p, y
			}
		}
		cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: tip, Radius: .0003})
		if err != nil {
			t.Fatal(err)
		}
		if patch.Hit(cut, 0) || blades.Hit(cut, 0) {
			t.Fatal("blade-tip damage removed a supported tuft")
		}
		cut, _ = terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: patch.Anchors[0], Radius: .0003})
		if !patch.Hit(cut, 0) || !blades.Hit(cut, 0) {
			t.Fatal("support damage separated grass blades from their patch")
		}
	}
}

func TestGrassBaseFitsOffsetGuideAndCellCorner(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		point := func(p geom.V) geom.V { return geom.V{X: .5, Y: .5}.Add(terrain.InternalPoint(mode, p)) }
		poly := []geom.V{point(geom.V{X: -.04}), point(geom.V{}), point(geom.V{X: .04, Y: .018}), point(geom.V{X: .04, Y: .08}), point(geom.V{X: -.04, Y: .08})}
		c := terrainRock(poly)
		c = terrain.WithRockOrientation(c, mode)
		n := terrain.InternalPoint(mode, geom.V{Y: -.8})
		c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		guide := terrain.Guide{Pts: []geom.V{point(geom.V{X: -.04, Y: -.002}), point(geom.V{Y: -.002}), point(geom.V{X: .04, Y: .016})}}
		patch, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
		if !ok {
			t.Fatal("supported corner lost its tuft")
		}
		for _, m := range []TriangleMesh{patch.Mesh, blades.Mesh} {
			checkMesh(t, m)
		}
		for _, v := range patch.Mesh.Vertices {
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			if !geom.InsidePolygon(p, c.Polygon) {
				// The sod lip overlaps the silhouette to hide its AA fringe.
				distance := math.Inf(1)
				for j, a := range c.Polygon {
					distance = min(distance, geom.SegmentDistanceSquared(p, p, a, c.Polygon[(j+1)%len(c.Polygon)]))
				}
				if distance > math.Pow(grassEdgeOverlap+.0005, 2) {
					t.Fatal("ground strip extends beyond its narrow edge overlap")
				}
			}
		}
		for _, root := range patch.Anchors {
			_, distance := guide.NearestPoint(root)
			if distance > .010 {
				t.Fatal("root no longer follows the eligible crest")
			}
		}
	}
}

func TestGrassPatchCoversSegmentedRockEdge(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		point := func(p geom.V) geom.V { return geom.V{X: .5, Y: .5}.Add(terrain.InternalPoint(mode, p)) }
		var poly []geom.V
		// Many short segments reproduce chipped silhouettes, including edges
		// shorter than the old strip cutoff. All still belong to one tuft.
		for j := 0; j <= 80; j++ {
			poly = append(poly, point(geom.V{X: -.02 + float64(j)*.0005}))
		}
		poly = append(poly, point(geom.V{X: .02, Y: .06}), point(geom.V{X: -.02, Y: .06}))
		c := terrain.WithRockOrientation(terrainRock(poly), mode)
		n := terrain.InternalPoint(mode, geom.V{Y: -.8})
		c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		guides := []terrain.Guide{{Pts: []geom.V{poly[0], poly[80]}}}
		patch, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: guides})
		if !ok || len(blades.Mesh.Vertices)/4 < 5 || len(blades.Mesh.Vertices)/4 > 7 {
			t.Fatal("segmented fixture lost its compact blade fan")
		}
		checkMesh(t, patch.Mesh)
		for j := -12; j <= 12; j++ {
			// Test the actual crest and the strip immediately below it, on
			// both sides of the anchor, including the outer overlap.
			for _, depth := range []float64{-.0015, 1e-9, .0001, .001} {
				p := patch.Anchor.Add(terrain.InternalPoint(mode, geom.V{X: float64(j) * .001, Y: depth}))
				covered := false
				for _, piece := range patch.Footprint {
					covered = covered || geom.InsidePolygon(p, piece)
					for k, a := range piece {
						covered = covered || geom.SegmentDistanceSquared(p, p, a, piece[(k+1)%len(piece)]) < 1e-20
					}
				}
				if !covered {
					t.Fatalf("bare rock edge in %s mode at %+v", mode, p)
				}
			}
		}
	}
}

func TestGrassColoniesUseRearBladesAndForegroundSod(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		rear, front, sparse, mixed := 0, 0, 0, 0
		counts := make(map[int]bool)
		for i := 0; i < 80; i++ {
			c := growthTestCell(mode, .05+float64(i)*.01)
			if mode == terrain.Horizontal {
				c.Center.Y++
				for j := range c.Polygon {
					c.Polygon[j].Y++
				}
			}
			grid := terrain.RockGrid{c}
			topology := terrain.NewRockTopology(grid)
			options := RockGrowthOptions{Grass: 1, Guides: grassTestGuides(c)}
			all := prepareGrowthAttachments(grid, topology.Boundary, options, false)
			tufts := make(map[geom.V][]Attachment)
			for _, a := range all {
				tufts[a.Anchor] = append(tufts[a.Anchor], a)
			}
			if len(tufts) < 2 || len(tufts) > 3 {
				t.Fatalf("colony in %s mode has %d instances, want 2–3", mode, len(tufts))
			}
			counts[len(tufts)] = true
			colonyRear, colonyFront := false, false
			for _, tuft := range tufts {
				if tuft[0].Kind == BackgroundGrassAttachment {
					rear++
					colonyRear = true
					if len(tuft) != 1 || !tuft[0].Underlay || tuft[0].Mesh.Texture != GrassAtlas || !UnderGrowthSpan.Contains(tuft[0]) || GrassSpan.Contains(tuft[0]) {
						t.Fatal("rear tuft must draw only blades behind rocks")
					}
				} else {
					front++
					colonyFront = true
					if len(tuft) != 2 || tuft[0].Mesh.Texture != NoAtlas || tuft[1].Mesh.Texture != GrassAtlas || !reflect.DeepEqual(tuft[0].Anchors, tuft[1].Anchors) {
						t.Fatal("foreground tuft lost its paired sod and blades")
					}
					for _, a := range tuft {
						if !GrassSpan.Contains(a) || UnderGrowthSpan.Contains(a) {
							t.Fatal("foreground grass moved behind rocks")
						}
					}
				}
			}
			if colonyRear && colonyFront {
				mixed++
			}
			if !reflect.DeepEqual(all, prepareGrowthAttachments(grid, topology.Boundary, options, false)) {
				t.Fatal("colony changed on rebuild")
			}
			options.Grass = .25
			if survivor := prepareGrowthAttachments(grid, topology.Boundary, options, false); len(survivor) > 0 {
				sparse++
				if !reflect.DeepEqual(survivor, all) {
					t.Fatal("density changed a surviving colony's layers or shape")
				}
			}
			owned := prepareGrowthAttachments(grid, topology.Boundary, RockGrowthOptions{Grass: 1, Guides: options.Guides}, true)
			if !reflect.DeepEqual(owned, all) {
				t.Fatal("owned preparation changed a visible colony")
			}
			cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: all[0].Anchors[0], Radius: .0001})
			if err != nil {
				t.Fatal(err)
			}
			filtered := FilterAttachments(all, []terrain.RockCut{cut}, 0)
			dead, live := 0, 0
			for j, a := range filtered {
				if a.Dead != all[j].Hit(cut, 0) {
					t.Fatal("colony support damage removed the wrong instance")
				}
				if a.Dead {
					dead++
				} else {
					live++
				}
			}
			if dead == 0 || live == 0 {
				t.Fatal("support damage must remove its tuft while retaining neighbors")
			}
		}
		if rear == 0 || front == 0 || sparse == 0 || mixed == 0 || len(counts) != 2 {
			t.Fatalf("missing colony variation in %s: rear=%d front=%d sparse=%d mixed=%d counts=%v", mode, rear, front, sparse, mixed, counts)
		}
	}
}

func TestGrassColoniesFollowConnectedSurface(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		point := func(p geom.V) geom.V { return geom.V{X: .5, Y: .5}.Add(terrain.InternalPoint(mode, p)) }
		poly := []geom.V{point(geom.V{X: -.04, Y: -.015}), point(geom.V{}), point(geom.V{X: .04, Y: -.015}), point(geom.V{X: .04, Y: .06}), point(geom.V{X: -.04, Y: .06})}
		c := terrain.WithRockOrientation(terrainRock(poly), mode)
		n := terrain.InternalPoint(mode, geom.V{Y: -.8})
		c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		guide := terrain.Guide{Pts: poly[:3]}
		support := []terrain.RockEdge{{A: poly[0], B: poly[1]}, {A: poly[1], B: poly[2]}}
		colony := prepareGrassColony(c, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}}, support)
		if len(colony) < 2 || len(colony) > 3 {
			t.Fatal("corner lost its colony")
		}
		left, right := false, false
		for _, tuft := range colony {
			patch, blades := tuft[0], tuft[1]
			checkMesh(t, patch.Mesh)
			checkMesh(t, blades.Mesh)
			world := terrain.WorldPoint(mode, patch.Anchor.Sub(poly[1]))
			left, right = left || world.X < 0, right || world.X > 0
			_, distance := guide.NearestPoint(patch.Anchor)
			if distance > 1e-10 {
				t.Fatal("colony instance floated away from its rock surface")
			}
			for _, root := range patch.Anchors {
				if !geom.InsidePolygon(root, c.Polygon) {
					t.Fatal("colony blade has no rock support")
				}
			}
			angle := math.Atan2(-.015, .04)
			if world.X < 0 {
				angle = -angle
			}
			if math.Abs(patch.Rotation-angle) > 1e-10 {
				t.Fatal("colony instance ignored its local surface slope")
			}
		}
		if !left || !right {
			t.Fatal("colony did not follow the surface around its corner")
		}
		// Walking a crest must stop at missing support rather than jumping
		// across a nearby air gap to another exposed segment.
		support[1].A = geom.LerpVector(poly[1], poly[2], .1)
		p, edge, distance := grassSurfacePoint(c, guide, support, 0, poly[1], .02)
		if p != poly[1] || edge != 0 || distance != 0 {
			t.Fatal("colony crossed an unsupported gap")
		}
	}
}

func TestGrassColoniesShareWorldCoordinatesAcrossSectionWindows(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		builder := terrain.NewSectionBuilder(42, nil, mode)
		var previous []Attachment
		var previousCell terrain.RockCell
		for id := int64(0); id < 2; id++ {
			build := builder.Begin(context.Background(), id)
			if build == nil || len(build.Data().Foreground) == 0 {
				t.Fatal("missing section fixture")
			}
			// Preserve the private material origin while placing one ledge in
			// the same world position through both padded section frames.
			c := build.Data().Foreground[0]
			origin := terrain.RockMaterialPoint(c, geom.V{})
			c.Polygon = []geom.V{{X: .2, Y: -.99}, {X: .28, Y: -.99}, {X: .28, Y: -.95}, {X: .2, Y: -.95}}
			if mode == terrain.Horizontal {
				c.Polygon = []geom.V{{X: .99, Y: .2}, {X: 1.07, Y: .2}, {X: 1.07, Y: .24}, {X: .99, Y: .24}}
			}
			terrain.MapPoints(c.Polygon, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p.Sub(origin)) })
			c.Center = geom.PolygonCenter(c.Polygon)
			c.Color, c.Raised = color.NRGBA{R: 145, A: 255}, true
			n := terrain.InternalPoint(mode, geom.V{Y: -.8})
			c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
			grid := terrain.RockGrid{c}
			topology := terrain.NewRockTopology(grid)
			all := prepareGrowthAttachments(grid, topology.Boundary, RockGrowthOptions{Grass: 1, Guides: grassTestGuides(c)}, false)
			if len(all) < 2 {
				t.Fatalf("section fixture lost its colony in %s mode at section %d (cell mode %s)", mode, id, terrain.RockOrientation(c))
			}
			if id > 0 {
				if len(all) != len(previous) {
					t.Fatal("section window changed colony size or depth")
				}
				for i, a := range all {
					b := previous[i]
					if a.Kind != b.Kind || math.Abs(a.Scale-b.Scale) > 1e-12 || math.Abs(a.Rotation-b.Rotation) > 1e-12 || terrain.RockMaterialPoint(c, a.Anchor).Sub(terrain.RockMaterialPoint(previousCell, b.Anchor)).Len() > 1e-12 || !reflect.DeepEqual(a.Mesh.Indices, b.Mesh.Indices) || len(a.Mesh.Vertices) != len(b.Mesh.Vertices) {
						t.Fatal("section window changed a colony instance")
					}
					for j, v := range a.Mesh.Vertices {
						w := b.Mesh.Vertices[j]
						p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
						q := geom.V{X: float64(w.DstX) / RasterPixelsPerUnit, Y: float64(w.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
						if terrain.RockMaterialPoint(c, p).Sub(terrain.RockMaterialPoint(previousCell, q)).Len() > 1e-6 || v.SrcX != w.SrcX || v.SrcY != w.SrcY || v.ColorR != w.ColorR || v.ColorG != w.ColorG || v.ColorB != w.ColorB {
							t.Fatal("section window moved or recolored grass")
						}
					}
				}
			}
			previous, previousCell = all, c
		}
	}
}

func TestGrassSodCoversProtrudingRockTrim(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		poly := []geom.V{{X: -.04}, {X: .04}, {X: .04, Y: .06}, {X: -.04, Y: .06}}
		terrain.MapPoints(poly, func(p geom.V) geom.V { return geom.V{X: .5, Y: .5}.Add(terrain.InternalPoint(mode, p)) })
		c := terrain.WithRockOrientation(terrainRock(poly), mode)
		n := terrain.InternalPoint(mode, geom.V{Y: -.8})
		c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		patch, _, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: grassTestGuides(c)})
		if !ok {
			t.Fatal("missing grass fixture")
		}
		right := terrain.InternalPoint(mode, geom.V{X: 1})
		up := terrain.InternalPoint(mode, geom.V{Y: -1})
		tip := patch.Anchor.Add(up.Mul(.006))
		trim := visualTooth(c, patch.Anchor.Sub(right.Mul(.006)), patch.Anchor.Add(right.Mul(.006)), tip, up, false, ViewShaded)
		if len(trim) == 0 {
			t.Fatal("missing protruding tooth")
		}
		supports := len(patch.Anchors)
		coverGrassTrim(&patch, trim)
		covered := false
		for _, piece := range patch.Footprint {
			covered = covered || geom.InsidePolygon(tip.Sub(up.Mul(.0001)), piece)
		}
		if !covered || len(patch.Anchors) <= supports {
			t.Fatal("rock spike protrudes through sod or has no live support")
		}
		checkMesh(t, patch.Mesh)
		cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: trim[0].Anchors[0], Radius: .00002})
		if err != nil {
			t.Fatal(err)
		}
		if !patch.Hit(cut, 0) {
			t.Fatal("removing a sod-covered spike leaves floating grass")
		}
	}
}

func TestGrassBladeAnglesFollowSurfaceWithPartialUpwardLean(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for _, angle := range []float64{-math.Pi / 2, -1, 0, 1, math.Pi / 2} {
			poly := []geom.V{{X: -.06}, {X: .06}, {X: .06, Y: .06}, {X: -.06, Y: .06}}
			terrain.MapPoints(poly, func(p geom.V) geom.V {
				return geom.V{X: .5, Y: .5}.Add(terrain.InternalPoint(mode, geom.Rotate(p, angle)))
			})
			c := terrain.WithRockOrientation(terrainRock(poly), mode)
			n := terrain.InternalPoint(mode, geom.V{Y: -.8})
			c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
			guide := terrain.Guide{Pts: []geom.V{poly[0], poly[1]}}
			_, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
			if !ok {
				t.Fatalf("missing grass on surface angle %g in %s mode", angle, mode)
			}
			mean, lo, hi := 0.0, math.Inf(1), math.Inf(-1)
			for i := 0; i < len(blades.Mesh.Vertices); i += 4 {
				a, b := blades.Mesh.Vertices[i], blades.Mesh.Vertices[i+1]
				d := terrain.WorldPoint(mode, geom.V{X: float64(b.DstX - a.DstX), Y: float64(b.DstY - a.DstY)})
				rotation := math.Atan2(d.Y, d.X)
				mean += rotation
				lo, hi = min(lo, rotation), max(hi, rotation)
			}
			mean /= float64(len(blades.Mesh.Vertices) / 4)
			if math.Abs(mean-angle*.5) > .065 || hi-lo < .9 {
				t.Fatalf("surface %g in %s mode: blade fan has mean %g and spread %g", angle, mode, mean, hi-lo)
			}
		}
	}
}

func TestGrassSodShadeMatchesBladeBases(t *testing.T) {
	for i := 0; i < 20; i++ {
		c := growthTestCell(terrain.Vertical, .05+float64(i)*.025)
		patch, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: grassTestGuides(c)})
		if !ok {
			t.Fatal("missing grass color fixture")
		}
		s := specForAtlas(GrassAtlas)
		quad := blades.Mesh.Vertices[0]
		cell := int(quad.SrcY)/s.pixels*s.columns + int(quad.SrcX)/s.pixels
		sample := AtlasRecipes(GrassAtlas)[cell]
		base := sample.Vertices[0]
		for _, v := range sample.Vertices {
			if v.DstY > base.DstY {
				base = v
			}
		}
		sod := patch.Mesh.Vertices[0]
		for _, pair := range [][2]float32{{sod.ColorR, base.ColorR * quad.ColorR}, {sod.ColorG, base.ColorG * quad.ColorG}, {sod.ColorB, base.ColorB * quad.ColorB}} {
			if math.Abs(float64(pair[0]/pair[1])-1) > .14 {
				t.Fatal("sod shade differs too much from the actual blade base")
			}
		}
	}
}
