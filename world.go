package infinicave

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
)

const (
	sectionHeight = W
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
}

type worldSection struct {
	terrain, vines, mushrooms, foreground *ebiten.Image
	foregroundVines                       *ebiten.Image
	geometry                              *terrainGeometry
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
	return sectionSeed(sectionSeed(seed, int64(math.Round(p.X*1000))), int64(math.Round((p.Y+top)*1000)))
}

func sectionTop(id int64) float64       { return -float64(id+1) * sectionHeight }
func sectionWindowTop(id int64) float64 { return sectionTop(id) - sectionHeight }

func worldGuides(seed, id int64) []Guide {
	var guides []Guide
	top := sectionWindowTop(id)
	proposals := make(map[int64][]Guide)
	for owner := id - 3; owner <= id+3; owner++ {
		proposals[owner] = generateGuideSection(rand.New(rand.NewSource(sectionSeed(seed, owner))))
	}
	// Include guides outside the window whose shoulders could reach inside.
	for owner := id + 2; owner >= id-2; owner-- {
		section := spacedWorldGuideSection(seed, owner, proposals)
		for i := range section {
			section[i].Seed = sectionSeed(sectionSeed(seed, owner), int64(i+1)) | 1
			section[i] = shiftedGuide(section[i], sectionTop(owner)-top)
		}
		guides = append(guides, section...)
	}
	return guides
}

