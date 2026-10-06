package infinicave

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

const (
	sectionHeight = generationWidth
)

// Each section is generated with a full section of padding on either side.
// Rocks use world-space seeds; vines belong to one section but retain their
// complete geometry across its neighbors, so section edges cannot cut a fork.
type sectionData struct {
	id                     int64
	background, foreground RockGrid
	vines                  []Vine
	foregroundVines        []Vine
	mushrooms              []MushroomGroup
	guides                 []Guide
	holes                  []Hole
	vegetationCuts         []rockCut
	foregroundTopology     *rockTopology
}

type worldSection struct {
	terrain, vines, mushrooms, foreground               *ebiten.Image
	foregroundVines                                     *ebiten.Image
	geometry                                            *terrainGeometry
	vinesBounds, foregroundVinesBounds, mushroomsBounds image.Rectangle
	vegetationPending                                   bool
}

type world struct {
	sections map[int64]*worldSection
	jobs     chan int64
	results  chan sectionMesh
	done     chan struct{}
	closing  sync.Once
	working  bool
	upload   *sectionUpload
	white    *ebiten.Image
	revision uint64 // invalidates hover overlays when cached sections change
	queries  *worldQueryIndex
	cuts     []rockCut // world-space edits survive section eviction
}

// sectionUpload spreads one section's GPU work over several frames.
type sectionUpload struct {
	data            sectionMesh
	img             *ebiten.Image
	foreground      *ebiten.Image
	vines           *ebiten.Image
	foregroundVines *ebiten.Image
	mushrooms       *ebiten.Image
	stage           int
	next            int
}

func sectionSeed(seed, id int64) int64 {
	x := uint64(seed) + uint64(id)*0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return int64(x ^ (x >> 31))
}

func cellSeed(seed int64, p V, top float64) int64 {
	return sectionSeed(sectionSeed(seed, int64(math.Round(p.X*1e6))), int64(math.Round((p.Y+top)*1e6)))
}

func sectionTop(id int64) float64       { return -float64(id+1) * sectionHeight }
func sectionWindowTop(id int64) float64 { return sectionTop(id) - sectionHeight }

func worldGuides(seed, id int64) []Guide {
	return newGuideCache(seed).window(id)
}

// Jittered world-space sites give neighboring generation windows exactly the
// same rocks in their overlap, independent of load order or cache eviction.
func worldSeeds(seed int64, top float64, noise *Perlin) []V {
	const step = .022
	first := int64(math.Floor((top + generationMinY) / step))
	last := int64(math.Ceil((top + generationMaxY) / step))
	rows := make([][]V, last-first)
	parallelFor(len(rows), func(n int) {
		row := first + int64(n)
		for col := int64(0); float64(col)*step < generationWidth; col++ {
			p := V{(float64(col)+.5)*step + (siteRandom(seed, row, col, 0)-.5)*step*.85,
				(float64(row)+.5)*step + (siteRandom(seed, row, col, 1)-.5)*step*.85 - top}
			if p.X <= .001 || p.X >= generationWidth-.001 || p.Y <= generationMinY+.001 || p.Y >= generationMaxY-.001 {
				continue
			}
			spacing := desiredSpacing(p, noise)
			if siteRandom(seed, row, col, 2) < min(1, step*step/(spacing*spacing)) {
				rows[n] = append(rows[n], p)
			}
		}
	})
	var seeds []V
	for _, r := range rows {
		seeds = append(seeds, r...)
	}
	return seeds
}

func buildSection(seed, id int64) sectionData {
	return buildSectionMode(seed, id, StudyNone, nil)
}

func buildSectionMode(seed, id int64, study Study, loadSection SectionLoader) sectionData {
	return newSectionBuilder(seed, study, loadSection).build(id)
}

