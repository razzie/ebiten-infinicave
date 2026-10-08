package infinicave

import (
	"context"
	"errors"
	"image"
	"math"
	"runtime"
	"slices"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type worldSection struct {
	terrain, vines, mushrooms, foreground               *ebiten.Image
	foregroundVines                                     *ebiten.Image
	geometry                                            *terrain.Geometry
	vinesBounds, foregroundVinesBounds, mushroomsBounds image.Rectangle
	vegetationPending                                   bool
	mesh                                                *render.SectionMesh
	pixels                                              int
	backgroundPixels, vegetationPixels                  int
}

type world struct {
	sections      map[int64]*worldSection
	jobs          chan sectionJob
	results       chan render.SectionMesh
	terrain       chan sectionTerrain
	terrainMeshes chan render.SectionMesh
	canceled      chan int64
	ctx           context.Context
	cancel        context.CancelFunc
	activeCancel  context.CancelFunc
	activeID      int64
	inflight      map[int64]sectionFlight
	maxJobs       int
	// Early collision topology is retained until terrain images publish.
	collision         map[int64]*terrain.Geometry
	done              chan struct{}
	closing           sync.Once
	working           bool
	upload            *sectionUpload
	white             *ebiten.Image
	revision          uint64 // queryable terrain version for the lazy query index
	collisionRevision uint64 // early collision availability changes
	queries           *worldQueryIndex
	cuts              []terrain.RockCut // world-space edits survive section eviction
	pixels            int
	pending           map[int64]render.SectionMesh
	viewport          Viewport
	priorities        chan Viewport
	parked            map[int64]*sectionUpload
}

type sectionJob struct {
	id       int64
	ctx      context.Context
	phase    int
	build    *terrain.SectionBuild
	geometry *terrain.Geometry
	mesh     render.SectionMesh
}

var errSectionComplete = errors.New("section generation completed")

type sectionFlight struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

type sectionTerrain struct {
	id       int64
	geometry *terrain.Geometry
	ctx      context.Context
}

func newWorld(seed int64, view View, tolerance float64, loadSection SectionLoader, orientation ...Orientation) *world {
	limit := min(2, max(1, runtime.GOMAXPROCS(0)-1))
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan sectionJob, sectionQueueLimit),
		results: make(chan render.SectionMesh, sectionQueueLimit*2), terrain: make(chan sectionTerrain, sectionQueueLimit),
		terrainMeshes: make(chan render.SectionMesh, sectionQueueLimit*2), canceled: make(chan int64, sectionQueueLimit),
		done: make(chan struct{}), inflight: make(map[int64]sectionFlight), maxJobs: limit, priorities: make(chan Viewport, 1), parked: make(map[int64]*sectionUpload)}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	// Keep application loader calls serialized within this world, even though
	// separate builders can generate their sections concurrently.
	var loaderMu sync.Mutex
	loader := loadSection
	if loader != nil {
		loadSection = func(id int64) SectionContent {
			loaderMu.Lock()
			defer loaderMu.Unlock()
			content := loader(id)
			content.Guides = slices.Clone(content.Guides)
			for i := range content.Guides {
				content.Guides[i].Pts = slices.Clone(content.Guides[i].Pts)
			}
			content.Holes = slices.Clone(content.Holes)
			return content
		}
	}

	advance := func(builder *terrain.Builder, job sectionJob, ctx context.Context) *sectionJob {
		id := job.id
		sendMesh := func(mesh render.SectionMesh, channel chan render.SectionMesh) bool {
			select {
			case <-ctx.Done():
				return false
			case channel <- mesh:
				return true
			}
		}
		ok := ctx.Err() == nil
		if ok {
			switch job.phase {
			case sectionGeometry:
				job.build = builder.Begin(ctx, id)
				ok = job.build != nil
				if ok {
					job.geometry = terrain.PrepareTerrainGeometry(job.build.Data(), tolerance)
					select {
					case <-ctx.Done():
						ok = false
					case w.terrain <- sectionTerrain{id: id, geometry: job.geometry, ctx: job.ctx}:
					}
				}
			case sectionForeground:
				ok = job.build.Materialize(ctx)
				if ok {
					job.geometry = terrain.BindTerrainMaterial(job.geometry, job.build.Data())
					job.mesh = render.PrepareForeground(job.build.Data(), view)
					job.mesh.Geometry = job.geometry
					ok = sendMesh(job.mesh, w.terrainMeshes)
				}
			case sectionBackground:
				mesh := render.PrepareBackground(job.build.Data(), view)
				mesh.Geometry = job.geometry
				job.mesh = render.MergeSectionMeshes(job.mesh, mesh)
				ok = sendMesh(mesh, w.terrainMeshes)
			case sectionVegetation:
				ok = builder.Decorate(ctx, job.build)
				if ok {
					mesh := render.PrepareVegetation(job.build.Data(), view)
					complete := *job.geometry
					complete.Vegetation = terrain.SectionVegetation(job.build.Data())
					mesh.Geometry = &complete
					mesh = render.MergeSectionMeshes(job.mesh, mesh)
					ok = sendMesh(mesh, w.results)
					if ok {
						job.phase = sectionComplete // completion must not retry if a yield races the send
						return &job
					}
				}
			}
		}
		if !ok {
			if context.Cause(ctx) == errSectionYield && job.ctx.Err() == nil {
				return nil
			}
			select {
			case <-w.done:
			case w.canceled <- id:
			}
			return nil
		}
		job.phase++
		return &job
	}
	go w.runSectionPhases(func() *terrain.Builder {
		return terrain.NewSectionBuilder(seed, loadSection, orientation...)
	}, advance)

	return w
}

