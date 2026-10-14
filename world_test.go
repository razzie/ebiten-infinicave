package infinicave

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestWorldCancelsObsoleteGenerationAndResumes(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	w := newWorld(42, ViewClay, 0, 0, terrain.DefaultSpiderWebDensity, terrain.DefaultMushroomDensity, false, func(int64) SectionContent {
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		return SectionContent{}
	}, render.RockGrowthOptions{}, terrain.ShapeOptions{})
	defer w.close()
	w.request(0)
	select {
	case <-entered:
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatal("loader did not start")
	}
	w.prune(-100.8, .8, 0)
	close(release)
	select {
	case id := <-w.canceled:
		if id != 0 {
			t.Fatal("wrong section was canceled")
		}
		w.canceled <- id
		w.collect()
	case <-time.After(20 * time.Second):
		t.Fatal("obsolete generation did not stop")
	}
	if w.working || len(w.results)+len(w.terrain)+len(w.terrainMeshes) != 0 {
		t.Fatal("canceled generation published geometry or remained active")
	}
	w.request(100)
	select {
	case mesh := <-w.results:
		if mesh.ID != 100 || len(mesh.Background.Faces.Indices) == 0 {
			t.Fatal("worker did not resume at the new viewport")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("worker did not resume after cancellation")
	}
}

func TestWorldWorkerPreparesAllLayers(t *testing.T) {
	density := render.RockGrowthOptions{Moss: .23, Hanging: .17, Grass: .13}
	w := newWorld(42, ViewShaded, 0, 0, terrain.DefaultSpiderWebDensity, terrain.DefaultMushroomDensity, false, nil, density, terrain.ShapeOptions{})
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
		if mesh.ID != 0 || len(mesh.Background.Faces.Indices) == 0 || len(mesh.Foreground.Faces.Indices) == 0 || len(mesh.Vines) == 0 || len(mesh.ForegroundVines) == 0 || len(mesh.Plants) == 0 {
			t.Fatal("worker returned incomplete section geometry")
		}
		checkMesh(t, mesh.Background.Faces)
		checkMesh(t, mesh.Foreground.Faces)
		checkMesh(t, mesh.Mushrooms)
		density.Guides = mesh.Geometry.Guides
		want := render.PrepareOwnedGridWithTopology(nil, ViewShaded, mesh.Geometry.Topology, density)
		if !reflect.DeepEqual(mesh.Foreground, want) {
			t.Fatal("streaming ignored configured rock growth density")
		}
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
	w := newWorld(42, ViewClay, 0, 0, terrain.DefaultSpiderWebDensity, terrain.DefaultMushroomDensity, false, testLedgeSection, render.RockGrowthOptions{}, terrain.ShapeOptions{})
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
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan sectionJob, 1)}
	if w.ensure(-.800, .800, 0) {
		t.Fatal("unloaded viewport reported ready")
	}
	if job := <-w.jobs; job.id != 0 {
		t.Fatalf("first request is %d, want floor section", job.id)
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

func TestPrefetchOnlyExpandsWhileMoving(t *testing.T) {
	stationary, required := prefetch(-10.8, 1.8, 0, DefaultConfig().LookaheadSections)
	if !reflect.DeepEqual(stationary, []int64{9, 10, 8, 11}) || required != 4 {
		t.Fatalf("stationary view queued distant sections: %v, required=%d", stationary, required)
	}
	for _, velocity := range []float64{-.008, .008} {
		ids, gotRequired := prefetch(-10.8, 1.8, velocity, DefaultConfig().LookaheadSections)
		if gotRequired != required || !reflect.DeepEqual(ids[:required], stationary) || len(ids) > sectionQueueLimit {
			t.Fatalf("moving prefetch lost required bands or exceeded its bound: %v", ids)
		}
		want := int64(12)
		if velocity > 0 {
			want = 7
		}
		if ids[required] != want {
			t.Fatalf("velocity %g looks ahead to %d, want %d", velocity, ids[required], want)
		}
	}
}

func TestConfiguredLookaheadInBothOrientations(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, count := range []int{0, 1, 4, 12} {
			for _, velocity := range []float64{0, -.0001, -.02, .0001, .02} {
				viewport := Viewport{Y: -11, Height: 1, Velocity: velocity}
				if orientation == Horizontal {
					viewport = Viewport{X: 10, Width: 1, Velocity: velocity}
				}
				v, valid := orientedViewport(orientation, viewport)
				ids, required := prefetch(v.Y, v.Height, v.Velocity, count)
				want := []int64{10, 9, 11}
				if velocity != 0 {
					first, step := int64(12), int64(1)
					if v.Velocity > 0 {
						first, step = 8, -1
					}
					for i := 0; i < count; i++ {
						want = append(want, first+int64(i)*step)
					}
				}
				if !valid || required != 3 || !reflect.DeepEqual(ids, want) {
					t.Fatalf("%v count=%d velocity=%g: got %v/%d, want %v/3", orientation, count, velocity, ids, required, want)
				}
			}
		}
	}
}

