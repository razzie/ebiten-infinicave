package render

import (
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
		if _, indices := appendRockMoss(nil, nil, cell, 1); len(indices) == 0 {
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
	_, indices := appendRockGrass(nil, nil, terrain.RockGrid{cell}, grassTestGuides(cell), true, 1)
	if len(indices) == 0 {
		t.Fatal("grass growing into the owned band was clipped at the section seam")
	}
	cell.Center.Y += 1
	for i := range cell.Polygon {
		cell.Polygon[i].Y += 1
	}
	if _, indices := appendRockGrass(nil, nil, terrain.RockGrid{cell}, grassTestGuides(cell), true, 1); len(indices) != 0 {
		t.Fatal("owned mesh retained distant grass")
	}
}
