package infinicave

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func resolutionTestMesh(id int64) sectionMesh {
	data := sectionData{id: id,
		background: RockGrid{{Center: V{.5, .5}, Polygon: []V{{0, -1}, {1, -1}, {1, 2}, {0, 2}}, Color: color.NRGBA{R: 40, A: 255}, Normal: V3{Z: 1}}},
		foreground: RockGrid{{Center: V{.5, .5}, Polygon: []V{{.2, .2}, {.8, .2}, {.8, .8}, {.2, .8}}, Color: color.NRGBA{R: 100, A: 255}, Normal: V3{Z: 1}, Raised: true}},
	}
	mesh := prepareSection(data, ViewClay)
	mesh.geometry = prepareTerrainGeometry(data, 0)
	finishSectionMesh(&mesh)
	return mesh
}

func resolutionTestScene(t *testing.T) *Scene {
	t.Helper()
	g, err := NewScene(Config{View: ViewClay})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	// No worker: any accidental regeneration is observable in the jobs queue.
	g.world = &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan sectionMesh, 1), done: make(chan struct{})}
	for id := int64(0); id < 5; id++ {
		mesh := resolutionTestMesh(id)
		g.world.sections[id] = &worldSection{mesh: &mesh, geometry: mesh.geometry,
			terrain: newSectionImageAt(1, 1000), foreground: newSectionImageAt(1, 1000), pixels: 1000}
	}
	t.Cleanup(g.Close)
	return g
}

func finishResolutionRefresh(t *testing.T, g *Scene, viewport Viewport) {
	t.Helper()
	for tick := 0; tick < 40; tick++ {
		if g.Update(viewport) {
			return
		}
	}
	t.Fatal("visible sections never finished refreshing")
}

func TestResizeUsesNativeImagesAndDefersOffscreenSections(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	viewport := Viewport{Y: -.8, Height: .8}
	before := w.sections[1].terrain
	geometry := w.sections[0].geometry
	hit, err := g.Query(Ray{Origin: V{.5, -.5}}, QueryOptions{Targets: TargetRock})
	if err != nil || !hit.Found {
		t.Fatalf("initial rock query: %+v, %v", hit, err)
	}
	g.SetRenderWidth(1920)
	finishResolutionRefresh(t, g, viewport)
	if w.sections[0].terrain.Bounds() != image.Rect(0, 0, 1920, 1920) || w.sections[0].foreground.Bounds() != image.Rect(0, 0, 1920, 1920) {
		t.Fatal("visible terrain did not use native raster dimensions")
	}
	if w.sections[1].terrain != before || w.sections[1].renderWidth() != 1000 {
		t.Fatal("offscreen terrain was rerendered during resize")
	}
	if w.sections[0].geometry != geometry || len(w.jobs) != 0 {
		t.Fatal("resize regenerated CPU terrain")
	}
	if _, available := g.Formation(hit.Hit.FormationID); !available {
		t.Fatal("resize invalidated formation IDs")
	}
	finishResolutionRefresh(t, g, Viewport{Y: -1.8, Height: .8})
	if w.sections[1].renderWidth() != 1920 || w.sections[1].terrain.Bounds().Dx() != 1920 {
		t.Fatal("scrolling did not refresh newly visible terrain")
	}
}

func TestResizeKeepsCutsAndCoalescesPartialUploads(t *testing.T) {
	g := resolutionTestScene(t)
	viewport := Viewport{Y: -.8, Height: .8}
	if _, err := g.CarveCircle(V{.5, -.5}, .1); err != nil {
		t.Fatal(err)
	}
	geometry := g.world.sections[0].geometry
	g.SetRenderWidth(1600)
	g.Update(viewport)
	g.Update(viewport) // Allocate the first native terrain image.
	if g.world.upload == nil || g.world.upload.img == nil {
		t.Fatal("resize did not begin a refresh")
	}
	g.SetRenderWidth(1920)
	finishResolutionRefresh(t, g, viewport)
	if g.world.sections[0].geometry != geometry || len(g.world.cuts) != 1 || g.world.sections[0].terrain.Bounds().Dx() != 1920 {
		t.Fatal("rapid resize lost cuts or retained an obsolete resolution")
	}
	hit, err := g.Query(Ray{Origin: V{.5, -.5}}, QueryOptions{Targets: TargetRock})
	if err != nil || hit.Found || !hit.Complete {
		t.Fatalf("resized scene refilled the carved hole: %+v, %v", hit, err)
	}
	g.Reset(42)
	if g.world.renderWidth() != 1920 {
		t.Fatal("seed reset lost the native render resolution")
	}
}

