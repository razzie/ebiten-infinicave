package infinicave

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCollisionReadyDuringUploadIncludesStoredCutsAndOwnsPolygons(t *testing.T) {
	w := &world{
		sections: make(map[int64]*worldSection), terrain: make(chan sectionTerrain, 1),
		jobs: make(chan int64, 1), done: make(chan struct{}), working: true,
		// Empty draw batches keep an unrelated upload busy.
		upload: &sectionUpload{stage: 1, img: render.NewBackgroundImageAt(1000), data: render.SectionMesh{
			Background: render.GridMesh{Outlines: make([]render.TriangleMesh, uploadDrawsPerTick*2)},
		}},
	}
	g, err := NewScene(Config{})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	g.world = w
	defer g.Close()
	cut, _ := terrain.CutFromHole((Hole{Shape: HoleCircle, Center: V{X: .5, Y: -1.5}, Radius: .1}))
	w.cuts = []terrain.RockCut{cut}
	early := terrain.PrepareTerrainGeometry(terrain.SectionData{ID: 1, Foreground: RockGrid{terrainRect(.2, .2, .6, .6)}}, 0)
	w.terrain <- sectionTerrain{id: 1, geometry: early}
	called := 0
	g.onCollisionReady = func(geometry CollisionGeometry) {
		called++
		if w.upload == nil || w.sections[1] != nil || !w.working {
			t.Fatal("collision waited for generation or upload to finish")
		}
		cached, ok := g.CollisionGeometry(-1)
		if !ok || geometry.ID != -1 || geometry.Top != -2 || !reflect.DeepEqual(geometry, cached) {
			t.Fatal("callback and collision accessor disagree")
		}
		if geometry.Contains(V{X: .5, Y: -1.5}) || !geometry.Contains(V{X: .3, Y: -1.3}) {
			t.Fatal("early collision did not apply stored runtime cuts")
		}
		geometry.Polygons[0][0].X = 100
		cached.Polygons[0][0].X = 200
	}
	viewport := Viewport{Y: -.8, Height: .8}
	if g.Update(viewport) || called != 1 {
		t.Fatal("Update did not notify before rendering was ready")
	}
	cached, ok := g.CollisionGeometry(-1)
	if !ok || cached.Polygons[0][0].X > 1 || !early.Collision.Contains(V{X: .5, Y: -1.5}) {
		t.Fatal("game-thread edits or callback modified worker-owned geometry")
	}
	// A runtime edit must update early geometry and report its section for physics.
	w.upload.img.Deallocate()
	w.upload = nil
	result, err := g.CarveCircle(V{X: .3, Y: -1.3}, .03)
	if err != nil || !reflect.DeepEqual(result.SectionIDs, []int64{-1}) {
		t.Fatalf("early carve did not report edited collisions: %+v, %v", result, err)
	}
	cached, _ = g.CollisionGeometry(-1)
	if cached.Contains(V{X: .3, Y: -1.3}) || called != 1 {
		t.Fatal("runtime carve left early collision stale or repeated notification")
	}
	w.prune(-100.8, .8, 0)
	if _, ok := g.CollisionGeometry(-1); ok {
		t.Fatal("distant early collision was not evicted")
	}
}

func TestCollisionReadyCallbackSurvivesResetAndCanCloseScene(t *testing.T) {
	called := 0
	var g *Scene
	var err error
	g, err = NewScene(Config{OnCollisionReady: func(CollisionGeometry) {
		called++
		g.Close()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.Reset(7)
	geometry := terrain.PrepareTerrainGeometry(terrain.SectionData{}, 0)
	g.world.terrain <- sectionTerrain{geometry: geometry}
	if g.Update(Viewport{Y: -.8, Height: .8}) || called != 1 || !g.closed {
		t.Fatal("reset lost callback or Update continued after callback closed scene")
	}
}

func TestSceneCollisionGeometryOwnershipAndAvailability(t *testing.T) {
	h := terrain.PrepareTerrainGeometry(terrain.SectionData{Foreground: RockGrid{terrainRect(0.03, 0.1, 0.03, 0.03)}}, 0)
	scene := &Scene{world: &world{sections: map[int64]*worldSection{0: {geometry: h}}}}
	geometry, ok := scene.CollisionGeometry(0)
	if !ok || len(geometry.Polygons) == 0 {
		t.Fatal("cached collision geometry is unavailable")
	}
	if geometry.Top != -1 || !geometry.Contains(V{X: .04, Y: -.89}) || geometry.Contains(V{X: .4, Y: -.89}) {
		t.Fatal("cached collision geometry does not use scene units")
	}
	before := h.Collision.Polygons[0][0]
	geometry.Polygons[0][0].X += 100
	if h.Collision.Polygons[0][0] != before {
		t.Fatal("caller modified the cached collision geometry")
	}
	copy, ok := scene.CollisionGeometry(0)
	if !ok || !copy.Contains(V{X: .04, Y: -.89}) {
		t.Fatal("repeated collision access changed the cached geometry")
	}
	upper := terrain.PrepareTerrainGeometry(terrain.SectionData{ID: 1, Foreground: RockGrid{terrainRect(0.03, 0.1, 0.03, 0.03)}}, 0)
	scene.world.sections[1] = &worldSection{geometry: upper}
	if geometry, ok := scene.CollisionGeometry(-1); !ok || geometry.ID != -1 || geometry.Top != -2 || !geometry.Contains(V{X: .04, Y: -1.89}) {
		t.Fatal("negative section ID did not resolve the correct cached geometry")
	}
	if _, ok := scene.CollisionGeometry(1); ok {
		t.Fatal("positive section ID accepted")
	}
	if _, ok := scene.CollisionGeometry(-2); ok {
		t.Fatal("missing section reported ready")
	}
	scene.closed = true
	if _, ok := scene.CollisionGeometry(0); ok {
		t.Fatal("closed scene exposed stale geometry")
	}
}
