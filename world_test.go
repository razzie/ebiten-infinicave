package infinicave

import (
	"reflect"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestWorldWorkerPreparesAllLayers(t *testing.T) {
	w := newWorld(42, ViewShaded, 0, nil)
	defer w.close()
	w.request(0)
	var early *terrain.Geometry
	select {
	case terrain := <-w.terrain:
		early = terrain.geometry
		if terrain.id != 0 || early == nil || len(early.Collision.Polygons) == 0 {
			t.Fatal("worker omitted early collision geometry")
		}
		if len(early.Vegetation.Vines)+len(early.Vegetation.ForegroundVines)+len(early.Vegetation.Mushrooms) != 0 {
			t.Fatal("worker published collision after adding vegetation")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("background collision preparation stalled")
	}
	select {
	case mesh := <-w.results:
		if mesh.Geometry == nil || len(mesh.Geometry.Collision.Polygons) == 0 {
			t.Fatal("worker omitted collision geometry")
		}
		if early == mesh.Geometry || !reflect.DeepEqual(early.Collision, mesh.Geometry.Collision) || len(early.Vegetation.Vines) != 0 {
			t.Fatal("worker mutated or replaced early collision boundaries")
		}
		if mesh.ID != 0 || len(mesh.Background.Faces.Indices) == 0 || len(mesh.Foreground.Faces.Indices) == 0 || len(mesh.Vines) == 0 || len(mesh.ForegroundVines) == 0 || len(mesh.Mushrooms.Indices) == 0 {
			t.Fatal("worker returned incomplete section geometry")
		}
		checkMesh(t, mesh.Background.Faces)
		checkMesh(t, mesh.Foreground.Faces)
		checkMesh(t, mesh.Mushrooms)
		for _, layer := range [][]render.TriangleMesh{mesh.Background.Outlines, mesh.Foreground.Outlines, mesh.Vines, mesh.ForegroundVines} {
			for _, m := range layer {
				checkMesh(t, m)
			}
		}
		if len(w.sections) != 0 || w.upload != nil || w.white != nil {
			t.Fatal("worker touched game-thread scene or GPU state")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("background section preparation stalled")
	}
}

func TestDiagnosticWorkerRetainsTerrainGeometry(t *testing.T) {
	w := newWorld(42, ViewClay, 0, testLedgeSection)
	defer w.close()
	w.request(0)
	select {
	case mesh := <-w.results:
		if mesh.Geometry == nil || len(mesh.Geometry.Blocks) == 0 || len(mesh.Geometry.Guides) == 0 || len(mesh.Geometry.Collision.Polygons) == 0 {
			t.Fatal("diagnostic view lost terrain or collision geometry")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("terrain geometry preparation stalled")
	}
}

func TestVisibleSectionsAtSeams(t *testing.T) {
	for _, tc := range []struct {
		y         float64
		height    float64
		low, high int64
	}{
		{-.8, .8, 0, 0}, {-1, 1, 0, 0}, {-1.001, .8, 0, 1}, {-2, 1, 1, 1}, {-10.8, .8, 10, 10}, {-1e6, .8, 999999, 999999},
		{-1.0001, .0002, 0, 1}, {-1.0001, .0001, 1, 1},
	} {
		viewport := Viewport{Y: tc.y, Height: tc.height}
		low, high := visibleSections(viewport.Y, viewport.Height)
		if low != tc.low || high != tc.high {
			t.Fatalf("viewport %v/%v: sections %d..%d, want %d..%d", tc.y, tc.height, low, high, tc.low, tc.high)
		}
	}
}

func TestStreamingRequestsAndCacheStayBounded(t *testing.T) {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1)}
	if w.ensure(-.800, .800, 0) {
		t.Fatal("unloaded viewport reported ready")
	}
	if id := <-w.jobs; id != 0 {
		t.Fatalf("first request is %d, want floor section", id)
	}
	// Repeated frames must not enqueue duplicate work while the worker is busy.
	w.ensure(-.800, .800, 0)
	if len(w.jobs) != 0 {
		t.Fatal("duplicate generation request")
	}
	w.working = false
	for id := int64(0); id < 100; id++ {
		w.sections[id] = &worldSection{terrain: ebiten.NewImage(1, 1), vines: ebiten.NewImage(1, 1)}
	}
	w.prune(-10.800, .800, 0)
	if len(w.sections) > 6 {
		t.Fatalf("cache grew with distance: %d sections", len(w.sections))
	}
	if w.sections[0] != nil || w.sections[10] == nil {
		t.Fatal("cache discarded the viewport or retained distant sections")
	}
	if !w.ensure(-10.800, .800, 0) {
		t.Fatal("loaded far-up viewport cannot be reached")
	}
	w.prune(-.800, .800, 0)
	if w.ensure(-.800, .800, 0) {
		t.Fatal("evicted starting area was not requested again")
	}
	for _, s := range w.sections {
		s.terrain.Deallocate()
		s.vines.Deallocate()
	}
}