func (w *world) close() {
	w.closing.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
		close(w.done)
	})
	if w.upload != nil {
		w.upload.deallocate()
	}
	for _, u := range w.parked {
		u.deallocate()
	}
	w.parked = nil
	w.upload = nil
	w.pending = nil
	w.collision = nil
	if w.white != nil {
		w.white.Deallocate()
		w.white = nil
	}
	for id, section := range w.sections {
		section.deallocate()
		delete(w.sections, id)
	}
}

func visibleSections(y float64, height float64) (low, high int64) {
	low = max(0, int64(math.Floor(-(y+height)/terrain.SectionHeight)))
	high = max(low, int64(math.Ceil(-y/terrain.SectionHeight))-1)
	return
}

func (w *world) request(id int64) {
	if w.inflight != nil {
		if _, exists := w.inflight[id]; exists {
			return
		}
		if len(w.inflight) >= sectionQueueLimit {
			return
		}
		// Count each retained result once, including paused GPU work.
		outstanding := make(map[int64]bool, sectionQueueLimit)
		for id := range w.inflight {
			outstanding[id] = true
		}
		for id := range w.pending {
			outstanding[id] = true
		}
		for id := range w.parked {
			outstanding[id] = true
		}
		if w.upload != nil {
			outstanding[w.upload.data.ID] = true
		}
		if len(outstanding) >= sectionQueueLimit {
			return
		}
		ctx, cancel := context.WithCancelCause(w.ctx)
		select {
		case w.jobs <- sectionJob{id: id, ctx: ctx}:
			w.inflight[id] = sectionFlight{ctx: ctx, cancel: cancel}
			w.working = true
		default:
			cancel(context.Canceled)
		}
		return
	}
	if !w.working && len(w.pending) < 2 {
		w.working = true
		w.activeID = id
		ctx := context.Background()
		if w.ctx != nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(w.ctx)
			w.activeCancel = cancel
		}
		w.jobs <- sectionJob{id: id, ctx: ctx}
	}
}

// prefetch lists sections to keep, nearest first: the viewport, its neighbors
// (their vines may reach in), then lookahead in the direction of travel.
func prefetch(y float64, height float64, velocity float64) (ids []int64, required int) {
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		ids = append(ids, id)
	}
	ids = append(ids, low-1, high+1)
	required = len(ids)
	// Stationary views need their neighboring plants, but no distant lookahead.
	if velocity == 0 {
		return
	}
	ahead := 2 + min(2, int(math.Abs(velocity)/.008))
	if velocity <= 0 {
		for i := 0; i < ahead; i++ {
			ids = append(ids, high+2+int64(i))
		}
		ids = append(ids, low-2)
	} else {
		for i := 0; i < ahead; i++ {
			ids = append(ids, low-2-int64(i))
		}
		ids = append(ids, high+2)
	}
	return
}

// ensure never blocks: it queues missing sections in viewport priority and reports
// whether everything the viewport needs is already built.
func (w *world) ensure(y float64, height float64, velocity float64) bool {
	ids, required := prefetch(y, height, velocity)
	if w.priorities != nil {
		select {
		case <-w.priorities:
		default:
		}
		select {
		case w.priorities <- Viewport{Y: y, Height: height, Velocity: velocity}:
		default:
		}
	}
	ready := true
	for i, id := range ids {
		if id < 0 {
			continue
		}
		viewport := Viewport{Y: y, Height: height, Velocity: velocity}
		if data, ok := w.pending[id]; ok {
			if !meshVisible(data, viewport) {
				continue
			}
			if w.upload == nil {
				w.startUpload(data)
			}
			if i < required {
				ready = false
			}
			continue
		}
		if section := w.sections[id]; section != nil {
			if section.mesh != nil && section.needsRefresh(w.renderWidth()) && meshVisible(*section.mesh, viewport) {
				if w.upload == nil {
					w.startUpload(*section.mesh)
				}
				if i < required {
					ready = false
				}
				continue
			}
			if section.vegetationPending && i < required {
				ready = false
			}
			continue
		}
		if w.upload != nil && w.upload.data.ID == id {
			ready = ready && i >= required
			continue
		}
		w.request(id)
		if i < required {
			ready = false
		}
		if w.inflight == nil {
			break
		}
	}
	return ready
}

func (w *world) prune(y float64, height float64, velocity float64) {
	revision := w.revision
	low, high := visibleSections(y, height)
	ids, _ := prefetch(y, height, velocity)
	keep := make(map[int64]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	for id, flight := range w.inflight {
		if !keep[id] {
			flight.cancel(context.Canceled)
		}
	}
	if w.working && w.activeCancel != nil && !keep[w.activeID] {
		w.activeCancel()
	}

	for id, u := range w.parked {
		if !keep[id] {
			u.deallocate()
			delete(w.parked, id)
		}
	}
	for id, section := range w.sections {
		if w.upload != nil && w.upload.data.ID == id {
			continue
		}
		current := id >= max(0, low-2) && id <= high+3
		if !current && !keep[id] {
			section.deallocate()
			delete(w.sections, id)
			w.revision++
		}
	}
	if w.revision != revision && w.queries != nil {
		w.queries.expire(w)
	}
	for id := range w.pending {
		if !keep[id] && (id < max(0, low-2) || id > high+3) {
			delete(w.pending, id)
		}
	}
	for id := range w.collision {
		if !keep[id] && (id < max(0, low-2) || id > high+3) {
			delete(w.collision, id)
			w.collisionRevision++
		}
	}
}

func (s *worldSection) deallocate() {
	for _, img := range []*ebiten.Image{s.terrain, s.foreground, s.vines, s.foregroundVines, s.mushrooms} {
		if img != nil {
			img.Deallocate()
		}
	}
}
