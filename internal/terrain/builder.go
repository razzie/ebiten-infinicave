package terrain

import (
	"context"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Builder retains reusable generation workspaces and a bounded guide cache.
// A builder has one caller at a time; concurrent sections use separate builders.
type Builder struct {
	seed        int64
	loadSection SectionLoader
	guides      *guideCache
	fields      vineWorkspace
	frontFields vineWorkspace
}

func NewSectionBuilder(seed int64, loadSection SectionLoader, orientation ...Orientation) *Builder {
	return &Builder{seed: seed, loadSection: loadSection, guides: newGuideCache(seed, orientation...)}
}

func (b *Builder) Build(id int64) SectionData {
	return b.BuildWithTerrain(id, nil)
}

// onTerrain runs before background, material, or decoration, once foreground
// topology includes authored holes. Its opaque shape snapshot remains read-only.
func (b *Builder) BuildWithTerrain(id int64, onTerrain func(SectionData)) SectionData {
	data, _ := b.BuildWithContext(context.Background(), id, onTerrain)
	return data
}

// BuildWithContext checks cancellation between generation stages. A canceled
// build returns no section and never publishes partial vegetation.
func (b *Builder) BuildWithContext(ctx context.Context, id int64, onTerrain func(SectionData)) (SectionData, bool) {
	s := b.Begin(ctx, id)
	if s == nil {
		return SectionData{}, false
	}
	if onTerrain != nil {
		onTerrain(s.Data())
	}
	if !s.Materialize(ctx) || !b.Decorate(ctx, s) {
		return SectionData{}, false
	}
	return s.Data(), true
}

// Each section is generated with a full section of padding on either side.
// Rocks use world-space seeds; vines belong to one section but retain their
// complete geometry across its neighbors, so section edges cannot cut a fork.
type SectionData struct {
	Orientation            Orientation
	ID                     int64
	Background, Foreground RockGrid
	Vines                  []Vine
	ForegroundVines        []Vine
	Mushrooms              []MushroomGroup
	Guides                 []Guide
	Holes                  []Hole
	vegetationCuts         []RockCut
	ForegroundTopology     *RockTopology
	BackgroundNeighbors    [][]int
}

func BuildSection(seed, id int64) SectionData {
	return NewSectionBuilder(seed, nil).Build(id)
}

func (b *Builder) Begin(ctx context.Context, id int64) *SectionBuild {
	seed, loadSection, guideCache := b.seed, b.loadSection, b.guides
	if ctx.Err() != nil {
		return nil
	}
	orientation := guideCache.orientation
	top := SectionTop(id)
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	var guides []Guide
	var holes []Hole
	if loadSection != nil {
		content := loadedWorldContent(seed, id, loadSection)
		guides, holes = content.Guides, content.Holes
	} else {
		guides = guideCache.window(id)
	}
	if ctx.Err() != nil {
		return nil
	}
	for i := range guides {
		guides[i].prepareProjection()
	}
	seeds := artisticRockSeeds(worldSeeds(seed, top, noise), guides, seed, top)
	if ctx.Err() != nil {
		return nil
	}
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(seed))))
	foreground := guideRockFaces(seeds, guides)
	if ctx.Err() != nil {
		return nil
	}
	shapeReliefGrid(foreground, guides, noise, branches)
	if ctx.Err() != nil {
		return nil
	}
	foreground = contourRockGrid(foreground, func(p geom.V) float64 { return reliefHeight(p, guides, noise, branches) })
	if ctx.Err() != nil {
		return nil
	}
	polishRockContours(foreground, guides, func(p geom.V) float64 { return reliefHeight(p, guides, noise, branches) })

	if ctx.Err() != nil {
		return nil
	}
	// Occupancy is independent of material brightness. Initialize metadata
	// before taking the immutable topology snapshot used by early collision.
	for i := range foreground {
		foreground[i].Color.A = 255
		foreground[i].orientation, foreground[i].worldTop = orientation, top
	}
	inset, parents := insetForegroundGrid(foreground, true)
	topology := &RockTopology{Grid: inset, neighbors: RockNeighbors(inset)}
	topology.prepareBoundary()
	s := &SectionBuild{seed: seed, raw: foreground, uncut: topology, uncutParents: parents}
	s.data = SectionData{Orientation: orientation, ID: id, Foreground: foreground, Guides: guides, Holes: holes}
	for _, hole := range holes {
		if ctx.Err() != nil {
			return nil
		}
		cut, err := CutFromHole(TranslateHoleY(hole, top))
		if err != nil {
			continue
		}
		grid, source, changed := cut.grid(topology.Grid, top)
		if changed {
			mapped := make([]int, len(source))
			for i, parent := range source {
				mapped[i] = parents[parent]
			}
			parents = mapped
			topology = CarvedTopology(grid, append(append([]RockCut(nil), topology.Cuts...), cut), top)
		}
		s.cuts = append(s.cuts, sectionCutStage{cut: cut, grid: topology.Grid, parents: parents})
	}
	s.data.ForegroundTopology = topology
	s.parents = parents
	if len(topology.Cuts) > 0 {
		s.data.Foreground = topology.Grid
	}
	return s
}