func TestConfiguredRetentionAndLookahead(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, tc := range []struct {
			retain, lookahead int
			velocity          float64
			first, last       int64
		}{
			{0, 0, 0, 9, 11}, // vegetation neighbors survive zero retention
			{1, 4, 0, 9, 11}, // stationary views do not retain lookahead
			{3, 0, 0, 7, 13},
			{6, 1, 0, 4, 16},
			{0, 4, -.01, 9, 15},
			{0, 4, .01, 5, 11},
		} {
			viewport := Viewport{Y: -11, Height: 1, Velocity: tc.velocity}
			if orientation == Horizontal {
				viewport = Viewport{X: 10, Width: 1, Velocity: -tc.velocity}
			}
			v, _ := orientedViewport(orientation, viewport)
			w := &world{retainSections: tc.retain, lookaheadSections: tc.lookahead,
				sections: make(map[int64]*worldSection), collision: make(map[int64]*terrain.Geometry)}
			for id := int64(0); id < 20; id++ {
				w.sections[id] = &worldSection{}
				w.collision[id] = &terrain.Geometry{}
			}
			// An active upload survives until the upload lifecycle releases it.
			w.upload = &sectionUpload{data: render.SectionMesh{ID: 19}}
			w.prune(v.Y, v.Height, v.Velocity)
			for id := int64(0); id < 20; id++ {
				want := id >= tc.first && id <= tc.last
				if (w.sections[id] != nil) != (want || id == 19) || (w.collision[id] != nil) != want {
					t.Fatalf("%v retention=%d lookahead=%d velocity=%g: wrong retention for section %d", orientation, tc.retain, tc.lookahead, tc.velocity, id)
				}
			}
		}
	}
}

func TestLookaheadDoesNotDelayReadinessOrExceedQueue(t *testing.T) {
	w := &world{sections: map[int64]*worldSection{9: {}, 10: {}, 11: {}},
		jobs: make(chan sectionJob, sectionQueueLimit), inflight: make(map[int64]sectionFlight),
		ctx: context.Background(), lookaheadSections: 20}
	defer func() {
		for _, flight := range w.inflight {
			flight.cancel(context.Canceled)
		}
	}()
	if !w.ensure(-11, 1, -.01) {
		t.Fatal("lookahead delayed a ready viewport")
	}
	if len(w.jobs) != sectionQueueLimit || len(w.inflight) != sectionQueueLimit {
		t.Fatalf("lookahead did not respect queue capacity: %d jobs, %d flights", len(w.jobs), len(w.inflight))
	}
}

func TestNeighborGenerationStillDelaysReadinessDuringPreload(t *testing.T) {
	early := resolutionTestMesh(11)
	early.Layers, early.TerrainOnly = render.ForegroundLayer, true
	w := &world{sections: map[int64]*worldSection{9: {}, 10: {}, 11: {mesh: &early, vegetationPending: true}},
		lookaheadSections: 1, viewport: Viewport{Y: -11, Height: 1}}
	if w.ensure(-11, 1, 0) {
		t.Fatal("neighbor generation reported ready before crossing plants were known")
	}
	complete := early
	complete.Layers, complete.TerrainOnly = render.AllLayers, false
	w.savePending(complete)
	if !w.ensure(-11, 1, 0) {
		t.Fatal("purely offscreen GPU uploads delayed completed neighbor generation")
	}
}

