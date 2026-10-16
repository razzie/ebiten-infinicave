package infinicave

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
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
		results: make(chan sectionResult, 2), terrainMeshes: make(chan sectionResult, 2), canceled: make(chan sectionCancellation, 2)}
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
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 1}, request: w.inflight[1].request}
	w.collect()
	if _, exists := w.inflight[0]; !exists || len(w.inflight) != 1 || !w.working {
		t.Fatal("out-of-order completion stopped another section")
	}
	w.prune(-100.8, .8, 0)
	if w.inflight[0].ctx.Err() == nil {
		t.Fatal("obsolete concurrent job was not canceled")
	}
	w.terrainMeshes <- sectionResult{SectionMesh: render.SectionMesh{ID: 0, TerrainOnly: true}, request: w.inflight[0].request}
	w.canceled <- sectionCancellation{id: 0, request: w.inflight[0].request}
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
		mesh.Geometry = terrain.BindTerrainVegetation(early, data)
		want[id] = mesh
	}
	w := newWorld(42, ViewClay, 0, 0, terrain.DefaultSpiderWebDensity, terrain.DefaultMushroomDensity, false, nil, render.RockGrowthOptions{}, terrain.ShapeOptions{})
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
			if !exists || !reflect.DeepEqual(got.SectionMesh, expected) {
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
		results: make(chan sectionResult, 2), terrain: make(chan sectionTerrain, 2)}
	calls := 0
	g := &Scene{world: w, onCollisionReady: func(CollisionGeometry) { calls++ }}
	w.request(0)
	first := <-w.jobs
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: first.request}
	w.collect()
	w.terrain <- sectionTerrain{id: 0, ctx: first.ctx, request: first.request, geometry: &terrain.Geometry{}}
	w.receiveCollision(g)
	if calls != 1 {
		t.Fatal("normal completion suppressed an unread collision callback")
	}
	w.request(1)
	second := <-w.jobs
	w.prune(-100.8, .8, 0)
	w.terrain <- sectionTerrain{id: 1, ctx: second.ctx, request: second.request, geometry: &terrain.Geometry{}}
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

func TestGrowthPublishesBeforeBackgroundWithoutHelpers(t *testing.T) {
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)
	w := newWorld(42, ViewShaded, 0, 0, 0, 0, false, nil, render.RockGrowthOptions{Moss: .2}, terrain.ShapeOptions{})
	defer w.close()
	w.request(0)
	var base render.SectionMesh
	select {
	case result := <-w.terrainMeshes:
		base = result.SectionMesh
		if !base.ForegroundBaseOnly {
			t.Fatal("first recipe was not the structural base")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("base preparation stalled")
	}
	select {
	case growth := <-w.terrainMeshes:
		if growth.LayerMask() != render.ForegroundLayer || growth.ForegroundBaseOnly || len(growth.Background.Faces.Indices) != 0 {
			t.Fatal("growth waited for background preparation")
		}
		if &growth.Foreground.Faces.Vertices[0] != &base.Foreground.Faces.Vertices[0] {
			t.Fatal("early growth rebuilt the structural recipe")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("growth preparation stalled")
	}
}

func requestTestWorld(t *testing.T) *world {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := &world{
		ctx: ctx, jobs: make(chan sectionJob, 2), inflight: make(map[int64]sectionFlight),
		results: make(chan sectionResult, 4), terrainMeshes: make(chan sectionResult, 4),
		canceled: make(chan sectionCancellation, 4), terrain: make(chan sectionTerrain, 4),
		sections: make(map[int64]*worldSection), parked: make(map[int64]*sectionUpload),
		cancel: cancel, done: make(chan struct{}),
	}
	t.Cleanup(w.close)
	return w
}

func TestStaleWorkerRepliesPreserveNewRequestAndProducts(t *testing.T) {
	w := requestTestWorld(t)
	w.request(0)
	old := <-w.jobs
	w.finishJob(0, false)
	w.request(0)
	current := <-w.jobs
	if old.request.epoch != current.request.epoch || old.request.incarnation == current.request.incarnation {
		t.Fatal("reload did not assign a new incarnation within the same world")
	}
	mesh := render.SectionMesh{ID: 0, Layers: render.ForegroundLayer, ForegroundBaseOnly: true, Geometry: &terrain.Geometry{}}
	section := &worldSection{mesh: &mesh, geometry: mesh.Geometry}
	w.sections[0] = section
	parked := &sectionUpload{data: mesh}
	w.parked[0] = parked
	w.terrainMeshes <- sectionResult{SectionMesh: mesh, request: current.request}
	// Stale products must neither merge into the replacement nor discard its
	// staging images/published siblings when a late final or cancel follows.
	w.terrainMeshes <- sectionResult{SectionMesh: render.SectionMesh{ID: 0, Layers: render.BackgroundLayer}, request: old.request}
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: old.request}
	w.canceled <- sectionCancellation{id: 0, request: old.request}
	w.collect()
	flight, active := w.inflight[0]
	if !active || flight.request != current.request || flight.ctx.Err() != nil || !w.working {
		t.Fatal("old reply finished the replacement request")
	}
	if w.pending[0].LayerMask() != render.ForegroundLayer || w.pending[0].Geometry != mesh.Geometry ||
		w.sections[0] != section || w.parked[0] != parked {
		t.Fatal("old reply changed current pending, staged, or published products")
	}
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: current.request}
	w.canceled <- sectionCancellation{id: 0, request: old.request}
	w.collect()
	if w.working || len(w.inflight) != 0 || w.pending[0].LayerMask() != render.AllLayers {
		t.Fatal("replacement could not complete after stale replies")
	}
}

