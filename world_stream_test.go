package infinicave

import (
	"image"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

func TestResizeUsesNativeImagesAndDefersOffscreenSections(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	viewport := Viewport{Y: -.8, Height: .8}
	before := w.sections[1].terrain
	geometry := w.sections[0].geometry
	hit, err := g.Query(Ray{Origin: V{X: .5, Y: -.5}}, QueryOptions{Targets: TargetRock})
	if err != nil || !hit.Found {
		t.Fatalf("initial rock query: %+v, %v", hit, err)
	}
	g.SetRenderWidth(1920)
	finishResolutionRefresh(t, g, viewport)
	if w.sections[0].terrain.Bounds() != image.Rect(0, 0, 3840, 1920) || w.sections[0].foreground.Bounds() != image.Rect(0, 0, 1920, 1920) {
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
	if w.sections[1].renderWidth() != 1920 || w.sections[1].terrain.Bounds().Dx() != 3840 {
		t.Fatal("scrolling did not refresh newly visible terrain")
	}
}

func TestResizeKeepsCutsAndCoalescesPartialUploads(t *testing.T) {
	g := resolutionTestScene(t)
	viewport := Viewport{Y: -.8, Height: .8}
	if _, err := g.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil {
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
	if g.world.sections[0].geometry != geometry || len(g.world.cuts) != 1 || g.world.sections[0].terrain.Bounds().Dx() != 3840 {
		t.Fatal("rapid resize lost cuts or retained an obsolete resolution")
	}
	hit, err := g.Query(Ray{Origin: V{X: .5, Y: -.5}}, QueryOptions{Targets: TargetRock})
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
	neighbor.mesh.VinesBounds = image.Rect(100, 2100, 110, 2300)
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
	if g.world.upload == nil || g.world.upload.data.ID != 1 || g.world.sections[0].renderWidth() != 1000 {
		t.Fatal("moving the camera did not defer the offscreen refresh")
	}
	viewport := Viewport{Y: -1.8, Height: .8}
	for tick := 0; tick < 10 && g.world.upload.stage < 5; tick++ {
		g.Update(viewport)
	}
	if g.world.upload.stage != 5 {
		t.Fatal("terrain was not published before vegetation")
	}
	if _, err := g.CarveCircle(V{X: .5, Y: -1.5}, .1); err != nil {
		t.Fatal(err)
	}
	finishResolutionRefresh(t, g, viewport)
	g.SetRenderWidth(1600)
	finishResolutionRefresh(t, g, viewport)
	// Retained rendering triangles must keep the hole as well as query geometry.
	mesh := g.world.sections[1].mesh.Foreground.Faces
	center := V{X: 500, Y: 1500}
	for i := 0; i < len(mesh.Indices); i += 3 {
		var triangle []V
		for _, index := range mesh.Indices[i : i+3] {
			v := mesh.Vertices[index]
			triangle = append(triangle, V{X: float64(v.DstX), Y: float64(v.DstY)})
		}
		if geom.InsidePolygon(center, triangle) {
			t.Fatal("a cut during upload was lost from the retained rendering mesh")
		}
	}
}

func TestSectionUploadsPublishTerrainBeforeVegetation(t *testing.T) {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan render.SectionMesh, 1), done: make(chan struct{}), working: true}
	// Empty batches isolate the scheduler; nonempty crop bounds exercise image ownership.
	bounds := image.Rect(10, 20, 30, 40)
	w.results <- render.SectionMesh{ID: 0, Background: render.GridMesh{Outlines: make([]render.TriangleMesh, uploadDrawsPerTick*2+1)}, Vines: make([]render.TriangleMesh, uploadDrawsPerTick*2+1), ForegroundVines: make([]render.TriangleMesh, uploadDrawsPerTick*2+1), VinesBounds: bounds, ForegroundVinesBounds: bounds, MushroomsBounds: bounds}
	g, err := NewScene(Config{})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	g.world = w
	defer g.Close()
	w.receive(g)
	if w.upload == nil || w.working {
		t.Fatal("result was not handed off to the upload queue")
	}
	// Neighbor is complete, so readiness below depends only on the pending upload.
	w.sections[1] = &worldSection{}
	published := false
	for tick := 0; tick < 30; tick++ {
		stage, next := w.upload.stage, w.upload.next
		w.receive(g)
		section := w.sections[0]
		if stage < 4 && section != nil {
			t.Fatal("incomplete terrain became visible")
		}
		if stage == 4 {
			if section == nil || section.terrain == nil || section.foreground == nil || !section.vegetationPending {
				t.Fatal("complete terrain was not published early")
			}
			if w.upload.img != nil || w.upload.foreground != nil {
				t.Fatal("upload retained ownership of published images")
			}
			if w.ensure(-.8, .8, 0) {
				t.Fatal("pending vegetation reported ready")
			}
			published = true
			// Camera jumps must not deallocate the section still being completed.
			w.prune(-100.8, .8, 0)
			if w.sections[0] != section {
				t.Fatal("pruning evicted an active upload")
			}
			w.sections[1] = &worldSection{}
		}
		if w.upload == nil {
			if !published || stage != 8 || section == nil || section.vegetationPending {
				t.Fatal("upload did not finish both publication stages")
			}
			if section.vines == nil || section.foregroundVines == nil || section.mushrooms == nil {
				t.Fatal("completed vegetation is missing a layer")
			}
			if section.vines.Bounds() != image.Rect(0, 0, 20, 20) || section.vinesBounds != bounds {
				t.Fatal("vegetation lost its crop or origin")
			}
			if !w.ensure(-.8, .8, 0) {
				t.Fatal("completed viewport is not ready")
			}
			return
		}
		if stage == w.upload.stage && w.upload.next-next > uploadDrawsPerTick {
			t.Fatal("tick submitted too many mesh uploads")
		}
		if (stage == 1 || stage == 5 || stage == 7) && next == 0 && w.upload.stage != stage {
			t.Fatal("large mesh list was uploaded in one tick")
		}
	}
	t.Fatal("section upload did not complete")
}