func TestRetentionDoesNotBlockNewViewportRequests(t *testing.T) {
	w := &world{sections: make(map[int64]*worldSection), pending: make(map[int64]render.SectionMesh),
		jobs: make(chan sectionJob, sectionQueueLimit), inflight: make(map[int64]sectionFlight),
		ctx: context.Background(), retainSections: 20}
	defer func() {
		for _, flight := range w.inflight {
			flight.cancel(context.Canceled)
		}
	}()
	for id := int64(0); id < sectionQueueLimit; id++ {
		w.pending[id] = render.SectionMesh{ID: id}
	}
	// Completed offscreen results initially fill the queue. Pruning must free
	// capacity even when those IDs lie inside the configured retention range.
	w.ensure(-11, 1, 0)
	w.prune(-11, 1, 0)
	if w.ensure(-11, 1, 0) || len(w.jobs) != 3 {
		t.Fatalf("obsolete pending results blocked the new viewport: %d requests", len(w.jobs))
	}
	if job := <-w.jobs; job.id != 10 {
		t.Fatalf("first request is %d, want visible section 10", job.id)
	}
}

func TestVisibleRequestsReclaimFullLookaheadQueue(t *testing.T) {
	w := &world{sections: map[int64]*worldSection{9: {}, 11: {}}, pending: make(map[int64]render.SectionMesh),
		jobs: make(chan sectionJob, sectionQueueLimit), inflight: make(map[int64]sectionFlight),
		ctx: context.Background(), retainSections: 20, lookaheadSections: 20,
		viewport: Viewport{Y: -11, Height: 1, Velocity: -.01}}
	defer func() {
		for _, flight := range w.inflight {
			flight.cancel(context.Canceled)
		}
	}()
	for id := int64(12); id < 12+sectionQueueLimit; id++ {
		w.pending[id] = render.SectionMesh{ID: id}
	}
	if w.ensure(w.viewport.Y, w.viewport.Height, w.viewport.Velocity) || len(w.jobs) != 1 {
		t.Fatalf("full lookahead queue blocked visible terrain: %d requests", len(w.jobs))
	}
	if job := <-w.jobs; job.id != 10 {
		t.Fatalf("requested section %d instead of visible section 10", job.id)
	}
	if _, ok := w.pending[19]; ok {
		t.Fatal("farthest speculative result was not reclaimed")
	}
	if _, ok := w.pending[12]; !ok && (w.upload == nil || w.upload.data.ID != 12) {
		t.Fatal("nearest speculative result was discarded")
	}
}

func TestSceneSpiderWebSizeSurvivesReset(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		t.Run(orientation.String(), func(t *testing.T) {
			scene, err := NewScene(Config{Seed: 42, Orientation: orientation, SpiderWebMaxSize: .08, SpiderWebDensity: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer scene.Close()
			for _, seed := range []int64{42, 43} {
				if seed != 42 {
					scene.Reset(seed)
				}
				scene.world.request(0)
				select {
				case mesh := <-scene.world.results:
					webs := mesh.Geometry.Topology.SpiderWebs
					if len(webs) == 0 {
						t.Fatal("streamed section has no webs to check")
					}
					for _, w := range webs {
						for i, a := range w.Network.Nodes {
							for _, b := range w.Network.Nodes[i+1:] {
								if a.Sub(b).Len() > .08+1e-9 {
									t.Fatalf("seed %d: scene or reset ignored the web size setting", seed)
								}
							}
						}
					}
				case <-time.After(20 * time.Second):
					t.Fatal("streamed section did not complete")
				}
			}
		})
	}
}

func TestSceneMushroomDensityMatchesGenerationAfterReset(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, density := range []float64{0, .5, 1} {
			config := Config{Seed: 42, Orientation: orientation, ForceGuidesUp: true, MushroomDensity: density, LoadSection: testLedgeSection}
			expected, err := GenerateSectionWithConfig(config, 0)
			if err != nil {
				t.Fatal(err)
			}
			if density == 0 && len(expected.Mushrooms) != 0 {
				t.Fatal("synchronous generation ignored zero mushroom density")
			}
			if density == 1 && len(expected.Mushrooms) == 0 {
				t.Fatal("fixture generated no mushrooms at full density")
			}
			scene, err := NewScene(config)
			if err != nil {
				t.Fatal(err)
			}
			var before []MushroomGroup
			for attempt := 0; attempt < 2; attempt++ {
				if attempt > 0 {
					scene.Reset(config.Seed)
				}
				scene.world.request(0)
				select {
				case mesh := <-scene.world.results:
					groups := mesh.Geometry.Vegetation.Mushrooms
					if len(groups) != len(expected.Mushrooms) {
						scene.Close()
						t.Fatalf("orientation %v density %g: streamed %d clusters, synchronous generation returned %d", orientation, density, len(groups), len(expected.Mushrooms))
					}
					if attempt > 0 && !reflect.DeepEqual(before, groups) {
						scene.Close()
						t.Fatal("reset changed mushroom density or surviving clusters")
					}
					before = groups
				case <-time.After(20 * time.Second):
					scene.Close()
					t.Fatal("streamed section did not complete")
				}
			}
			scene.Close()
		}
	}
}

