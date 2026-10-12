package render

import (
	"image"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestSpiderWebMeshUsesPreparedTerrainSites(t *testing.T) {
	grid := terrain.RockGrid{terrainRect(.1, .1, .6, .6)}
	web := terrain.NewSpiderWeb([]geom.V{{X: .3, Y: .3}, {X: .36, Y: .3}, {X: .33, Y: .35}}, .5)
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
			expected := SectionMesh{}
			expected.SpiderWebs = PrepareOwnedSpiderWebs(topology.SpiderWebs)
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
		web := terrain.NewSpiderWeb([]geom.V{{X: .3, Y: y}, {X: .36, Y: y}, {X: .33, Y: y + .05}}, .5)
		mesh := PrepareVegetation(terrain.SectionData{ForegroundTopology: &terrain.RockTopology{SpiderWebs: []terrain.SpiderWeb{web}}}, ViewShaded)
		if mesh.SpiderWebsBounds.Empty() {
			t.Fatal("seam-crossing web was culled")
		}
		for _, bounds := range []image.Rectangle{mesh.SpiderWebsBounds} {
			if !bounds.Empty() && (bounds.Min.Y < RasterPixelsPerUnit || bounds.Max.Y > 2*RasterPixelsPerUnit) {
				t.Fatal("web image overlaps a neighboring section's owned band")
			}
		}
		for _, pixels := range []int{1, 501, 1920} {
			scaled := ScaleVegetationMesh(mesh, pixels)
			for _, bounds := range []image.Rectangle{scaled.SpiderWebsBounds} {
				if !bounds.Empty() && (bounds.Min.Y < pixels || bounds.Max.Y > 2*pixels) {
					t.Fatal("scaled web image overlaps a neighboring band")
				}
			}
		}
	}
}

func TestSpiderWebMeshDrawsCachedNetworkAndThreadRoles(t *testing.T) {
	w := terrain.NewSpiderWeb([]geom.V{{X: .2, Y: .3}, {X: .34, Y: .3}, {X: .23, Y: .4}}, .5)
	if w.Network == nil {
		t.Fatal("fixture has no silk network")
	}
	// A prepared node can be moved independently of the pocket metadata.
	// Rendering must use that exact snapshot without refitting the web.
	w.Network.Nodes[w.Network.Frame[0]].X += .001
	mesh := prepareSpiderWebs([]terrain.SpiderWeb{w}, false)
	checkMesh(t, mesh)
	if len(mesh.Vertices) != 4*len(w.Network.Threads) || len(mesh.Indices) != 6*len(w.Network.Threads) {
		t.Fatal("renderer added or omitted prepared silk")
	}
	var supportWidth, captureWidth, supportAlpha, captureAlpha float32
	for i, silk := range w.Network.Threads {
		vertices := mesh.Vertices[i*4 : (i+1)*4]
		for _, end := range []struct {
			node, first, second int
		}{{silk.A, 0, 3}, {silk.B, 1, 2}} {
			p := w.Network.Nodes[end.node]
			x := (vertices[end.first].DstX + vertices[end.second].DstX) / 2
			y := (vertices[end.first].DstY + vertices[end.second].DstY) / 2
			if absFloat32(x-float32(p.X*RasterPixelsPerUnit)) > .0001 || absFloat32(y-float32((p.Y-terrain.GenerationMinY)*RasterPixelsPerUnit)) > .0002 {
				t.Fatal("rendering changed a settled silk endpoint")
			}
		}
		dx, dy := vertices[0].DstX-vertices[3].DstX, vertices[0].DstY-vertices[3].DstY
		width := dx*dx + dy*dy
		if silk.Kind == terrain.WebSupport {
			supportWidth, supportAlpha = width, vertices[0].ColorA
		} else if silk.Kind == terrain.WebCapture {
			captureWidth, captureAlpha = width, vertices[0].ColorA
		}
	}
	if supportWidth <= captureWidth || supportAlpha <= captureAlpha {
		t.Fatal("attachment silk is not stronger than capture silk")
	}
}

func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
