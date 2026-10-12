package infinicave

import (
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
	}, render.RockGrowthOptions{})
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
	w := newWorld(42, ViewShaded, 0, 0, terrain.DefaultSpiderWebDensity, terrain.DefaultMushroomDensity, false, nil, density)
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
	w := newWorld(42, ViewClay, 0, 0, terrain.DefaultSpiderWebDensity, terrain.DefaultMushroomDensity, false, testLedgeSection, render.RockGrowthOptions{})
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
	stationary, required := prefetch(-10.8, 1.8, 0)
	if !reflect.DeepEqual(stationary, []int64{9, 10, 8, 11}) || required != 4 {
		t.Fatalf("stationary view queued distant sections: %v, required=%d", stationary, required)
	}
	for _, velocity := range []float64{-.008, .008} {
		ids, gotRequired := prefetch(-10.8, 1.8, velocity)
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
						carved := render.SectionMesh{Geometry: geometry}
						scene.prepareCarvedVegetation(&carved)
						check(carved.Geometry.Topology)
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
