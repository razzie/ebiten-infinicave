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
}

type worldSection struct {
	terrain, vines *ebiten.Image
}

type World struct {
	sections map[int64]*worldSection
	jobs     chan int64
	results  chan sectionData
	done     chan struct{}
	closing  sync.Once
	working  bool
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
	// Include guides outside the window whose shoulders could reach inside.
	for owner := id + 2; owner >= id-2; owner-- {
		rng := rand.New(rand.NewSource(sectionSeed(seed, owner)))
		section := generateGuideSection(rng)
		for i := range section {
			section[i].Seed = sectionSeed(sectionSeed(seed, owner), int64(i+1)) | 1
			section[i].translateY(sectionTop(owner) - top)
		}
		guides = append(guides, section...)
	}
	return guides
}

// Jittered world-space sites give neighboring generation windows exactly the
// same rocks in their overlap, independent of load order or cache eviction.
func worldSeeds(seed int64, top float64, guides []Guide, noise *Perlin) []V {
	const step = 32.0
	var seeds []V
	first := int64(math.Floor(top / step))
	last := int64(math.Ceil((top + H) / step))
	for row := first; row < last; row++ {
		for col := int64(0); float64(col)*step < W; col++ {
			rng := rand.New(rand.NewSource(sectionSeed(sectionSeed(seed, row), col)))
			p := V{(float64(col)+.5)*step + (rng.Float64()-.5)*step*.85,
				(float64(row)+.5)*step + (rng.Float64()-.5)*step*.85 - top}
			if p.X <= 1 || p.X >= W-1 || p.Y <= 1 || p.Y >= H-1 {
				continue
			}
			spacing := desiredSpacing(p, guides, noise)
			if rng.Float64() < math.Min(1, step*step/(spacing*spacing)) {
				seeds = append(seeds, p)
			}
		}
	}
	return seeds
}

func buildSection(seed, id int64) sectionData {
	top := sectionWindowTop(id)
	backgroundNoise := NewPerlin(rand.New(rand.NewSource(seed ^ 0x62617365)))
	backgroundNoise.OffsetY = top
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	guides := worldGuides(seed, id)
	backgroundSeeds := worldSeeds(seed^0x62617365, top, nil, backgroundNoise)
	seeds := worldSeeds(seed, top, guides, noise)
	warpSeeds(seeds, guides)
	// The paired crest and independent rows use each guide's own stable stream.
	seeds = addGuideSeeds(seeds, guides, rand.New(rand.NewSource(seed)))
	branches := generateBranches(guides, noise, rand.New(rand.NewSource(seed)))
	background := newRockGrid(backgroundSeeds, func(p V) color.NRGBA { return backgroundCellColor(p, backgroundNoise) })
	foreground := newRockGrid(seeds, func(p V) color.NRGBA { return guideCellColor(p, guides, noise, branches) })
	vines := generateVinesInBand(newVineTerrain(background, foreground), rand.New(rand.NewSource(sectionSeed(seed^0x76696e6573, id))), W, 2*W, 5)
	return sectionData{id, background, foreground, vines}
}

func newWorld(seed int64) *World {
	w := &World{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan sectionData, 1), done: make(chan struct{})}
	go func() {
		for {
			select {
			case <-w.done:
				return
			case id := <-w.jobs:
				data := buildSection(seed, id)
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
	for id, section := range w.sections {
		section.terrain.Deallocate()
		section.vines.Deallocate()
		delete(w.sections, id)
	}
}

// Keep cached render targets out of the atlas: reallocation must not change
// their raster origin and introduce subpixel differences on revisiting.
func newSectionImage(height int) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, W, height), &ebiten.NewImageOptions{Unmanaged: true})
}

// GPU work stays on the game thread; the worker only builds CPU geometry.
func (w *World) receive(g *Game) {
	select {
	case data := <-w.results:
		w.working = false
		img := newSectionImage(H)
		img.Fill(color.Black)
		top := sectionWindowTop(data.id)
		g.drawGrid(img, data.background, top)
		g.drawGrid(img, data.foreground, top)
		terrain := newSectionImage(sectionHeight)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(0, -sectionHeight)
		terrain.DrawImage(img, op)
		img.Deallocate()
		vines := newSectionImage(H)
		drawVines(vines, data.vines)
		w.sections[data.id] = &worldSection{terrain, vines}
	default:
	}
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

func (w *World) ensure(y float64, height int) bool {
	low, high := visibleSections(y, height)
	// Neighbor-owned vines may extend into the viewport from either side.
	for id := max(0, low-1); id <= high+1; id++ {
		if w.sections[id] == nil {
			w.request(id)
			return false
		}
	}
	// Prefetch in the growth direction, with one section behind for returning.
	for _, id := range []int64{high + 2, high + 3, low - 2} {
		if id >= 0 && w.sections[id] == nil {
			w.request(id)
			break
		}
	}
	return true
}

func (w *World) prune(y, target float64, height int) {
	low, high := visibleSections(y, height)
	targetLow, targetHigh := visibleSections(target, height)
	for id, section := range w.sections {
		current := id >= max(0, low-2) && id <= high+3
		requested := id >= max(0, targetLow-2) && id <= targetHigh+3
		if !current && !requested {
			section.terrain.Deallocate()
			section.vines.Deallocate()
			delete(w.sections, id)
		}
	}
}

func (w *World) draw(dst *ebiten.Image, y float64, height int) {
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
}