func TestSceneForceGuidesUpSurvivesReset(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		t.Run(orientation.String(), func(t *testing.T) {
			scene, err := NewScene(Config{Seed: 42, Orientation: orientation, ForceGuidesUp: true, LoadSection: func(id int64) SectionContent {
				if id == 0 {
					return SectionContent{Guides: []Guide{{Pts: []V{{X: .8, Y: .4}, {X: .2, Y: .4}}}}}
				}
				return SectionContent{}
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer scene.Close()
			for _, seed := range []int64{42, 43} {
				if seed != 42 {
					scene.Reset(seed)
				}
				scene.world.request(0)
				select {
				case result := <-scene.world.terrain:
					collision := terrain.OrientedCollision(orientation, result.geometry.Collision)
					origin := collision.Origin
					if !collision.Contains(V{X: .5, Y: .43}.Add(origin)) || collision.Contains(V{X: .5, Y: .37}.Add(origin)) {
						t.Fatalf("seed %d: scene or reset ignored ForceGuidesUp", seed)
					}
				case <-time.After(20 * time.Second):
					t.Fatal("streamed terrain did not complete")
				}
			}
		})
	}
}

func TestSceneSpiderWebDensitySurvivesResetAndCarving(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, tc := range []struct {
			name    string
			density float64
		}{{"disabled", 0}, {"sparse", .2}} {
			t.Run(orientation.String()+"/"+tc.name, func(t *testing.T) {
				scene, err := NewScene(Config{Seed: 42, Orientation: orientation, SpiderWebDensity: tc.density})
				if err != nil {
					t.Fatal(err)
				}
				defer scene.Close()
				check := func(topology *terrain.RockTopology) {
					if tc.density == 0 && len(topology.SpiderWebs) != 0 {
						t.Fatal("scene restored disabled webs")
					}
					for _, w := range topology.SpiderWebs {
						key := terrain.CollisionVertexKey(terrain.RockMaterialPoint(topology.Grid[0], w.Corner))
						if terrain.SiteRandom(0x737069646572, key[0], key[1], 0) >= tc.density {
							t.Fatal("scene ignored density when selecting a pocket")
						}
					}
				}
				seen := 0
				for _, seed := range []int64{42, 43} {
					if seed != 42 {
						scene.Reset(seed)
					}
					scene.world.request(0)
					select {
					case mesh := <-scene.world.results:
						check(mesh.Geometry.Topology)
						seen += len(mesh.Geometry.Topology.SpiderWebs)
						cut, err := terrain.CutFromHole(Hole{Shape: HoleCircle, Center: mesh.Geometry.Grid[0].Center.Add(V{Y: mesh.Geometry.Top}), Radius: .005})
						if err != nil {
							t.Fatal(err)
						}
						geometry, _, changed := cut.Geometry(0, mesh.Geometry, 0)
						if !changed {
							t.Fatal("fixture carve did not change terrain")
						}
						check(geometry.Topology)
					case <-time.After(20 * time.Second):
						t.Fatal("streamed section did not complete")
					}
				}
				if tc.density > 0 && seen == 0 {
					t.Fatal("sparse scene produced no webs to check")
				}
			})
		}
	}
}
