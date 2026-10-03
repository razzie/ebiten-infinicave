package main

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
	exportHeight  = 2400
)

// Each section is generated with a full section of padding on either side.
// Rocks use world-space seeds; vines belong to one section but retain their
// complete geometry across its neighbors, so section edges cannot cut a fork.
type sectionData struct {
	id                     int64
	background, foreground RockGrid
	vines                  []Vine
	mushrooms              []MushroomGroup
}

type worldSection struct {
	terrain, vines, mushrooms, foreground *ebiten.Image
}

type World struct {
	sections map[int64]*worldSection
	jobs     chan int64
	results  chan sectionData
	done     chan struct{}
	closing  sync.Once
	working  bool
	upload   *sectionUpload
}

// sectionUpload spreads one section's GPU work over several frames.
type sectionUpload struct {
	data       sectionData
	img        *ebiten.Image
	foreground *ebiten.Image
	stage      int
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
	return buildSectionMode(seed, id, "")
}

func buildSectionMode(seed, id int64, study string) sectionData {
	top := sectionWindowTop(id)
	backgroundNoise := NewPerlin(rand.New(rand.NewSource(seed ^ 0x62617365)))
	backgroundNoise.OffsetY = top
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	guides := worldGuides(seed, id)
	if study != "" {
		guides = studyGuides(id, study)
	}
	backgroundSeeds := worldSeeds(seed^0x62617365, top, backgroundNoise)
	seeds := artisticRockSeeds(worldSeeds(seed, top, noise), guides, seed, top)
	var branches *BranchField
	if study == "" {
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
	var vines []Vine
	if study == "" {
		vines = generateVinesInBand(newVineTerrain(background, foreground), rand.New(rand.NewSource(sectionSeed(seed^0x76696e6573, id))), W, 2*W, 5)
	}
	return sectionData{id, background, foreground, vines, mushrooms}
}

func newWorld(seed int64, study string) *World {
	w := &World{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan sectionData, 1), done: make(chan struct{})}
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
				case w.results <- data:
				}
			}
		}
	}()
	return w
}

func (w *World) close() {
	w.closing.Do(func() { close(w.done) })
	if w.upload != nil && w.upload.img != nil {
		w.upload.img.Deallocate()
	}
	if w.upload != nil && w.upload.foreground != nil {
		w.upload.foreground.Deallocate()
	}
	w.upload = nil
	for id, section := range w.sections {
		section.terrain.Deallocate()
		section.vines.Deallocate()
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

// GPU work stays on the game thread; the worker only builds CPU geometry.
// One stage runs per frame so a finished section never stalls scrolling.
func (w *World) receive(g *Game) {
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
	shaded := g.view == "shaded" || g.view == ""
	top := sectionWindowTop(u.data.id)
	switch u.stage {
	case 0:
		u.img = newSectionImage(H)
		u.img.Fill(color.Black)
		g.drawGrid(u.img, u.data.background, top)
	case 1:
		u.foreground = newSectionImage(H)
		g.drawGrid(u.foreground, u.data.foreground, top)
	case 2:
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
	default:
		vines := newSectionImage(H)
		if shaded {
			drawVines(vines, u.data.vines)
		}
		var mushrooms *ebiten.Image
		if shaded {
			mushrooms = newSectionImage(H)
			drawMushrooms(mushrooms, u.data.mushrooms)
		}
		w.sections[u.data.id] = &worldSection{terrain: u.img, vines: vines, mushrooms: mushrooms, foreground: u.foreground}
		w.upload = nil
		return
	}
	u.stage++
}

func visibleSections(y float64, height int) (low, high int64) {
	low = max(0, int64(math.Floor(-(y+float64(height))/sectionHeight)))
	high = max(low, int64(math.Ceil(-y/sectionHeight))-1)
	return
}

func (w *World) request(id int64) {
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
func (w *World) ensure(y float64, height int, velocity float64) bool {
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

func (w *World) prune(y float64, height int, velocity float64) {
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
			if section.foreground != nil {
				section.foreground.Deallocate()
			}
			if section.mushrooms != nil {
				section.mushrooms.Deallocate()
			}
			delete(w.sections, id)
		}
	}
}

func (w *World) draw(dst *ebiten.Image, y float64, height int) {
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
}