func TestResizeRefreshesNeighborOnlyForVisibleVegetation(t *testing.T) {
	g := resolutionTestScene(t)
	neighbor := g.world.sections[1]
	// Section 1's padded window begins at -3; this vine reaches into [-.9, -.7].
	neighbor.mesh.vinesBounds = image.Rect(100, 2100, 110, 2300)
	g.SetRenderWidth(1500)
	finishResolutionRefresh(t, g, Viewport{Y: -.8, Height: .8})
	if g.world.sections[1].renderWidth() != 1500 || g.world.sections[2].renderWidth() != 1000 {
		t.Fatal("resize did not restrict neighbor refreshes to contributing vegetation")
	}
}

func TestResizeRefreshFollowsCameraAndRetainsCutsDuringUpload(t *testing.T) {
	g := resolutionTestScene(t)
	g.SetRenderWidth(1920)
	g.Update(Viewport{Y: -.8, Height: .8})
	g.Update(Viewport{Y: -.8, Height: .8})
	g.Update(Viewport{Y: -1.8, Height: .8})
	if g.world.upload == nil || g.world.upload.data.id != 1 || g.world.sections[0].renderWidth() != 1000 {
		t.Fatal("moving the camera did not defer the offscreen refresh")
	}
	viewport := Viewport{Y: -1.8, Height: .8}
	for tick := 0; tick < 10 && g.world.upload.stage < 5; tick++ {
		g.Update(viewport)
	}
	if g.world.upload.stage != 5 {
		t.Fatal("terrain was not published before vegetation")
	}
	if _, err := g.CarveCircle(V{.5, -1.5}, .1); err != nil {
		t.Fatal(err)
	}
	finishResolutionRefresh(t, g, viewport)
	g.SetRenderWidth(1600)
	finishResolutionRefresh(t, g, viewport)
	// Retained rendering triangles must keep the hole as well as query geometry.
	mesh := g.world.sections[1].mesh.foreground.faces
	center := V{500, 1500}
	for i := 0; i < len(mesh.indices); i += 3 {
		var triangle []V
		for _, index := range mesh.indices[i : i+3] {
			v := mesh.vertices[index]
			triangle = append(triangle, V{float64(v.DstX), float64(v.DstY)})
		}
		if insideFace(center, triangle) {
			t.Fatal("a cut during upload was lost from the retained rendering mesh")
		}
	}
}

func TestNativeMeshScalingRetainsMaterialCoordinatesAndCropOrigin(t *testing.T) {
	base := resolutionTestMesh(0)
	base.vines = []triangleMesh{{vertices: []ebiten.Vertex{{DstX: 2.25, DstY: 3.5, SrcX: .5, SrcY: .5}}, indices: []uint32{0, 0, 0}}}
	base.vinesBounds = image.Rect(101, 1003, 110, 1020)
	saved := append([]ebiten.Vertex(nil), base.background.faces.vertices...)
	for _, pixels := range []int{500, 1000, 1920} {
		scaled := scaleSectionMesh(base, pixels)
		for i, vertex := range base.background.faces.vertices {
			native := scaled.background.faces.vertices[i]
			if math.Abs(float64(native.DstY)-(float64(vertex.DstY)*float64(pixels)/1000-float64(pixels))) > .001 || native.SrcX != vertex.SrcX || native.SrcY != vertex.SrcY {
				t.Fatal("terrain scaling lost owned-band alignment or material coordinates")
			}
		}
		p := scaled.vines[0].vertices[0]
		wantX := (2.25 + 101) * float64(pixels) / 1000
		wantY := (3.5 + 1003) * float64(pixels) / 1000
		if math.Abs(float64(p.DstX)+float64(scaled.vinesBounds.Min.X)-wantX) > .001 || math.Abs(float64(p.DstY)+float64(scaled.vinesBounds.Min.Y)-wantY) > .001 {
			t.Fatal("native vegetation crop moved its world origin")
		}
	}
	if !reflect.DeepEqual(base.background.faces.vertices, saved) || base.vines[0].vertices[0].DstX != 2.25 {
		t.Fatal("native scaling mutated cached reference meshes")
	}
}
