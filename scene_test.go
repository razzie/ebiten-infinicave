package infinicave

import (
	"reflect"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestBackgroundConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {BackgroundBlur: .003}, {ShadowOpacity: .5}, {ShadowBlur: .01}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{BackgroundBlur: .003, ShadowOpacity: .5, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		enabled := config.View == ViewShaded && (config.BackgroundBlur > 0 || config.ShadowOpacity > 0)
		if (scene.background != nil) != enabled {
			t.Fatalf("background effects enabled incorrectly for %+v", config)
		}
		before := scene.background
		scene.Reset(42)
		if scene.background != before {
			t.Fatal("world reset lost background rendering settings")
		}
		scene.Close()
		scene.Close()
	}
}

func TestBatsConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {BatsPerMinute: .5}, {BatsPerMinute: 30}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{BatsPerMinute: 30, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		if enabled := config.BatsPerMinute > 0 && config.View == ViewShaded; (scene.bats != nil) != enabled {
			t.Fatalf("bats enabled incorrectly for config %+v", config)
		}
		if scene.bats == nil {
			scene.Reset(42)
			if scene.bats != nil {
				t.Fatal("reset enabled disabled bats")
			}
			continue
		}
		flock := scene.bats
		before := flock.Next
		scene.Update(Viewport{})
		if flock.Next != before {
			t.Fatal("invalid viewport advanced animation")
		}
		scene.Update(Viewport{Y: -.8, Height: .8})
		if flock.Next >= before {
			t.Fatal("valid update did not advance animation while terrain loads")
		}
		flock.Step(Viewport{Y: -.8, Height: .8}, 6)
		flock.Step(Viewport{Y: -.8, Height: .8}, 1)
		image := ebiten.NewImage(100, 80)
		before, bats := flock.Next, append([]render.Bat(nil), flock.Bats...)
		scene.Draw(image, Viewport{Y: -.8, Height: .8})
		scene.SetRenderWidth(200)
		scene.Draw(image, Viewport{Y: -.8, Height: .8})
		image.Deallocate()
		if flock.Next != before || !reflect.DeepEqual(flock.Bats, bats) {
			t.Fatal("repeated draws or resizing advanced animation")
		}
		scene.Reset(42)
		if scene.bats == nil || len(scene.bats.Bats) != 0 {
			t.Fatal("reset did not clear bats while keeping them enabled")
		}
		fresh := render.NewBatFlock(42, config.BatsPerMinute)
		if scene.bats.Next != fresh.Next || scene.bats.PerMinute != config.BatsPerMinute {
			t.Fatal("reset did not restart the animation with the new seed and configured frequency")
		}
		scene.Close()
		scene.Update(Viewport{Y: -.8, Height: .8})
		if scene.bats != nil {
			t.Fatal("closed scene retained bats")
		}
	}
}

func TestFogConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {Fog: .25}, {Fog: .5}, {Fog: 1}, {Fog: 2}, {Fog: 10}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{Fog: 2, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		if enabled := config.Fog > 0 && config.View == ViewShaded; (scene.fog != nil) != enabled {
			t.Fatalf("fog enabled incorrectly for config %+v", config)
		}
		if scene.fog == nil {
			continue
		}
		fog := scene.fog
		if fog.Strength != config.Fog {
			t.Fatalf("fog strength: got %g, want %g", fog.Strength, config.Fog)
		}
		scene.Update(Viewport{})
		if fog.Time != 0 {
			t.Fatal("invalid viewport advanced fog animation")
		}
		scene.Update(Viewport{Y: -.8, Height: .8})
		if fog.Time <= 0 {
			t.Fatal("valid update did not advance fog animation while terrain loads")
		}
		scene.Reset(42)
		if scene.fog != fog || scene.fog.Strength != config.Fog {
			t.Fatal("world reset lost the fog renderer or configured strength")
		}
		scene.Close()
		before := fog.Time
		scene.Update(Viewport{Y: -.8, Height: .8})
		if fog.Time != before {
			t.Fatal("closed scene advanced fog animation")
		}
		scene.Close()
	}
}

