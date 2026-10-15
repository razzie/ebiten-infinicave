package infinicave

import (
	"context"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
