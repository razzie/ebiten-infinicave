package render

import (
	"image/color"
	"reflect"
	"testing"

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

func TestRockMossClipsToUpwardForegroundFacets(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		patches, green, yellow := 0, false, false
		for i := 0; i < 40; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.02)
			vertices, indices := appendRockMoss(nil, nil, cell, DefaultMossDensity)
			if len(indices) > 0 {
				patches++
			}
			againVertices, againIndices := appendRockMoss(nil, nil, cell, DefaultMossDensity)
			if !reflect.DeepEqual(vertices, againVertices) || !reflect.DeepEqual(indices, againIndices) {
				t.Fatal("moss changed on repeated mesh preparation")
			}
			for j := 0; j < len(indices); j += 3 {
				center := geom.V{}
				for _, index := range indices[j : j+3] {
					v := vertices[index]
					green = green || v.ColorG > v.ColorR
					yellow = yellow || v.ColorR > v.ColorG
					center = center.Add(geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY})
				}
				if !geom.InsidePolygon(center.Mul(1.0/3), cell.Polygon) {
					t.Fatal("moss paints across the concave notch")
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
				if _, is := appendRockMoss(nil, nil, ineligible, DefaultMossDensity); len(is) != 0 {
					t.Fatalf("moss on %s cell in %s mode", kind, mode)
				}
			}
		}
		if patches == 0 || patches == 40 || !green || !yellow {
			t.Fatalf("expected intermittent green and yellow moss in %s mode: %d patches, green=%v yellow=%v", mode, patches, green, yellow)
		}
	}
}

func TestHangingGrowthFollowsGravityAndNeedsAir(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		bulbs, lichens := 0, 0
		for i := 0; i < 60; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.013)
			// The bottom edge on the left arm faces downward in world axes.
			edge := terrain.RockEdge{A: cell.Polygon[6], B: cell.Polygon[7], Cell: 0}
			growth, ok := hangingGrowthForEdge(cell, edge, DefaultHangingGrowthDensity)
			if !ok {
				continue
			}
			if growth.lichen {
				lichens++
			} else {
				bulbs++
			}
			delta := terrain.WorldPoint(mode, growth.center.Sub(growth.stem[0]))
			if delta.Y <= .01 {
				t.Fatalf("growth hangs against gravity in %s mode: %+v", mode, delta)
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
		if bulbs == 0 || lichens == 0 {
			t.Fatalf("missing hanging variants in %s mode: bulbs=%d lichens=%d", mode, bulbs, lichens)
		}
	}
}

func TestRockGrowthDensityPreservesSurvivors(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		mossCount, hangingCount := 0, 0
		for i := 0; i < 80; i++ {
			cell := growthTestCell(mode, .05+float64(i)*.01)
			vertices, indices := appendRockMoss(nil, nil, cell, .25)
			fullVertices, fullIndices := appendRockMoss(nil, nil, cell, 1)
			if _, is := appendRockMoss(nil, nil, cell, 0); len(is) != 0 {
				t.Fatal("zero density retained moss")
			}
			if len(indices) > 0 {
				mossCount++
				if !reflect.DeepEqual(vertices, fullVertices) || !reflect.DeepEqual(indices, fullIndices) {
					t.Fatal("changing density moved or reshaped surviving moss")
				}
			}
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
		if mossCount == 0 || mossCount == 80 || hangingCount == 0 || hangingCount == 80 {
			t.Fatalf("density failed to thin growth: moss=%d hanging=%d", mossCount, hangingCount)
		}
	}
}

func TestRockGrowthDensityReachesAllMeshPreparationPaths(t *testing.T) {
	cell := growthTestCell(terrain.Vertical, .3)
	topology := terrain.NewRockTopology(terrain.RockGrid{cell})
	topology.Appearance = topology.Grid
	guides := grassTestGuides(cell)
	data := terrain.SectionData{Foreground: topology.Grid, ForegroundTopology: topology, Guides: guides}
	bare := PrepareGridWithTopology(nil, ViewShaded, topology, RockGrowthOptions{})
	for _, density := range []RockGrowthOptions{{Moss: 1}, {Hanging: 1}, {Grass: 1}, {Moss: 1, Hanging: 1, Grass: 1}} {
		density.Guides = guides
		full := PrepareGridWithTopology(nil, ViewShaded, topology, density)
		if len(full.Faces.Indices) <= len(bare.Faces.Indices) {
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