func TestGeometryRevisionTracksPublicationGrowthAndEviction(t *testing.T) {
	// Padded copies join across world Y=-4. Section 4 additionally extends
	// the same formation; section 3 remains cached when section 4 is evicted.
	bottom := terrain.SectionData{ID: 3, Foreground: RockGrid{terrainRect(.03, -.02, .03, .04)}}
	top := terrain.SectionData{ID: 4, Foreground: RockGrid{
		terrainRect(.03, .98, .03, .04), terrainRect(.03, .94, .03, .04),
	}}
	g := queryScene(bottom)
	w := g.world
	w.done = make(chan struct{})
	w.terrain = make(chan sectionTerrain, 1)
	t.Cleanup(g.Close)
	hit := rockAt(t, g, V{X: .04, Y: -3.99})
	before, _ := g.Formation(hit.Hit.FormationID)
	revision := g.GeometryRevision()
	geometry := terrain.PrepareTerrainGeometry(top, 0)
	w.terrain <- sectionTerrain{id: top.ID, geometry: geometry}
	called := false
	g.onCollisionReady = func(CollisionGeometry) {
		called = true
		if g.GeometryRevision() <= revision {
			t.Fatal("collision callback observed a stale revision")
		}
		if _, ok := g.CollisionGeometry(-4); !ok {
			t.Fatal("collision callback ran before collision publication")
		}
	}
	index := w.queryIndex()
	queryRevision := index.revision
	w.receiveCollision(g)
	if w.queryIndex().revision != queryRevision {
		t.Fatal("early collision unnecessarily rebuilt the query index")
	}
	if !called || g.GeometryRevision() <= revision {
		t.Fatal("early collision publication did not advance revision")
	}
	still, _ := g.Formation(before.ID)
	if still.Min != before.Min || len(still.SectionIDs) != 1 {
		t.Fatal("early collision unexpectedly published query geometry")
	}
	revision = g.GeometryRevision()
	w.upload = &sectionUpload{stage: 4, data: render.SectionMesh{ID: top.ID, Geometry: geometry}}
	w.receive(g)
	grown, ok := g.Formation(before.ID)
	if g.GeometryRevision() <= revision || !ok || grown.ID != before.ID ||
		grown.Min.Y >= before.Min.Y || len(grown.SectionIDs) != 2 {
		t.Fatalf("growth was not observable under the original ID: %+v", grown)
	}
	revision = g.GeometryRevision()
	w.upload = nil
	w.prune(-.8, .8, 0)
	shrunk, ok := g.Formation(before.ID)
	if g.GeometryRevision() <= revision || !ok || shrunk.ID != before.ID ||
		shrunk.Min != before.Min || len(shrunk.SectionIDs) != 1 {
		t.Fatalf("partial eviction was not observable: %+v", shrunk)
	}
	revision = g.GeometryRevision()
	w.prune(-100.8, .8, 0)
	if g.GeometryRevision() <= revision {
		t.Fatal("full eviction did not advance revision")
	}
	if _, ok := g.Formation(before.ID); ok {
		t.Fatal("fully evicted formation remained fetchable")
	}
}

