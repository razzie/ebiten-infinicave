package infinicave

import (
	"context"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestSectionWorkersOverlapWithSeparateBuilders(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &world{jobs: make(chan sectionJob, 2), done: make(chan struct{}), maxJobs: 2}
	type startedJob struct {
		id      int64
		builder *terrain.Builder
	}
	started := make(chan startedJob, 2)
	release := make(chan struct{})
	finished := make(chan int64, 2)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		w.runSectionJobs(func() *terrain.Builder { return terrain.NewSectionBuilder(42, nil) }, func(b *terrain.Builder, j sectionJob) {
			started <- startedJob{j.id, b}
			if j.id == 0 {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			finished <- j.id
		})
	}()
	defer func() { cancel(); close(w.done); <-stopped }()
	w.jobs <- sectionJob{id: 0, ctx: ctx}
	var first startedJob
	select {
	case first = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("root did not start")
	}
	w.jobs <- sectionJob{id: 1, ctx: ctx}
	select {
	case second := <-started:
		if first.id != 0 || second.id != 1 || first.builder == second.builder {
			t.Fatal("workers share a builder or changed request priority")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second section cannot run while first is blocked")
	}
	select {
	case id := <-finished:
		if id != 1 {
			t.Fatal("second section did not finish independently")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second job stalled")
	}
	close(release)
	select {
	case id := <-finished:
		if id != 0 {
			t.Fatal("root result was lost")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("root job stalled")
	}
}

func TestConcurrentRequestsAndCancellationStayBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &world{ctx: ctx, jobs: make(chan sectionJob, 2), inflight: make(map[int64]sectionFlight), maxJobs: 2,
		results: make(chan render.SectionMesh, 2), terrainMeshes: make(chan render.SectionMesh, 2), canceled: make(chan int64, 2)}
	w.ensure(-.8, .8, 0)
	if len(w.inflight) != 2 || len(w.jobs) != 2 {
		t.Fatal("prefetch did not fill two bounded job slots")
	}
	w.ensure(-.8, .8, 0)
	if len(w.jobs) != 2 {
		t.Fatal("duplicate requests bypassed in-flight tracking")
	}
	// Simulate the worker dequeuing both accepted jobs.
	<-w.jobs
	<-w.jobs
	w.results <- render.SectionMesh{ID: 1}
	w.collect()
	if _, exists := w.inflight[0]; !exists || len(w.inflight) != 1 || !w.working {
		t.Fatal("out-of-order completion stopped another section")
	}
	w.prune(-100.8, .8, 0)
	if w.inflight[0].ctx.Err() == nil {
		t.Fatal("obsolete concurrent job was not canceled")
	}
	w.terrainMeshes <- render.SectionMesh{ID: 0, TerrainOnly: true}
	w.canceled <- 0
	w.collect()
	if w.working || len(w.inflight) != 0 {
		t.Fatal("canceled job stayed in flight")
	}
	if _, exists := w.pending[0]; exists {
		t.Fatal("canceled terrain result resurrected a section")
	}
	w.request(100)
	if len(w.inflight) != 1 {
		t.Fatal("streamer cannot resume after canceling obsolete jobs")
	}
}

func TestConcurrentWorldSectionsMatchSerialPreparation(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 3 {
		t.Skip("concurrent section jobs require at least three processors")
	}
	want := make(map[int64]render.SectionMesh)
	builder := terrain.NewSectionBuilder(42, nil)
	for id := int64(0); id < 2; id++ {
		data := builder.Build(id)
		mesh := render.PrepareVegetation(data, ViewClay)
		rock := render.PrepareTerrain(data, ViewClay)
		mesh.Background, mesh.Foreground = rock.Background, rock.Foreground
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		mesh.Geometry.Vegetation = terrain.SectionVegetation(data)
		want[id] = mesh
	}
	w := newWorld(42, ViewClay, 0, nil)
	defer w.close()
	w.request(0)
	w.request(1)
	if len(w.inflight) != 2 {
		t.Fatal("second section was not scheduled")
	}
	for range 2 {
		select {
		case got := <-w.results:
			expected, exists := want[got.ID]
			if !exists || !reflect.DeepEqual(got, expected) {
				t.Fatalf("concurrent preparation changed section %d", got.ID)
			}
			delete(want, got.ID)
		case <-time.After(30 * time.Second):
			t.Fatal("concurrent section preparation stalled")
		}
	}
}

func TestLateCollisionDeliveryDistinguishesCompletionFromCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &world{ctx: ctx, jobs: make(chan sectionJob, 2), inflight: make(map[int64]sectionFlight), maxJobs: 2,
		results: make(chan render.SectionMesh, 2), terrain: make(chan sectionTerrain, 2)}
	calls := 0
	g := &Scene{world: w, onCollisionReady: func(CollisionGeometry) { calls++ }}
	w.request(0)
	first := <-w.jobs
	w.results <- render.SectionMesh{ID: 0}
	w.collect()
	w.terrain <- sectionTerrain{id: 0, ctx: first.ctx, geometry: &terrain.Geometry{}}
	w.receiveCollision(g)
	if calls != 1 {
		t.Fatal("normal completion suppressed an unread collision callback")
	}
	w.request(1)
	second := <-w.jobs
	w.prune(-100.8, .8, 0)
	w.terrain <- sectionTerrain{id: 1, ctx: second.ctx, geometry: &terrain.Geometry{}}
	w.receiveCollision(g)
	if calls != 1 {
		t.Fatal("canceled job delivered obsolete collision geometry")
	}
	if _, exists := w.collision[1]; exists {
		t.Fatal("canceled collision event resurrected a section")
	}
}
