package render

import (
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

func TestGrassTuftsUseRearBladesAndForegroundSod(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		rear, front, sparse := 0, 0, 0
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
			if len(all) == 0 {
				t.Fatal("eligible tuft disappeared")
			}
			if all[0].Kind == BackgroundGrassAttachment {
				rear++
				if len(all) != 1 || !all[0].Underlay || all[0].Mesh.Texture != GrassAtlas || !UnderGrowthSpan.Contains(all[0]) || GrassSpan.Contains(all[0]) {
					t.Fatal("rear tuft must draw only blades behind rocks")
				}
			} else {
				front++
				if len(all) != 2 || all[0].Mesh.Texture != NoAtlas || all[1].Mesh.Texture != GrassAtlas {
					t.Fatal("foreground tuft lost its sod or blades")
				}
				for _, a := range all {
					if !GrassSpan.Contains(a) || UnderGrowthSpan.Contains(a) {
						t.Fatal("foreground grass moved behind rocks")
					}
				}
			}
			options.Grass = .25
			if survivor := prepareGrowthAttachments(grid, topology.Boundary, options, false); len(survivor) > 0 {
				sparse++
				if !reflect.DeepEqual(survivor, all) {
					t.Fatal("density changed a surviving tuft's layer or shape")
				}
			}
			owned := prepareGrowthAttachments(grid, topology.Boundary, RockGrowthOptions{Grass: 1, Guides: options.Guides}, true)
			if !reflect.DeepEqual(owned, all) {
				t.Fatal("owned preparation changed a visible tuft")
			}
			cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: all[0].Anchors[0], Radius: .0003})
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range FilterAttachments(all, []terrain.RockCut{cut}, 0) {
				if !a.Dead {
					t.Fatal("cut support retained grass")
				}
			}
		}
		if rear == 0 || front == 0 || sparse == 0 {
			t.Fatalf("missing grass variation in %s: rear=%d front=%d sparse=%d", mode, rear, front, sparse)
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