func TestLateCompletedCollisionCannotReplaceReloadedGeometry(t *testing.T) {
	w := requestTestWorld(t)
	calls := 0
	g := &Scene{world: w, onCollisionReady: func(CollisionGeometry) { calls++ }}
	w.request(0)
	old := <-w.jobs
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: old.request}
	w.collect()
	if context.Cause(old.ctx) != errSectionComplete {
		t.Fatal("old request did not complete normally")
	}
	w.request(0)
	current := <-w.jobs
	geometry := &terrain.Geometry{}
	w.terrain <- sectionTerrain{id: 0, request: current.request, ctx: current.ctx, geometry: geometry}
	w.terrain <- sectionTerrain{id: 0, request: old.request, ctx: old.ctx, geometry: &terrain.Geometry{}}
	w.receiveCollision(g)
	if calls != 1 || w.collision[0] != geometry {
		t.Fatal("completed old request restored obsolete collision geometry")
	}
}

func TestWorldEpochRejectsResultsFromPreviousWorld(t *testing.T) {
	oldWorld, currentWorld := requestTestWorld(t), requestTestWorld(t)
	oldWorld.request(0)
	old := <-oldWorld.jobs
	currentWorld.request(0)
	current := <-currentWorld.jobs
	if old.request.epoch == current.request.epoch || old.request.incarnation != current.request.incarnation {
		t.Fatal("world identity did not distinguish identical first requests")
	}
	currentWorld.terrainMeshes <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: old.request}
	currentWorld.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: old.request}
	currentWorld.canceled <- sectionCancellation{id: 0, request: old.request}
	currentWorld.terrain <- sectionTerrain{id: 0, request: old.request, ctx: old.ctx, geometry: &terrain.Geometry{}}
	currentWorld.collect()
	currentWorld.receiveCollision(&Scene{world: currentWorld})
	if len(currentWorld.pending) != 0 || len(currentWorld.collision) != 0 || len(currentWorld.inflight) != 1 {
		t.Fatal("old world reply changed the current world")
	}
}

func TestEvictionForgetsCompletedRequestAndRejectsLateCollision(t *testing.T) {
	w := requestTestWorld(t)
	w.request(0)
	job := <-w.jobs
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: job.request}
	w.collect()
	w.prune(-100.8, .8, 0)
	if len(w.requests) != 0 {
		t.Fatal("eviction retained obsolete request identities")
	}
	w.terrain <- sectionTerrain{id: 0, request: job.request, ctx: job.ctx, geometry: &terrain.Geometry{}}
	w.receiveCollision(&Scene{world: w})
	if len(w.collision) != 0 {
		t.Fatal("completed request resurrected evicted collision geometry")
	}
}

func TestReclaimedResultCannotPublishLateCollision(t *testing.T) {
	w := requestTestWorld(t)
	w.viewport = Viewport{Y: -.8, Height: .8}
	w.request(7)
	job := <-w.jobs
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 7}, request: job.request}
	w.collect()
	if !w.reclaimSpeculativeResult(0) {
		t.Fatal("required section did not reclaim a distant unpublished result")
	}
	if _, retained := w.requests[7]; retained {
		t.Fatal("reclaimed result retained its request identity")
	}
	w.terrain <- sectionTerrain{id: 7, request: job.request, ctx: job.ctx, geometry: &terrain.Geometry{}}
	w.receiveCollision(&Scene{world: w})
	if len(w.collision) != 0 {
		t.Fatal("late collision resurrected a reclaimed speculative result")
	}
}

