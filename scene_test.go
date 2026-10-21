package infinicave

import (
	"context"
	"math"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestStreamingConfigurationSurvivesResetAndResize(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, counts := range [][2]int{{0, 0}, {2, 1}, {8, 12}} {
			scene, err := NewScene(Config{Orientation: orientation, RetainSections: counts[0], LookaheadSections: counts[1]})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(scene.Close)
			for _, seed := range []int64{0, 42} {
				if seed != 0 {
					scene.Reset(seed)
				}
				scene.SetRenderWidth(1200)
				if scene.world.retainSections != counts[0] || scene.world.lookaheadSections != counts[1] {
					t.Fatalf("%v reset/resize lost streaming configuration %v", orientation, counts)
				}
			}
		}
	}
}

func TestBackgroundConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {BackgroundBlur: .003}, {ShadowOpacity: .5}, {ShadowBlur: .01}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{BackgroundBlur: .003, ShadowOpacity: .5, View: view})
	}
	for _, config := range append([]Config(nil), configs...) {
		config.DisableBackground = true
		configs = append(configs, config)
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		enabled := !config.DisableBackground && config.View == ViewShaded && (config.BackgroundBlur > 0 || config.ShadowOpacity > 0)
		if (scene.background != nil) != enabled {
			t.Fatalf("background effects enabled incorrectly for %+v", config)
		}
		before := scene.background
		scene.Reset(42)
		if scene.world.disableBackground != config.DisableBackground || (scene.backgroundFade == nil) != config.DisableBackground {
			t.Fatal("world reset lost background enablement")
		}
		if (scene.fog != nil) != (config.Fog > 0 && config.View == ViewShaded) {
			t.Fatal("background enablement changed fog")
		}
		if scene.background != before {
			t.Fatal("world reset lost background rendering settings")
		}
		scene.Close()
		scene.Close()
	}
}

func TestAppearanceFadeConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {AppearanceFade: true}, {AppearanceBurn: true}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{AppearanceFade: true, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		enabled := (config.AppearanceFade || config.AppearanceBurn) && config.View == ViewShaded
		if (scene.appearanceFade != nil) != enabled || scene.world.appearanceFade != scene.appearanceFade {
			t.Fatalf("appearance fade enabled incorrectly for %+v", config)
		}
		fade := scene.appearanceFade
		scene.SetRenderWidth(384)
		scene.Reset(42)
		if scene.appearanceFade != fade || scene.world.appearanceFade != fade {
			t.Fatal("reset lost appearance fade configuration")
		}
		scene.Close()
		scene.Close()
		if scene.appearanceFade != nil {
			t.Fatal("close retained appearance renderer")
		}
	}
}

func TestDecorationBurnConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {DecorationBurn: true}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{DecorationBurn: true, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		enabled := config.DecorationBurn && config.View == ViewShaded
		if (scene.decorationBurn != nil) != enabled {
			t.Fatalf("burn enabled incorrectly for %+v", config)
		}
		burn := scene.decorationBurn
		scene.SetRenderWidth(384)
		scene.Reset(42)
		if scene.decorationBurn != burn {
			t.Fatal("reset lost burn configuration")
		}
		scene.Close()
		scene.Close()
		if scene.decorationBurn != nil {
			t.Fatal("close retained burn renderer")
		}
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

func TestFogVelocityLifecycle(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		scene, err := NewScene(Config{Fog: 1, Orientation: orientation})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		wind := V{X: .12, Y: -.03}
		scene.SetFogVelocity(wind)
		fog := scene.fog
		if fog.Velocity != wind {
			t.Fatalf("%v: fog velocity = %v, want %v", orientation, fog.Velocity, wind)
		}
		for _, invalid := range []V{{X: math.NaN()}, {Y: math.NaN()}, {X: math.Inf(1)}, {Y: math.Inf(-1)}} {
			scene.SetFogVelocity(invalid)
			if fog.Velocity != wind {
				t.Fatal("invalid velocity changed fog motion")
			}
		}
		scene.SetRenderWidth(128)
		scene.Reset(42)
		if scene.fog != fog || fog.Velocity != wind {
			t.Fatal("reset or resizing lost fog velocity")
		}
		scene.SetFogVelocity(V{})
		if fog.Velocity != (V{}) {
			t.Fatal("zero velocity did not stop fog")
		}
		scene.Close()
		scene.SetFogVelocity(wind)
		if fog.Velocity != (V{}) {
			t.Fatal("closed scene accepted fog velocity")
		}
	}
	for _, config := range []Config{{}, {Fog: 1, View: ViewClay}} {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		scene.SetFogVelocity(V{X: .1})
		if scene.fog != nil {
			t.Fatal("setting velocity enabled disabled fog")
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
	w.retainSections = 3
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
	w.upload = &sectionUpload{stage: uploadForegroundPublish, initialized: true, needed: render.ForegroundLayer, data: render.SectionMesh{ID: top.ID, Geometry: geometry}}
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

func TestReadinessSeparatesForegroundFromDetails(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		scene, err := NewScene(Config{Orientation: orientation})
		if err != nil {
			t.Fatal(err)
		}
		viewport := Viewport{Y: -1, Height: 1}
		if orientation == Horizontal {
			viewport = Viewport{X: 0, Width: 1}
		}
		if got := scene.Readiness(viewport); got != (SceneReadiness{}) {
			t.Fatalf("empty scene ready: %+v", got)
		}
		scene.world.collision = map[int64]*terrain.Geometry{0: {}}
		if got := scene.Readiness(viewport); !got.Collision || got.Foreground {
			t.Fatalf("early collision readiness: %+v", got)
		}
		scene.world.sections[0] = &worldSection{geometry: &terrain.Geometry{}, foreground: ebiten.NewImage(8, 8), pixels: 8, vegetationPending: true}
		scene.world.pixels = 8
		got := scene.Readiness(viewport)
		if !got.Collision || !got.Foreground || got.Background || got.Decorations {
			t.Fatalf("rock-base readiness: %+v", got)
		}
		if len(scene.world.jobs) != 0 {
			t.Fatal("readiness requested generation")
		}
		scene.world.sections[0].terrain = ebiten.NewImage(8, 8)
		scene.world.sections[0].vegetationPending = false
		scene.world.sections[1] = &worldSection{mesh: &render.SectionMesh{ID: 1, Layers: render.AllLayers}}
		if got := scene.Readiness(viewport); !got.Background || !got.Decorations {
			t.Fatalf("finished layers: %+v", got)
		}
		scene.SetRenderWidth(9)
		if got := scene.Readiness(viewport); !got.Collision || got.Foreground || got.Background {
			t.Fatalf("resized readiness: %+v", got)
		}
		scene.Close()
		if got := scene.Readiness(viewport); got != (SceneReadiness{}) {
			t.Fatal("closed scene ready")
		}
	}
}

func TestRenderPassCoordinatesAndLifecycle(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		scene, err := NewScene(Config{Orientation: orientation})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		for _, size := range [][2]int{{100, 180}, {180, 100}, {501, 800}} {
			dst := ebiten.NewImage(size[0], size[1])
			pixels := float64(min(size[0], size[1]))
			viewport := Viewport{Y: -2.003, Height: float64(size[1]) / pixels}
			if orientation == Horizontal {
				viewport = Viewport{X: 2.003, Width: float64(size[0]) / pixels}
			}
			var calls []string
			check := func(name string) RenderPass {
				return func(c RenderContext) {
					calls = append(calls, name)
					if c.Orientation != orientation {
						t.Fatal("pass lost orientation")
					}
					var point V
					var expectedX, expectedY float64
					if orientation == Horizontal {
						point = V{X: c.Viewport.X + .4, Y: .3}
						expectedX, expectedY = .4*pixels, float64(size[1]-int(pixels))/2+.3*pixels
					} else {
						point = V{X: .3, Y: c.Viewport.Y + .4}
						expectedX, expectedY = float64(size[0]-int(pixels))/2+.3*pixels, .4*pixels
					}
					x, y := c.WorldToTarget.Apply(point.X, point.Y)
					if orientation == Horizontal {
						x, y = float64(size[0])-y, x
					}
					if math.Abs(x-expectedX) > 1e-8 || math.Abs(y-expectedY) > 1e-8 {
						t.Fatalf("%s %v: transformed point %g,%g, want %g,%g", orientation, size, x, y, expectedX, expectedY)
					}
					if point.X < c.Min.X || point.X > c.Max.X || point.Y < c.Min.Y || point.Y > c.Max.Y {
						t.Fatal("visible world point excluded from context bounds")
					}
					if orientation == Vertical && c.Dst != dst {
						t.Fatal("vertical pass lost target")
					}
					if orientation == Horizontal && c.Dst == dst {
						t.Fatal("horizontal pass bypassed compositor")
					}
				}
			}
			passes := RenderPasses{BehindRocks: check("back"), AboveRocks: check("front")}
			scene.DrawWithPasses(dst, viewport, passes)
			scene.DrawWithPasses(dst, viewport, passes)
			if !reflect.DeepEqual(calls, []string{"back", "front", "back", "front"}) {
				t.Fatalf("passes must run once in order even while unloaded: %v", calls)
			}
			scene.DrawWithPasses(dst, Viewport{}, passes)
			if len(calls) != 4 {
				t.Fatal("invalid viewport invoked passes")
			}
			dst.Deallocate()
		}
		scene.Close()
		dst := ebiten.NewImage(10, 10)
		scene.DrawWithPasses(dst, Viewport{Y: -1, Height: 1, X: 0, Width: 1}, RenderPasses{
			BehindRocks: func(RenderContext) { t.Fatal("closed scene invoked pass") },
			AboveRocks:  func(RenderContext) { t.Fatal("closed scene invoked pass") },
		})
		dst.Deallocate()
	}
}

// Run in a fresh process so atlas construction and shader compilation are cold.
// Timing is diagnostic: machine speed must not make this test flaky.
func TestColdSceneStartup(t *testing.T) {
	if os.Getenv("INFINICAVE_STARTUP") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestColdSceneStartup$", "-test.v")
		cmd.Env = append(os.Environ(), "INFINICAVE_STARTUP=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	g := &startupGame{t: t, start: time.Now()}
	config := DefaultConfig()
	config.Seed = 42
	config.OnCollisionReady = func(CollisionGeometry) { g.t.Logf("collision: %s", time.Since(g.start)) }
	var err error
	g.scene, err = NewScene(config)
	if err != nil {
		t.Fatal(err)
	}
	defer g.scene.Close()
	ebiten.SetWindowSize(640, 640)
	ebiten.SetRunnableOnUnfocused(true)
	g.scene.SetRenderWidth(640)
	g.scene.Update(Viewport{Y: -1, Height: 1})
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type startupGame struct {
	t                       *testing.T
	scene                   *Scene
	start                   time.Time
	ready, foreground, done bool
	frames                  int
	products                [vegetationProducts]bool
}

func (g *startupGame) Layout(int, int) (int, int) { return 640, 640 }
func (g *startupGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	if time.Since(g.start) > 30*time.Second {
		g.t.Fatalf("startup stalled: frames=%d, sections=%d pending=%d inflight=%d upload=%+v", g.frames, len(g.scene.world.sections), len(g.scene.world.pending), len(g.scene.world.inflight), g.scene.world.upload != nil)
	}
	g.scene.SetRenderWidth(640)
	g.ready = g.scene.Update(Viewport{Y: -1, Height: 1})
	return nil
}
func (g *startupGame) Draw(screen *ebiten.Image) {
	if g.done {
		return
	}
	g.frames++
	g.scene.Draw(screen, Viewport{Y: -1, Height: 1})
	if g.frames == 1 {
		screen.At(0, 0)
		g.t.Logf("first completed frame: %s", time.Since(g.start))
	}
	s := g.scene.world.sections[0]
	if !g.foreground && s != nil && s.foreground != nil {
		screen.At(0, 0) // include GPU completion through this frame
		g.foreground = true
		g.t.Logf("first foreground: %s, frame %d", time.Since(g.start), g.frames)
	}
	if s != nil {
		flushed := false
		for kind, name := range []string{"background vines", "webs", "mushrooms", "foreground vines"} {
			if !g.products[kind] && s.plants[kind].pixels > 0 {
				if !flushed {
					screen.At(0, 0)
					flushed = true
				}
				g.products[kind] = true
				g.t.Logf("%s published: %s, frame %d", name, time.Since(g.start), g.frames)
			}
		}
	}
	if g.ready {
		screen.At(0, 0)
		g.t.Logf("all visible layers: %s, frame %d", time.Since(g.start), g.frames)
		g.done = true
	}
}