func buildSectionCached(seed, id int64, study Study, loadSection SectionLoader, guideCache *guideCache, fields *vineWorkspace) sectionData {
	top := sectionTop(id)
	backgroundNoise := NewPerlin(rand.New(rand.NewSource(seed ^ 0x62617365)))
	backgroundNoise.OffsetY = top
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	var guides []Guide
	var holes []Hole
	if loadSection != nil {
		content := loadedWorldContent(seed, id, loadSection)
		guides, holes = content.Guides, content.Holes
	} else if study != StudyNone {
		guides = studyGuides(id, study)
	} else {
		guides = guideCache.window(id)
	}
	backgroundSeeds := worldSeeds(seed^0x62617365, top, backgroundNoise)
	seeds := artisticRockSeeds(worldSeeds(seed, top, noise), guides, seed, top)
	var branches *BranchField
	if study == StudyNone {
		branches = newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(seed))))
	}
	background := newRockGrid(backgroundSeeds, func(V) color.NRGBA { return color.NRGBA{A: 255} })
	foreground := guideRockFaces(seeds, guides)
	shapeRockGrid(background, nil, backgroundNoise)
	shapeReliefGrid(foreground, guides, noise, branches)
	foreground = contourRockGrid(foreground, func(p V) float64 {
		return reliefHeight(p, guides, noise, branches)
	})
	polishRockContours(foreground, guides, func(p V) float64 {
		return reliefHeight(p, guides, noise, branches)
	})
	shadeRockGrids(background, foreground, backgroundNoise)
	topology := newRockTopology(foreground)
	mushrooms := mushroomsForGuides(guides, topology.grid)
	var vines, foregroundVines []Vine
	if study == StudyNone {
		field := newVineTerrainWithWorkspace(background, foreground, false, fields)
		vines = generateVinesInBand(field, rand.New(rand.NewSource(sectionSeed(seed^0x76696e6573, id))), 0, SectionHeight, 5)
		field.release()
		foregroundVines = generateForegroundVinesWithWorkspace(foreground, guides, rand.New(rand.NewSource(sectionSeed(seed^0x73757266616365, id))), fields)
	}
	plants := vegetationGeometry{vines: vines, foregroundVines: foregroundVines, mushrooms: mushrooms}
	for _, hole := range holes {
		cut, err := hole.translatedY(top).rockCut()
		if err != nil {
			continue
		}
		grid, _, changed := cut.grid(topology.grid, top)
		if changed {
			cuts := append(append([]rockCut(nil), topology.cuts...), cut)
			topology = carvedTopology(grid, cuts, top)
		}
		plants, _ = cut.vegetation(plants, topology.grid, top)
	}
	if len(topology.cuts) > 0 {
		foreground = topology.grid
	}
	return sectionData{id: id, background: background, foreground: foreground, vines: plants.vines, foregroundVines: plants.foregroundVines, mushrooms: plants.mushrooms, guides: guides, holes: holes, vegetationCuts: plants.cuts, foregroundTopology: topology}
}

func newWorld(seed int64, study Study, view View, tolerance float64, loadSection SectionLoader) *world {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan sectionMesh, 1), done: make(chan struct{})}
	go func() {
		builder := newSectionBuilder(seed, study, loadSection)
		for {
			select {
			case <-w.done:
				return
			case id := <-w.jobs:
				data := builder.build(id)
				select {
				case <-w.done:
					return
				default:
				}
				mesh := prepareSection(data, view)
				mesh.geometry = prepareTerrainGeometry(data, tolerance)
				finishSectionMesh(&mesh)
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
	if w.white != nil {
		w.white.Deallocate()
		w.white = nil
	}
	for id, section := range w.sections {
		section.deallocate()
		delete(w.sections, id)
	}
}

// Keep cached render targets out of the atlas: reallocation must not change
// their raster origin and introduce subpixel differences on revisiting.
func newSectionImage(height int) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, rasterPixelsPerUnit, height*rasterPixelsPerUnit), &ebiten.NewImageOptions{Unmanaged: true})
}

// Limit draw submissions per tick as well as separating the large layer
// uploads. CPU tessellation has already finished before a result arrives.
const uploadDrawsPerTick = 16
const uploadTimePerTick = 2 * time.Millisecond
const uploadIndicesPerTick = 128 * 1024

func (w *world) uploadMeshes(dst *ebiten.Image, meshes []triangleMesh) bool {
	u := w.upload
	end := min(u.next+uploadDrawsPerTick, len(meshes))
	start := time.Now()
	indices := 0
	for ; u.next < end; u.next++ {
		// Always make progress, even when a single batch exceeds the budget.
		if indices > 0 && (indices+len(meshes[u.next].indices) > uploadIndicesPerTick || time.Since(start) >= uploadTimePerTick) {
			break
		}
		meshes[u.next].draw(dst, w.white)
		indices += len(meshes[u.next].indices)
	}
	return u.next == len(meshes)
}