func TestCancellationKeepsPublishedSiblingsAndRetriesMissingProducts(t *testing.T) {
	w := requestTestWorld(t)
	w.pixels = 8
	w.request(0)
	job := <-w.jobs
	mesh := render.SectionMesh{ID: 0, Layers: render.ForegroundLayer, ForegroundBaseOnly: true, Geometry: &terrain.Geometry{}}
	rock, grass := ebiten.NewImage(8, 8), ebiten.NewImage(8, 8)
	section := &worldSection{
		mesh: &mesh, geometry: mesh.Geometry, rockSource: &mesh,
		foreground: rock, grass: grass, pixels: 8, grassPixels: 8,
		vegetationPending: true,
	}
	section.foregroundReveal.Start(nil)
	section.foregroundReveal.Update()
	section.grassReveal.Start(nil)
	section.grassReveal.Update()
	rockReveal, grassReveal := section.foregroundReveal.Progress(), section.grassReveal.Progress()
	w.sections[0] = section
	w.collision = map[int64]*terrain.Geometry{0: mesh.Geometry}
	w.inflight[0].cancel(context.Canceled)
	w.canceled <- sectionCancellation{id: 0, request: job.request}
	w.collect()
	if w.sections[0] != section || section.foreground != rock || section.grass != grass ||
		section.foregroundReveal.Progress() != rockReveal || section.grassReveal.Progress() != grassReveal ||
		w.collision[0] != mesh.Geometry || w.revision != 0 || w.collisionRevision != 0 {
		t.Fatal("canceling missing details removed published siblings or restarted their reveals")
	}
	if w.ensure(-.8, .8, 0) {
		t.Fatal("canceled incomplete section reported readiness")
	}
	retry := <-w.jobs
	if retry.id != 0 || retry.request == job.request || w.sections[0] != section {
		t.Fatal("missing products did not retry while retaining visible siblings")
	}
}

func TestRetryReusesBoundedOutstandingSectionSlot(t *testing.T) {
	w := requestTestWorld(t)
	w.pending = make(map[int64]render.SectionMesh)
	for id := int64(0); id < sectionQueueLimit; id++ {
		w.pending[id] = render.SectionMesh{ID: id, Layers: render.ForegroundLayer}
	}
	w.request(0)
	if len(w.inflight) != 1 || len(w.pending) != sectionQueueLimit {
		t.Fatal("retry needed another section slot or reclaimed another section's recipe")
	}
	w.request(sectionQueueLimit)
	if len(w.inflight) != 1 {
		t.Fatal("new section bypassed the bounded outstanding queue")
	}
}

func TestClosedWorldRejectsQueuedWorkerReplies(t *testing.T) {
	w := requestTestWorld(t)
	w.request(0)
	job := <-w.jobs
	w.terrainMeshes <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: job.request}
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}, request: job.request}
	w.terrain <- sectionTerrain{id: 0, request: job.request, ctx: job.ctx, geometry: &terrain.Geometry{}}
	w.close()
	w.collect()
	w.receiveCollision(&Scene{world: w})
	if len(w.pending) != 0 || len(w.collision) != 0 || len(w.requests) != 0 {
		t.Fatal("queued replies recreated resources after close")
	}
}

func TestCloseDrainsLateCanceledCPURepliesAfterProducerExit(t *testing.T) {
	w := &world{jobs: make(chan sectionJob, 1), results: make(chan sectionResult, 1), terrain: make(chan sectionTerrain, 1), done: make(chan struct{}), maxJobs: 1}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	entered, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		w.runSectionPhases(func() *terrain.Builder { return nil }, func(_ *terrain.Builder, job sectionJob, ctx context.Context) *sectionJob {
			close(entered)
			<-ctx.Done()
			// Model a buffered publication winning a select after cancellation.
			w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 0}}
			w.terrain <- sectionTerrain{geometry: &terrain.Geometry{}}
			return nil
		})
	}()
	w.jobs <- sectionJob{ctx: w.ctx, phase: sectionForeground}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("CPU producer did not start")
	}
	w.close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled CPU producer did not exit")
	}
	if len(w.results)+len(w.terrain)+len(w.jobs) != 0 {
		t.Fatal("closed world retained late CPU snapshots")
	}
}
