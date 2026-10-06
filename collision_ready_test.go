package infinicave

import (
	"reflect"
	"testing"
)

func TestCollisionReadyBeforeDecorationIncludesAuthoredHoles(t *testing.T) {
	load := func(id int64) SectionContent {
		if id != 0 {
			return SectionContent{}
		}
		return SectionContent{
			Guides: []Guide{{Pts: []V{{.15, .4}, {.5, .35}, {.85, .45}}, Seed: 1234}},
			Holes: []Hole{
				{Shape: HoleCircle, Center: V{.5, .4}, Radius: .08},
				{Shape: HoleSegment, Start: V{.3, .3}, End: V{.6, .6}, Width: .04},
			},
		}
	}
	var early *terrainGeometry
	data := newSectionBuilder(42, load).buildWithTerrain(0, func(data sectionData) {
		if len(data.vines)+len(data.foregroundVines)+len(data.mushrooms) != 0 {
			t.Fatal("collision waited for decoration")
		}
		early = prepareTerrainGeometry(data, .002)
		if len(early.collision.Polygons) == 0 || early.collision.Contains(V{.5, -.6}) {
			t.Fatal("early collision omitted terrain or authored holes")
		}
	})
	final := prepareTerrainGeometry(data, .002)
	if early == nil || !reflect.DeepEqual(early.collision, final.collision) || !reflect.DeepEqual(early.grid, final.grid) {
		t.Fatal("decoration changed published terrain")
	}
	if len(data.vines)+len(data.foregroundVines)+len(data.mushrooms) == 0 {
		t.Fatal("prioritizing collision lost decoration")
	}
}

func TestCollisionReadyDuringUploadIncludesStoredCutsAndOwnsPolygons(t *testing.T) {
	w := &world{
		sections: make(map[int64]*worldSection), terrain: make(chan sectionTerrain, 1),
		jobs: make(chan int64, 1), done: make(chan struct{}), working: true,
		// Empty draw batches keep an unrelated upload busy without shaders.
		upload: &sectionUpload{stage: 1, data: sectionMesh{
			background: gridMesh{outlines: make([]triangleMesh, uploadDrawsPerTick*2)},
		}},
	}
	g := &Scene{world: w}
	defer g.Close()
	cut, _ := (Hole{Shape: HoleCircle, Center: V{.5, -1.5}, Radius: .1}).rockCut()
	w.cuts = []rockCut{cut}
	early := prepareTerrainGeometry(sectionData{id: 1, foreground: RockGrid{hoverRect(.2, .2, .6, .6)}}, 0)
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
		if geometry.Contains(V{.5, -1.5}) || !geometry.Contains(V{.3, -1.3}) {
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
	if !ok || cached.Polygons[0][0].X > 1 || !early.collision.Contains(V{.5, -1.5}) {
		t.Fatal("game-thread edits or callback modified worker-owned geometry")
	}
	// A runtime edit must update early geometry and report its section for physics.
	w.upload = nil
	result, err := g.CarveCircle(V{.3, -1.3}, .03)
	if err != nil || !reflect.DeepEqual(result.SectionIDs, []int64{-1}) {
		t.Fatalf("early carve did not report edited collisions: %+v, %v", result, err)
	}
	cached, _ = g.CollisionGeometry(-1)
	if cached.Contains(V{.3, -1.3}) || called != 1 {
		t.Fatal("runtime carve left early collision stale or repeated notification")
	}
	w.prune(-100.8, .8, 0)
	if _, ok := g.CollisionGeometry(-1); ok {
		t.Fatal("distant early collision was not evicted")
	}
}

func TestSynchronousCollisionReadyOwnershipAndEmptySections(t *testing.T) {
	for _, empty := range []bool{false, true} {
		called := 0
		var notified CollisionGeometry
		config := Config{Seed: 42, LoadSection: testCurlSection, CollisionTolerance: .002}
		if empty {
			config.LoadSection = func(int64) SectionContent { return SectionContent{} }
		}
		config.OnCollisionReady = func(geometry CollisionGeometry) {
			called++
			notified = copyCollisionGeometry(geometry)
			if len(geometry.Polygons) > 0 {
				geometry.Polygons[0][0].X = 100
			}
		}
		section, err := GenerateSectionWithConfig(config, -1)
		if err != nil || called != 1 || !reflect.DeepEqual(notified, section.Collision) {
			t.Fatalf("synchronous notification changed collision: empty=%v, calls=%d, err=%v", empty, called, err)
		}
		if empty != (len(notified.Polygons) == 0) {
			t.Fatal("empty sections did not notify with empty collision")
		}
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
	geometry := prepareTerrainGeometry(sectionData{}, 0)
	g.world.terrain <- sectionTerrain{geometry: geometry}
	if g.Update(Viewport{Y: -.8, Height: .8}) || called != 1 || !g.closed {
		t.Fatal("reset lost callback or Update continued after callback closed scene")
	}
}