// GPU resources stay on the game thread. Publish complete terrain/collision
// first, then add vegetation together once all its layers have finished.
func (w *world) receive(g *Scene) {
	if w.upload == nil {
		select {
		case data := <-w.results:
			w.working = false
			g.applyStoredCuts(&data)
			w.upload = &sectionUpload{data: data}
		default:
		}
		return
	}
	u := w.upload
	top := sectionWindowTop(u.data.id)
	if w.white == nil {
		w.white = ebiten.NewImage(1, 1)
		w.white.Fill(color.White)
	}
	switch u.stage {
	case 0:
		u.img = newSectionImage(generationHeight)
		u.img.Fill(color.Black)
		g.drawGridFaces(u.img, u.data.background.faces, top)
	case 1:
		if !w.uploadMeshes(u.img, u.data.background.outlines) {
			return
		}
	case 2:
		u.foreground = newSectionImage(generationHeight)
		g.drawGridFaces(u.foreground, u.data.foreground.faces, top)
	case 3:
		if !w.uploadMeshes(u.foreground, u.data.foreground.outlines) {
			return
		}
	case 4:
		terrain := newSectionImage(sectionHeight)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(0, -sectionHeight*rasterPixelsPerUnit)
		terrain.DrawImage(u.img, op)
		u.img.Deallocate()
		u.img = terrain
		foreground := newSectionImage(sectionHeight)
		foreground.DrawImage(u.foreground, op)
		u.foreground.Deallocate()
		u.foreground = foreground
		w.sections[u.data.id] = &worldSection{terrain: u.img, foreground: u.foreground,
			geometry: u.data.geometry, vegetationPending: true}
		u.img, u.foreground = nil, nil // ownership moved to the cache
		w.revision++
	case 5:
		if u.data.vinesBounds.Empty() {
			break
		}
		if u.vines == nil {
			u.vines = newVegetationImage(u.data.vinesBounds)
		}
		if !w.uploadMeshes(u.vines, u.data.vines) {
			return
		}
	case 6:
		u.vines = g.softenVines(u.vines, u.data.vinesBounds, top)
		if u.data.geometry != nil {
			eraseVineCuts(u.vines, u.data.vinesBounds, top, u.data.geometry.vegetation.cuts)
		}
		if !u.data.mushroomsBounds.Empty() {
			u.mushrooms = newVegetationImage(u.data.mushroomsBounds)
			u.data.mushrooms.draw(u.mushrooms, w.white)
		}
	case 7:
		if !u.data.foregroundVinesBounds.Empty() {
			if u.foregroundVines == nil {
				u.foregroundVines = newVegetationImage(u.data.foregroundVinesBounds)
			}
			if !w.uploadMeshes(u.foregroundVines, u.data.foregroundVines) {
				return
			}
		}
		if u.data.geometry != nil {
			eraseVineCuts(u.foregroundVines, u.data.foregroundVinesBounds, top, u.data.geometry.vegetation.cuts)
		}
	default:
		section := w.sections[u.data.id]
		section.vines, section.foregroundVines, section.mushrooms = u.vines, u.foregroundVines, u.mushrooms
		section.vinesBounds, section.foregroundVinesBounds, section.mushroomsBounds = u.data.vinesBounds, u.data.foregroundVinesBounds, u.data.mushroomsBounds
		section.vegetationPending = false
		w.upload = nil
		return
	}
	u.stage++
	u.next = 0
}

func visibleSections(y float64, height float64) (low, high int64) {
	low = max(0, int64(math.Floor(-(y+height)/sectionHeight)))
	high = max(low, int64(math.Ceil(-y/sectionHeight))-1)
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
		if section := w.sections[id]; section != nil {
			if section.vegetationPending && i < required {
				ready = false
			}
			continue
		}
		if w.upload != nil && w.upload.data.id == id {
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
		if w.upload != nil && w.upload.data.id == id {
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
}

func (w *world) draw(dst *ebiten.Image, y float64, height float64) {
	scale := float64(dst.Bounds().Dx()) / rasterPixelsPerUnit
	y = rasterAlignedY(y)
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, (sectionTop(id)-y)*rasterPixelsPerUnit)
			op.GeoM.Scale(scale, scale)
			dst.DrawImage(section.terrain, op)
		}
	}
	// A vine is drawn once in world coordinates, even while crossing a seam.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			drawVegetation(dst, section.vines, section.vinesBounds, sectionWindowTop(id), y, scale)
		}
	}
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			drawVegetation(dst, section.mushrooms, section.mushroomsBounds, sectionWindowTop(id), y, scale)
		}
	}
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, (sectionTop(id)-y)*rasterPixelsPerUnit)
			op.GeoM.Scale(scale, scale)
			dst.DrawImage(section.foreground, op)
		}
	}
	// Keep complete foreground stems in their owner's padded window.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			drawVegetation(dst, section.foregroundVines, section.foregroundVinesBounds, sectionWindowTop(id), y, scale)
		}
	}
}

func drawVegetation(dst, layer *ebiten.Image, bounds image.Rectangle, top, y, scale float64) {
	if layer == nil {
		return
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(bounds.Min.X), (top-y)*rasterPixelsPerUnit+float64(bounds.Min.Y))
	op.GeoM.Scale(scale, scale)
	dst.DrawImage(layer, op)
}

func newVegetationImage(bounds image.Rectangle) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), &ebiten.NewImageOptions{Unmanaged: true})
}

func (s *worldSection) deallocate() {
	for _, img := range []*ebiten.Image{s.terrain, s.foreground, s.vines, s.foregroundVines, s.mushrooms} {
		if img != nil {
			img.Deallocate()
		}
	}
}
