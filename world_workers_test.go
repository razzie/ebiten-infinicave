package infinicave

import (
	"context"
	"fmt"
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
		w.runSectionPhases(func() *terrain.Builder { return terrain.NewSectionBuilder(42, nil) }, func(b *terrain.Builder, j sectionJob, _ context.Context) *sectionJob {
			started <- startedJob{j.id, b}
			if j.id == 0 {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			finished <- j.id
			return nil
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
		build := builder.Begin(context.Background(), id)
		early := terrain.PrepareTerrainGeometry(build.Data(), 0)
		if !build.Materialize(context.Background()) || !builder.Decorate(context.Background(), build) {
			t.Fatal("serial build failed")
		}
		data := build.Data()
		mesh := render.PrepareVegetation(data, ViewClay)
		rock := render.PrepareTerrain(data, ViewClay)
		mesh.Background, mesh.Foreground = rock.Background, rock.Foreground
		mesh.Layers = render.AllLayers
		mesh.Geometry = terrain.BindTerrainMaterial(early, data)
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

func TestForegroundPhaseInterruptsVegetationAndKeepsVisiblePriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &world{jobs: make(chan sectionJob, 8), done: make(chan struct{}), maxJobs: 1, priorities: make(chan Viewport, 1)}
	w.priorities <- Viewport{Y: -1.8, Height: 1.8}
	started := make(chan int64, 8)
	yielded := make(chan struct{})
	allowYield := make(chan struct{})
	stopped := make(chan struct{})
	attempts := 0
	go func() {
		defer close(stopped)
		w.runSectionPhases(func() *terrain.Builder { return terrain.NewSectionBuilder(42, nil) }, func(_ *terrain.Builder, job sectionJob, phaseCtx context.Context) *sectionJob {
			started <- job.id
			if job.phase == sectionVegetation {
				attempts++
				if attempts == 1 {
					<-phaseCtx.Done()
					if context.Cause(phaseCtx) != errSectionYield {
						return nil
					}
					close(yielded)
					<-allowYield
				}
			}
			return nil
		})
	}()
	defer func() { cancel(); close(w.done); <-stopped }()
	next := func() int64 {
		t.Helper()
		select {
		case id := <-started:
			return id
		case <-time.After(5 * time.Second):
			t.Fatal("phase did not start")
			return -1
		}
	}
	w.jobs <- sectionJob{id: 0, phase: sectionVegetation, ctx: ctx}
	if next() != 0 {
		t.Fatal("vegetation did not start")
	}
	w.jobs <- sectionJob{id: 100, phase: sectionBackground, ctx: ctx}
	w.jobs <- sectionJob{id: 1, phase: sectionGeometry, ctx: ctx}
	select {
	case <-yielded:
	case <-time.After(5 * time.Second):
		t.Fatal("vegetation did not yield")
	}
	close(allowYield)
	if next() != 1 {
		t.Fatal("visible foreground did not precede background and vegetation")
	}
	if next() != 0 {
		t.Fatal("interrupted vegetation was lost")
	}
	if next() != 100 {
		t.Fatal("speculative background preceded visible vegetation")
	}
}

func TestQueuedSectionsStayBoundedWithoutHoldingWorkerSlots(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &world{ctx: ctx, jobs: make(chan sectionJob, sectionQueueLimit), inflight: make(map[int64]sectionFlight), maxJobs: 1}
	for id := int64(0); id < 30; id++ {
		w.request(id)
	}
	if len(w.jobs) != sectionQueueLimit || len(w.inflight) != sectionQueueLimit {
		t.Fatal("queued sections are not bounded independently of CPU workers")
	}
}

func TestSectionPhasesPrioritizeViewportAndTravelDirection(t *testing.T) {
	for _, velocity := range []float64{0, -.008, .008} {
		t.Run(fmt.Sprintf("velocity=%g", velocity), func(t *testing.T) {
			ctx := context.Background()
			w := &world{jobs: make(chan sectionJob, 8), done: make(chan struct{}), maxJobs: 1, priorities: make(chan Viewport, 1)}
			w.priorities <- Viewport{Y: -2.75, Height: 1.5, Velocity: velocity} // Visible: 1 and 2.
			for id := int64(0); id < 5; id++ {
				w.jobs <- sectionJob{id: id, ctx: ctx}
			}
			type phase struct {
				id   int64
				kind int
			}
			started := make(chan phase, 20)
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				w.runSectionPhases(func() *terrain.Builder { return terrain.NewSectionBuilder(42, nil) }, func(_ *terrain.Builder, job sectionJob, _ context.Context) *sectionJob {
					started <- phase{job.id, job.phase}
					if job.phase == sectionVegetation {
						return nil
					}
					job.phase++
					return &job
				})
			}()
			defer func() { close(w.done); <-stopped }()
			order := make(map[phase]int)
			for i := 0; i < 20; i++ {
				select {
				case job := <-started:
					order[job] = i
				case <-time.After(5 * time.Second):
					t.Fatal("queued phases stalled")
				}
			}
			before := func(a, b phase) {
				t.Helper()
				if order[a] >= order[b] {
					t.Fatalf("phase %+v did not precede %+v: %v", a, b, order)
				}
			}
			for _, id := range []int64{1, 2} {
				before(phase{id, sectionForeground}, phase{0, sectionGeometry})
				before(phase{id, sectionForeground}, phase{3, sectionGeometry})
				before(phase{id, sectionBackground}, phase{id, sectionVegetation})
				if velocity == 0 {
					before(phase{id, sectionVegetation}, phase{0, sectionGeometry})
					before(phase{id, sectionVegetation}, phase{3, sectionGeometry})
				} else {
					approaching, departing := int64(3), int64(0)
					if velocity > 0 {
						approaching, departing = departing, approaching
					}
					before(phase{approaching, sectionForeground}, phase{id, sectionBackground})
					before(phase{id, sectionVegetation}, phase{departing, sectionGeometry})
				}
			}
			for _, id := range []int64{0, 3} {
				before(phase{id, sectionVegetation}, phase{4, sectionGeometry})
			}
		})
	}
}

func TestSpareWorkerDoesNotInterruptVisibleVegetation(t *testing.T) {
	for _, id := range []int64{1, 3} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			old := runtime.GOMAXPROCS(4)
			defer runtime.GOMAXPROCS(old)
			w := &world{jobs: make(chan sectionJob, 8), done: make(chan struct{}), maxJobs: 2, priorities: make(chan Viewport, 1)}
			w.priorities <- Viewport{Y: -1.8, Height: 1.8}
			vegetation := make(chan context.Context, 8)
			prefetched := make(chan struct{}, 8)
			release := make(chan struct{})
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				w.runSectionPhases(func() *terrain.Builder { return terrain.NewSectionBuilder(42, nil) }, func(_ *terrain.Builder, job sectionJob, ctx context.Context) *sectionJob {
					if job.phase == sectionVegetation {
						vegetation <- ctx
					} else {
						prefetched <- struct{}{}
					}
					select {
					case <-release:
					case <-ctx.Done():
					}
					return nil
				})
			}()
			defer func() { close(release); close(w.done); <-stopped }()
			w.jobs <- sectionJob{id: 0, phase: sectionVegetation, ctx: context.Background()}
			var active context.Context
			select {
			case active = <-vegetation:
			case <-time.After(5 * time.Second):
				t.Fatal("visible vegetation did not start")
			}
			w.jobs <- sectionJob{id: id, phase: sectionGeometry, ctx: context.Background()}
			select {
			case <-prefetched:
				if active.Err() != nil {
					t.Fatal("new geometry restarted visible vegetation despite a spare worker")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("spare worker did not start prefetch")
			}
		})
	}
}
