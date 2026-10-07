package infinicave

import (
	"image"
	"math"
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
}

type world struct {
	sections map[int64]*worldSection
	jobs     chan int64
	results  chan render.SectionMesh
	terrain  chan sectionTerrain
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
}

type sectionTerrain struct {
	id       int64
	geometry *terrain.Geometry
}

func newWorld(seed int64, view View, tolerance float64, loadSection SectionLoader, orientation ...Orientation) *world {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan render.SectionMesh, 1), terrain: make(chan sectionTerrain, 1), done: make(chan struct{})}
	go func() {
		builder := terrain.NewSectionBuilder(seed, loadSection, orientation...)
		for {
			select {
			case <-w.done:
				return
			case id := <-w.jobs:
				var geometry *terrain.Geometry
				data := builder.BuildWithTerrain(id, func(data terrain.SectionData) {
					geometry = terrain.PrepareTerrainGeometry(data, tolerance)
					select {
					case <-w.done:
					case w.terrain <- sectionTerrain{id: id, geometry: geometry}:
					}
				})
				select {
				case <-w.done:
					return
				default:
				}
				mesh := render.PrepareSection(data, view)
				// The early geometry is already owned by the game loop. Add vegetation
				// to a separate value without mutating its published topology.
				complete := *geometry
				complete.Vegetation = terrain.SectionVegetation(data)
				mesh.Geometry = &complete
				render.FinishSectionMesh(&mesh)
				select {
				case <-w.done:
					return
				case w.results <- mesh:
				}
			}
		}
	}()
	return w
}

func (w *world) close() {
	w.closing.Do(func() { close(w.done) })
	if w.upload != nil && w.upload.img != nil {
		w.upload.img.Deallocate()
	}
	if w.upload != nil && w.upload.foreground != nil {
		w.upload.foreground.Deallocate()
	}
	if w.upload != nil && w.upload.vines != nil {
		w.upload.vines.Deallocate()
	}
	if w.upload != nil && w.upload.foregroundVines != nil {
		w.upload.foregroundVines.Deallocate()
	}
	if w.upload != nil && w.upload.mushrooms != nil {
		w.upload.mushrooms.Deallocate()
	}
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
	if !w.working {
		w.working = true
		w.jobs <- id
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

// ensure never blocks: it queues the most useful missing section and reports
// whether everything the viewport needs is already built.
func (w *world) ensure(y float64, height float64, velocity float64) bool {
	ids, required := prefetch(y, height, velocity)
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
			if section.mesh != nil && section.renderWidth() != w.renderWidth() && meshVisible(*section.mesh, viewport) {
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
		break
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