func TestGeometryRevisionEarlyEvictionAndCarving(t *testing.T) {
	g := queryScene()
	w := g.world
	w.done = make(chan struct{})
	w.terrain = make(chan sectionTerrain, 1)
	t.Cleanup(g.Close)
	w.terrain <- sectionTerrain{id: 4, geometry: terrain.PrepareTerrainGeometry(terrain.SectionData{
		ID: 4, Foreground: RockGrid{terrainRect(.2, .2, .6, .6)},
	}, 0)}
	w.receiveCollision(g)
	revision := g.GeometryRevision()
	result, err := g.CarveCircle(V{X: .5, Y: -4.5}, .1)
	if err != nil || len(result.SectionIDs) != 1 || g.GeometryRevision() <= revision {
		t.Fatalf("early collision carve did not advance revision: %+v, %v", result, err)
	}
	geometry, _ := g.CollisionGeometry(-4)
	if geometry.Contains(V{X: .5, Y: -4.5}) {
		t.Fatal("early collision carve did not publish updated geometry")
	}
	revision = g.GeometryRevision()
	if _, err := g.CarveCircle(V{X: .5, Y: -4.5}, .1); err != nil || g.GeometryRevision() != revision {
		t.Fatal("repeated cut advanced revision")
	}
	w.prune(-.8, .8, 0)
	if g.GeometryRevision() <= revision {
		t.Fatal("early collision eviction did not advance revision")
	}
	if _, ok := g.CollisionGeometry(-4); ok {
		t.Fatal("early collision remained available after eviction")
	}
}

func TestGeometryRevisionLifecycleReadsAndResize(t *testing.T) {
	g := resolutionTestScene(t)
	g.world.prune(-.8, .8, 0) // Eviction is separate from the resize below.
	revision := g.GeometryRevision()
	hit := rockAt(t, g, V{X: .5, Y: -.5})
	g.Formation(hit.Hit.FormationID)
	g.Guide(GuideID{})
	g.CollisionGeometry(0)
	g.Update(Viewport{})
	image := ebiten.NewImage(1, 1)
	defer image.Deallocate()
	g.Draw(image, Viewport{})
	if g.GeometryRevision() != revision {
		t.Fatal("reads or invalid viewport advanced revision")
	}
	g.SetRenderWidth(500)
	finishResolutionRefresh(t, g, Viewport{Y: -.8, Height: .8})
	if g.GeometryRevision() != revision {
		t.Fatal("resize and rerasterization advanced revision")
	}
	if _, err := g.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil || g.GeometryRevision() <= revision {
		t.Fatal("loaded terrain carve did not advance revision")
	}
	revision = g.GeometryRevision()
	if _, err := g.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil || g.GeometryRevision() != revision {
		t.Fatal("repeated loaded cut advanced revision")
	}
	if _, err := g.CarveCircle(V{X: .5, Y: .5}, .1); err != nil || g.GeometryRevision() != revision {
		t.Fatal("out-of-world cut advanced revision")
	}
	g.Reset(7)
	if g.GeometryRevision() <= revision {
		t.Fatal("reset restarted or retained the old revision")
	}
	revision = g.GeometryRevision()
	g.Reset(8)
	if g.GeometryRevision() <= revision {
		t.Fatal("second reset did not advance revision")
	}
	revision = g.GeometryRevision()
	g.Close()
	if g.GeometryRevision() <= revision {
		t.Fatal("close did not advance revision")
	}
	revision = g.GeometryRevision()
	g.Close()
	g.Reset(42)
	if g.GeometryRevision() != revision {
		t.Fatal("operations after close advanced revision")
	}
}

func TestSceneUsesSectionLoaderAfterReset(t *testing.T) {
	scene, err := NewScene(Config{
		Seed: 42, View: ViewClay,
		LoadSection: func(id int64) SectionContent {
			if id != -1 {
				return SectionContent{}
			}
			return SectionContent{Guides: []Guide{{Pts: []V{{X: .2, Y: .4}, {X: .7, Y: .5}}, Seed: 1234}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer scene.Close()
	checkWorker := func() {
		t.Helper()
		scene.world.request(1)
		select {
		case mesh := <-scene.world.results:
			if mesh.ID != 1 || mesh.Geometry == nil || len(mesh.Geometry.Guides) != 1 || mesh.Geometry.Guides[0].Seed != 1234 || len(mesh.Geometry.Collision.Polygons) == 0 {
				t.Fatal("scene worker did not use the configured guide loader")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("custom guide generation stalled")
		}
	}
	checkWorker()
	scene.Reset(7)
	checkWorker()
}
