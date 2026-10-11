package terrain

import (
	"context"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func acuteWebFixture(t *testing.T, angle float64) (RockGrid, geom.V) {
	t.Helper()
	for i := 0; i < 50; i++ {
		p := geom.V{X: .2 + .01*float64(i), Y: .4}
		key := CollisionVertexKey(p)
		if SiteRandom(0x737069646572, key[0], key[1], 0) > .95 {
			continue
		}
		b := geom.Rotate(geom.V{X: .12}, angle)
		poly := []geom.V{
			p.Add(geom.V{X: -.15, Y: -.1}), p.Add(geom.V{X: .15, Y: -.1}),
			p.Add(geom.V{X: .15}), p, p.Add(b), p.Add(geom.V{X: -.15, Y: b.Y}),
		}
		cell := terrainRock(poly)
		cell.Color = color.NRGBA{R: 22, G: 21, B: 19, A: 255}
		return RockGrid{cell}, p
	}
	t.Fatal("fixture has no selected corner")
	return nil, geom.V{}
}

func TestSpiderWebCandidatesRequireAnAcuteEmptyPocket(t *testing.T) {
	for _, degrees := range []float64{15, 60, 90, 120, 150} {
		grid, p := acuteWebFixture(t, degrees*math.Pi/180)
		corners := findWebCorners(grid, ExposedRockEdges(grid))
		if degrees == 60 {
			if len(corners) == 0 || corners[0].web.Corner.Sub(p).Len() > 1e-9 {
				t.Fatalf("acute corner was missed: %+v", corners)
			}
			blocked := append(append(RockGrid(nil), grid...), terrainRect(p.X+.003, p.Y+.003, .1, .1))
			if len(findWebCorners(blocked, ExposedRockEdges(grid))) != 0 {
				t.Fatal("web crosses an adjacent formation")
			}
		} else if len(corners) != 0 {
			t.Fatalf("%g degree opening produced a web", degrees)
		}
	}
	convex := RockGrid{terrainRect(.1, .1, .6, .6)}
	if len(findWebCorners(convex, ExposedRockEdges(convex))) != 0 {
		t.Fatal("a convex corner produced an unsupported web")
	}
}

func TestSpiderWebCornersDoNotDependOnWallSegmentCount(t *testing.T) {
	grid, _ := acuteWebFixture(t, math.Pi/3)
	want := findWebCorners(grid, ExposedRockEdges(grid))
	if len(want) == 0 {
		t.Fatal("reference pocket has no candidate")
	}
	for _, segments := range []int{32, 1024} {
		dense := append(RockGrid(nil), grid...)
		var poly []geom.V
		for i, a := range grid[0].Polygon {
			b := grid[0].Polygon[(i+1)%len(grid[0].Polygon)]
			count := 1
			if i == 2 || i == 3 {
				count = segments
			}
			for j := 0; j < count; j++ {
				poly = append(poly, a.Add(b.Sub(a).Mul(float64(j)/float64(count))))
			}
		}
		dense[0].Polygon = poly
		got := findWebCorners(dense, ExposedRockEdges(dense))
		if len(got) != len(want) {
			t.Fatalf("%d segments per wall changed candidate count: got %d, want %d", segments, len(got), len(want))
		}
		for i, c := range got {
			w := want[i].web
			if c.web.Corner.Sub(w.Corner).Len() > 1e-9 || c.web.A.Sub(w.A).Len() > 1e-9 || c.web.B.Sub(w.B).Len() > 1e-9 {
				t.Fatalf("%d segments per wall changed the fitted web", segments)
			}
		}
	}
}

func TestSpiderWebFindsRoundedPocketWithDenseWalls(t *testing.T) {
	grid, p := acuteWebFixture(t, math.Pi/3)
	start := p.Add(geom.V{X: .012})
	end := p.Add(geom.Rotate(geom.V{X: .012}, math.Pi/3))
	var poly []geom.V
	poly = append(poly, grid[0].Polygon[:2]...)
	wall := func(a, b geom.V) {
		for i := 0; i < 1024; i++ {
			poly = append(poly, a.Add(b.Sub(a).Mul(float64(i)/1024)))
		}
	}
	wall(grid[0].Polygon[2], start)
	for i := 0; i < 64; i++ {
		u := float64(i) / 64
		poly = append(poly, start.Mul((1-u)*(1-u)).Add(p.Mul(2*u*(1-u))).Add(end.Mul(u*u)))
	}
	wall(end, grid[0].Polygon[4])
	poly = append(poly, grid[0].Polygon[4:]...)
	grid[0].Polygon = poly
	corners := findWebCorners(grid, ExposedRockEdges(grid))
	if len(corners) == 0 || len(resolveSpiderWebs(grid, corners)) == 0 {
		t.Fatal("rounded acute pocket with tiny contour segments was missed")
	}
	for _, c := range corners {
		if c.web.Corner.Sub(p).Len() > .012 {
			t.Fatal("candidate moved away from the rounded pocket root")
		}
	}
	bright := append(RockGrid(nil), grid...)
	bright[0].Color = color.NRGBA{R: 170, G: 163, B: 145, A: 255}
	if len(resolveSpiderWebs(bright, corners)) != 0 {
		t.Fatal("rounded pocket bypassed the dark-support requirement")
	}
}

func TestSpiderWebAnchorCrossesSmallContourReversals(t *testing.T) {
	points := []geom.V{{}, {X: .0005}, {X: .0002, Y: .0002}, {X: .002}, {X: .08}}
	var edges []RockEdge
	links := make(map[[2]int64]int)
	for i := 0; i+1 < len(points); i++ {
		edges = append(edges, RockEdge{A: points[i], B: points[i+1], Cell: i})
		links[CollisionVertexKey(points[i])] = i
	}
	anchor, cells := webAnchor(points[0], 0, true, .04, edges, links)
	if anchor.X < .035 || anchor.X > .04 || anchor.Y != 0 || len(cells) != 4 {
		t.Fatalf("small jagged turn stopped the wall trace: anchor %+v, supports %v", anchor, cells)
	}
}

func TestSpiderWebsRequireDarknessAlongBothSupportingWalls(t *testing.T) {
	grid, _ := acuteWebFixture(t, math.Pi/3)
	var split RockGrid
	for _, tri := range geom.Triangulate(grid[0].Polygon) {
		cell := grid[0]
		cell.Polygon = []geom.V{grid[0].Polygon[tri[0]], grid[0].Polygon[tri[1]], grid[0].Polygon[tri[2]]}
		cell.Center = geom.PolygonCenter(cell.Polygon)
		split = append(split, cell)
	}
	corners := findWebCorners(split, ExposedRockEdges(split))
	if len(corners) == 0 || len(resolveSpiderWebs(split, corners)) != 1 {
		t.Fatal("dark acute pocket did not receive a web")
	}
	if len(corners[0].cells) < 2 {
		t.Fatal("fixture did not exercise both supporting walls")
	}
	for _, i := range corners[0].cells {
		bright := append(RockGrid(nil), split...)
		bright[i].Color = color.NRGBA{R: 170, G: 163, B: 145, A: 255}
		if len(resolveSpiderWebs(bright, corners[:1])) != 0 {
			t.Fatal("web attached to an illuminated support")
		}
	}
}

func TestSpiderWebCandidatesSkipPocketsTooSmallForCaptureRings(t *testing.T) {
	grid, p := acuteWebFixture(t, math.Pi/3)
	for i, v := range grid[0].Polygon {
		grid[0].Polygon[i] = p.Add(v.Sub(p).Mul(.12))
	}
	grid[0].Center = geom.PolygonCenter(grid[0].Polygon)
	if len(findWebCorners(grid, ExposedRockEdges(grid))) != 0 {
		t.Fatal("tiny pocket produces rings that collapse into a bright knot")
	}
}

func TestSpiderWebsPreferTheLargestFullyShadedFit(t *testing.T) {
	grid, _ := acuteWebFixture(t, math.Pi/3)
	corners := findWebCorners(grid, ExposedRockEdges(grid))
	if len(corners) < 2 {
		t.Fatal("acute pocket did not retain multiple anchor distances")
	}
	bright := grid[0]
	bright.Color = color.NRGBA{R: 170, G: 163, B: 145, A: 255}
	appearance := append(append(RockGrid(nil), grid...), bright)
	// The longest attachment crosses a lit face, but the next shorter pair
	// still lies within the shaded pocket and must remain eligible.
	corners[0].cells = append(append([]int(nil), corners[0].cells...), 1)
	webs := resolveSpiderWebs(appearance, corners)
	if len(webs) != 1 || webs[0] != corners[1].web {
		t.Fatal("a bright outer support discarded the shorter dark fit")
	}
}

func TestCarvingRebuildsWebSitesWithoutChangingThePublishedTopology(t *testing.T) {
	grid, p := acuteWebFixture(t, math.Pi/3)
	topology := NewRockTopology(grid)
	topology.Appearance = append(RockGrid(nil), topology.Grid...)
	topology.SpiderWebs = resolveSpiderWebs(topology.Appearance, topology.webCorners)
	if len(topology.SpiderWebs) != 1 {
		t.Fatal("fixture has no web to remove")
	}
	original := append([]SpiderWeb(nil), topology.SpiderWebs...)
	geometry := PrepareTerrainGeometry(SectionData{ForegroundTopology: topology}, 0)
	cut, err := CutFromHole(Hole{Shape: HoleCircle, Center: p.Sub(geom.V{Y: 1}), Radius: .04})
	if err != nil {
		t.Fatal(err)
	}
	updated, _, changed := cut.Geometry(0, geometry, 0)
	if !changed || updated.Topology.Appearance == nil {
		t.Fatal("cut did not rebuild the prepared appearance")
	}
	for _, w := range updated.Topology.SpiderWebs {
		if w.Corner.Sub(p).Len() < .04 {
			t.Fatal("web retained a removed rock corner")
		}
	}
	if !reflect.DeepEqual(topology.SpiderWebs, original) {
		t.Fatal("cut changed the previously published web sites")
	}
	full := PrepareRockAppearance(CarvedTopology(updated.Grid, updated.Cuts, updated.Top))
	if !reflect.DeepEqual(updated.Topology.SpiderWebs, full.SpiderWebs) || !reflect.DeepEqual(updated.Topology.Appearance, full.Appearance) {
		t.Fatal("incremental carving differs from a full web/lighting rebuild")
	}
}

func TestSpiderWebsBindAfterFinalLightingAndSurviveSectionOverlap(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		total, shared := 0, 0
		builder := NewSectionBuilder(42, nil, orientation)
		var previous *RockTopology
		for id := int64(0); id < 4; id++ {
			s := builder.Begin(context.Background(), id)
			early := s.Data().ForegroundTopology
			if early.Appearance != nil || early.SpiderWebs != nil {
				t.Fatal("webs were placed before final lighting")
			}
			if !s.Materialize(context.Background()) {
				t.Fatal("materialization failed")
			}
			topology := s.Data().ForegroundTopology
			if early.Appearance != nil || early.SpiderWebs != nil {
				t.Fatal("materialization modified the published shape snapshot")
			}
			if PrepareRockAppearance(topology) != topology {
				t.Fatal("prepared appearance was regenerated")
			}
			for _, w := range topology.SpiderWebs {
				a, b := w.A.Sub(w.Corner), w.B.Sub(w.Corner)
				angle := math.Atan2(geom.Cross(a, b), a.Dot(b))
				if angle < 20*math.Pi/180 || angle > 85*math.Pi/180 {
					t.Fatalf("generated web opening is not acute: %g", angle*180/math.Pi)
				}
				found := false
				for _, c := range topology.webCorners {
					if c.web == w {
						found = true
						for _, i := range c.cells {
							if !darkWebSupport(topology.Appearance[i]) {
								t.Fatal("generated web touches a bright face")
							}
						}
					}
				}
				if !found {
					t.Fatal("web was not precomputed during shaping")
				}
				if w.Corner.Y >= 0 && w.Corner.Y < 1 {
					total++
				}
			}
			if previous != nil {
				for _, w := range previous.SpiderWebs {
					if w.Corner.Y < -.8 || w.Corner.Y > .8 {
						continue
					}
					found := false
					for _, v := range topology.SpiderWebs {
						if w.Corner.Sub(v.Corner.Sub(geom.V{Y: 1})).Len() < 1e-6 {
							if w.A.Sub(v.A.Sub(geom.V{Y: 1})).Len() > 1e-6 || w.B.Sub(v.B.Sub(geom.V{Y: 1})).Len() > 1e-6 || w.Variation != v.Variation {
								t.Fatal("neighboring windows changed the web")
							}
							found = true
							shared++
							break
						}
					}
					if !found {
						t.Fatalf("%s: neighboring window lost web at %+v", orientation, w.Corner)
					}
				}
			}
			previous = topology
		}
		if total == 0 || shared == 0 {
			t.Fatalf("%s: no webs in owned/shared bands: %d/%d", orientation, total, shared)
		}
	}
}
