package render

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestSpiderWebPocketEnvelopeHasBoundedDepthAndMeetsRockAnchors(t *testing.T) {
	for _, b := range []geom.V{{X: 0, Y: .05}, {X: -.03, Y: .025}, {X: .01, Y: .08}, {X: .02, Y: .01}, {X: .07, Y: .03}} {
		w := terrain.SpiderWeb{Corner: geom.V{X: .3, Y: .4}}
		w.A, w.B = w.Corner.Add(geom.V{X: .05}), w.Corner.Add(b)
		a, b := w.A.Sub(w.Corner), w.B.Sub(w.Corner)
		chord := b.Sub(a)
		outward := chord.Perp().Norm().Mul(-1)
		depth := geom.Cross(a, b) / chord.Len()
		for spoke := 0; spoke <= 32; spoke++ {
			for ring := 0; ring <= 5; ring++ {
				p := w.Point(float64(spoke)/32, float64(ring)/5).Sub(w.Corner)
				if geom.Cross(a, p) < -1e-12 || geom.Cross(p, b) < -1e-12 {
					t.Fatal("thread escaped the two supporting walls")
				}
				if beyond := p.Sub(a).Dot(outward); beyond > .012+1e-12 || beyond > .45*depth+1e-12 {
					t.Fatal("web extends too far beyond its rock anchors")
				}
			}
		}
		if w.Point(0, 1).Sub(w.A).Len() > 1e-12 || w.Point(1, 1).Sub(w.B).Len() > 1e-12 {
			t.Fatal("outer thread does not meet both rock anchors")
		}
		mid := w.Point(.5, 1).Sub(w.A).Dot(outward)
		if math.IsNaN(mid) || mid < .20*math.Min(depth, .012) {
			t.Fatal("outer frame collapsed to a flat line between the anchors")
		}
	}
}

func TestSpiderWebHasClosedOrbRingsAndRadialSpokesInOpenSpace(t *testing.T) {
	for _, degrees := range []float64{25, 50, 80} {
		w := terrain.SpiderWeb{Corner: geom.V{X: .3, Y: .4}, Variation: .5}
		w.A = w.Corner.Add(geom.V{X: .075})
		w.B = w.Corner.Add(geom.Rotate(geom.V{X: .065}, degrees*math.Pi/180))
		hub, radius := w.Orb()
		frame := w.Frame()
		if radius <= 0 || !geom.InsidePolygon(hub, frame) || hub.Sub(w.Corner).Len() < radius {
			t.Fatal("hub must lie in the opening, away from the rock corner")
		}
		spokes, rings := 0, 0
		for _, silk := range spiderWebThreads(w) {
			if len(silk.points) == 2 {
				a, b := silk.points[0].Sub(hub), silk.points[1].Sub(hub)
				if math.Abs(geom.Cross(a, b)) > 1e-12 || a.Len() <= 0 || a.Len() >= radius {
					t.Fatal("spoke is not radial or leaves no open hub")
				}
				if b.Len() < radius {
					t.Fatal("spoke ends inside the capture rings")
				}
				spokes++
				continue
			}
			if silk.points[0] != silk.points[len(silk.points)-1] {
				t.Fatal("web contains an unclosed frame or capture ring")
			}
			minR, maxR, winding := math.Inf(1), 0.0, 0.0
			for i, p := range silk.points {
				d := p.Sub(hub)
				minR, maxR = math.Min(minR, d.Len()), math.Max(maxR, d.Len())
				if i > 0 {
					prev := silk.points[i-1].Sub(hub)
					winding += math.Atan2(geom.Cross(prev, d), prev.Dot(d))
				}
			}
			if maxR > radius*1.01 {
				continue // The anchored outer frame surrounds the orb.
			}
			if maxR/minR > 1.15 || math.Abs(winding-2*math.Pi) > 1e-9 {
				t.Fatal("capture thread must form a round loop all the way around the hub")
			}
			rings++
		}
		if spokes < 8 || rings < 3 {
			t.Fatalf("missing recognizable orb pattern: %d spokes, %d rings", spokes, rings)
		}
	}
}

func TestSpiderWebMeshUsesPreparedTerrainSites(t *testing.T) {
	grid := terrain.RockGrid{terrainRect(.1, .1, .6, .6)}
	web := terrain.SpiderWeb{Corner: geom.V{X: .3, Y: .3}, A: geom.V{X: .36, Y: .3}, B: geom.V{X: .33, Y: .35}, Variation: .5}
	// Supplying a prepared appearance prevents the renderer from selecting
	// different sites or redoing the lighting when a cached layer is resized.
	topology := terrain.NewRockTopology(grid)
	topology.Appearance = append(terrain.RockGrid(nil), topology.Grid...)
	topology.SpiderWebs = []terrain.SpiderWeb{web}
	want := prepareSpiderWebs(topology.SpiderWebs, false)
	checkMesh(t, want)
	if len(want.Indices) == 0 || !want.premultiplied {
		t.Fatal("missing translucent silk mesh")
	}
	for _, view := range []View{ViewShaded, ViewClay, ViewHeight, ViewNormals, ViewShadows} {
		rock := PrepareForeground(terrain.SectionData{ForegroundTopology: topology}, view)
		bare := *topology
		bare.SpiderWebs = nil
		if !reflect.DeepEqual(rock.Foreground, PrepareForeground(terrain.SectionData{ForegroundTopology: &bare}, view).Foreground) {
			t.Fatal("rock mesh includes web decoration")
		}
		decoration := PrepareVegetation(terrain.SectionData{ForegroundTopology: topology}, view)
		if view == ViewShaded {
			expected := SectionMesh{SpiderWebs: prepareSpiderWebs(topology.SpiderWebs, true)}
			FinishSectionMesh(&expected)
			if !reflect.DeepEqual(decoration.SpiderWebs, expected.SpiderWebs) || decoration.SpiderWebsBounds != expected.SpiderWebsBounds {
				t.Fatal("decoration renderer did not use the precomputed web sites")
			}
		} else if len(decoration.SpiderWebs.Indices) != 0 {
			t.Fatalf("web appeared in %s diagnostic view", view)
		}
	}
	if !reflect.DeepEqual(want, prepareSpiderWebs(topology.SpiderWebs, false)) {
		t.Fatal("prepared web changed on redraw")
	}
}

func TestSpiderWebImagesClipToOwnedBandAcrossSeams(t *testing.T) {
	for _, y := range []float64{-.02, .98} {
		web := terrain.SpiderWeb{Corner: geom.V{X: .3, Y: y}, A: geom.V{X: .36, Y: y}, B: geom.V{X: .33, Y: y + .05}}
		mesh := PrepareVegetation(terrain.SectionData{ForegroundTopology: &terrain.RockTopology{SpiderWebs: []terrain.SpiderWeb{web}}}, ViewShaded)
		if mesh.SpiderWebsBounds.Empty() {
			t.Fatal("seam-crossing web was culled")
		}
		if mesh.SpiderWebsBounds.Min.Y < RasterPixelsPerUnit || mesh.SpiderWebsBounds.Max.Y > 2*RasterPixelsPerUnit {
			t.Fatal("web image overlaps a neighboring section's owned band")
		}
		for _, pixels := range []int{1, 501, 1920} {
			scaled := ScaleVegetationMesh(mesh, pixels)
			if scaled.SpiderWebsBounds.Min.Y < pixels || scaled.SpiderWebsBounds.Max.Y > 2*pixels {
				t.Fatal("scaled web image overlaps a neighboring band")
			}
		}
	}
}