// Jittered world-space sites give neighboring generation windows exactly the
// same rocks in their overlap, independent of load order or cache eviction.
func worldSeeds(seed int64, top float64, noise *Perlin) []V {
	const step = 22.0
	first := int64(math.Floor(top / step))
	last := int64(math.Ceil((top + H) / step))
	rows := make([][]V, last-first)
	parallelFor(len(rows), func(n int) {
		row := first + int64(n)
		for col := int64(0); float64(col)*step < W; col++ {
			rng := rand.New(rand.NewSource(sectionSeed(sectionSeed(seed, row), col)))
			p := V{(float64(col)+.5)*step + (rng.Float64()-.5)*step*.85,
				(float64(row)+.5)*step + (rng.Float64()-.5)*step*.85 - top}
			if p.X <= 1 || p.X >= W-1 || p.Y <= 1 || p.Y >= H-1 {
				continue
			}
			spacing := desiredSpacing(p, noise)
			if rng.Float64() < math.Min(1, step*step/(spacing*spacing)) {
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
	return buildSectionMode(seed, id, StudyNone)
}

func buildSectionMode(seed, id int64, study Study) sectionData {
	top := sectionWindowTop(id)
	backgroundNoise := NewPerlin(rand.New(rand.NewSource(seed ^ 0x62617365)))
	backgroundNoise.OffsetY = top
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	guides := worldGuides(seed, id)
	if study != StudyNone {
		guides = studyGuides(id, study)
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
	mushrooms := mushroomsForGuides(guides, insetForegroundGrid(foreground))
	var vines, foregroundVines []Vine
	if study == StudyNone {
		vines = generateVinesInBand(newVineTerrain(background, foreground), rand.New(rand.NewSource(sectionSeed(seed^0x76696e6573, id))), W, 2*W, 5)
		foregroundVines = generateForegroundVines(foreground, guides, rand.New(rand.NewSource(sectionSeed(seed^0x73757266616365, id))))
	}
	return sectionData{id: id, background: background, foreground: foreground, vines: vines, foregroundVines: foregroundVines, mushrooms: mushrooms, guides: guides}
}

func newWorld(seed int64, study Study, view View, tolerance float64) *world {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan sectionMesh, 1), done: make(chan struct{})}
	go func() {
		for {
			select {
			case <-w.done:
				return
			case id := <-w.jobs:
				data := buildSectionMode(seed, id, study)
				select {
				case <-w.done:
					return
				default:
				}
				mesh := prepareSection(data, view)
				mesh.geometry = prepareTerrainGeometry(data, tolerance)
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
		section.terrain.Deallocate()
		section.vines.Deallocate()
		if section.foregroundVines != nil {
			section.foregroundVines.Deallocate()
		}
		if section.foreground != nil {
			section.foreground.Deallocate()
		}
		if section.mushrooms != nil {
			section.mushrooms.Deallocate()
		}
		delete(w.sections, id)
	}
}

// Keep cached render targets out of the atlas: reallocation must not change
// their raster origin and introduce subpixel differences on revisiting.
func newSectionImage(height int) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, W, height), &ebiten.NewImageOptions{Unmanaged: true})
}

// Limit draw submissions per tick as well as separating the large layer
// uploads. CPU tessellation has already finished before a result arrives.
const uploadDrawsPerTick = 4

func (w *world) uploadMeshes(dst *ebiten.Image, meshes []triangleMesh) bool {
	u := w.upload
	end := min(u.next+uploadDrawsPerTick, len(meshes))
	for ; u.next < end; u.next++ {
		meshes[u.next].draw(dst, w.white)
	}
	return u.next == len(meshes)
}

// GPU resources stay on the game thread. Publish all layers together only
// after their uploads finish, so drawing never sees a partial section.
func (w *world) receive(g *Scene) {
	if w.upload == nil {
		select {
		case data := <-w.results:
			w.working = false
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
		u.img = newSectionImage(H)
		u.img.Fill(color.Black)
		g.drawGridFaces(u.img, u.data.background.faces, top)
	case 1:
		if !w.uploadMeshes(u.img, u.data.background.outlines) {
			return
		}
	case 2:
		u.foreground = newSectionImage(H)
		g.drawGridFaces(u.foreground, u.data.foreground.faces, top)
	case 3:
		if !w.uploadMeshes(u.foreground, u.data.foreground.outlines) {
			return
		}
	case 4:
		terrain := newSectionImage(sectionHeight)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(0, -sectionHeight)
		terrain.DrawImage(u.img, op)
		u.img.Deallocate()
		u.img = terrain
		foreground := newSectionImage(sectionHeight)
		foreground.DrawImage(u.foreground, op)
		u.foreground.Deallocate()
		u.foreground = foreground
	case 5:
		if u.vines == nil {
			u.vines = newSectionImage(H)
		}
		if !w.uploadMeshes(u.vines, u.data.vines) {
			return
		}
	case 6:
		if len(u.data.vines) > 0 && g.vineMaterial != nil {
			softened := newSectionImage(H)
			softened.DrawRectShader(W, H, g.vineMaterial, &ebiten.DrawRectShaderOptions{
				Images: [4]*ebiten.Image{u.vines},
				Uniforms: map[string]any{
					"Offset":  []float32{0, float32(top)},
					"Texture": float32(g.texture / 8),
				},
			})
			u.vines.Deallocate()
			u.vines = softened
		}
		if g.view == ViewShaded {
			u.mushrooms = newSectionImage(H)
			u.data.mushrooms.draw(u.mushrooms, w.white)
		}
	case 7:
		if len(u.data.foregroundVines) > 0 {
			if u.foregroundVines == nil {
				u.foregroundVines = newSectionImage(H)
			}
			if !w.uploadMeshes(u.foregroundVines, u.data.foregroundVines) {
				return
			}
		}
	default:
		w.sections[u.data.id] = &worldSection{terrain: u.img, vines: u.vines, mushrooms: u.mushrooms, foreground: u.foreground, foregroundVines: u.foregroundVines, geometry: u.data.geometry}
		w.revision++
		w.upload = nil
		return
	}
	u.stage++
	u.next = 0
}

func visibleSections(y float64, height int) (low, high int64) {
	low = max(0, int64(math.Floor(-(y+float64(height))/sectionHeight)))
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
func prefetch(y float64, height int, velocity float64) (ids []int64, required int) {
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		ids = append(ids, id)
	}
	ids = append(ids, low-1, high+1)
	required = len(ids)
	ahead := 2 + min(2, int(math.Abs(velocity)/8))
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
func (w *world) ensure(y float64, height int, velocity float64) bool {
	ids, required := prefetch(y, height, velocity)
	ready := true
	for i, id := range ids {
		if id < 0 || w.sections[id] != nil {
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

func (w *world) prune(y float64, height int, velocity float64) {
	low, high := visibleSections(y, height)
	ids, _ := prefetch(y, height, velocity)
	keep := make(map[int64]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	for id, section := range w.sections {
		current := id >= max(0, low-2) && id <= high+3
		if !current && !keep[id] {
			section.terrain.Deallocate()
			section.vines.Deallocate()
			if section.foregroundVines != nil {
				section.foregroundVines.Deallocate()
			}
			if section.foreground != nil {
				section.foreground.Deallocate()
			}
			if section.mushrooms != nil {
				section.mushrooms.Deallocate()
			}
			delete(w.sections, id)
			w.revision++
		}
	}
}

func (w *world) draw(dst *ebiten.Image, y float64, height int) {
	y = math.Round(y)
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, sectionTop(id)-y)
			dst.DrawImage(section.terrain, op)
		}
	}
	// A vine is drawn once in world coordinates, even while crossing a seam.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, sectionWindowTop(id)-y)
			dst.DrawImage(section.vines, op)
		}
	}
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil && section.mushrooms != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, sectionWindowTop(id)-y)
			dst.DrawImage(section.mushrooms, op)
		}
	}
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, sectionTop(id)-y)
			dst.DrawImage(section.foreground, op)
		}
	}
	// Keep complete foreground stems in their owner's padded window, just
	// like background vines, so scrolling across a section cannot cut them.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil && section.foregroundVines != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(0, sectionWindowTop(id)-y)
			dst.DrawImage(section.foregroundVines, op)
		}
	}
}
