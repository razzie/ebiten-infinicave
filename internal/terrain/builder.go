package terrain

import (
	"context"
	"image/color"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
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

// onTerrain runs before decoration, once foreground topology includes all
// authored holes. Published terrain is read-only for the rest of the build.
func (b *Builder) BuildWithTerrain(id int64, onTerrain func(SectionData)) SectionData {
	data, _ := b.BuildWithContext(context.Background(), id, onTerrain)
	return data
}

// BuildWithContext checks cancellation between generation stages. A canceled
// build returns no section and never publishes partial vegetation.
func (b *Builder) BuildWithContext(ctx context.Context, id int64, onTerrain func(SectionData)) (SectionData, bool) {
	data := buildSectionCached(ctx, b.seed, id, b.loadSection, b.guides, &b.fields, &b.frontFields, onTerrain)
	return data, ctx.Err() == nil
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

func buildSectionCached(ctx context.Context, seed, id int64, loadSection SectionLoader, guideCache *guideCache, fields, frontFields *vineWorkspace, onTerrain func(SectionData)) SectionData {
	if ctx.Err() != nil {
		return SectionData{}
	}
	orientation := guideCache.orientation
	top := SectionTop(id)
	backgroundNoise := NewPerlin(rand.New(rand.NewSource(seed ^ 0x62617365)))
	backgroundNoise.OffsetY = top
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
		return SectionData{}
	}
	for i := range guides {
		guides[i].prepareProjection()
	}
	var background RockGrid
	var backgroundNeighbors [][]int
	joinBackground := parallel.Start(func() {
		seeds := worldSeedsInRange(seed^0x62617365, top, backgroundNoise, BackgroundMinX, BackgroundMaxX)
		if ctx.Err() != nil {
			return
		}
		background = newRockGridInRange(seeds, func(geom.V) color.NRGBA { return color.NRGBA{A: 255} }, BackgroundMinX, BackgroundMaxX)
		if ctx.Err() != nil {
			return
		}
		backgroundNeighbors = shapeRockGrid(background, nil, backgroundNoise)
	})
	seeds := artisticRockSeeds(worldSeeds(seed, top, noise), guides, seed, top)
	if ctx.Err() != nil {
		joinBackground()
		return SectionData{}
	}
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(seed))))
	foreground := guideRockFaces(seeds, guides)
	if ctx.Err() != nil {
		joinBackground()
		return SectionData{}
	}
	shapeReliefGrid(foreground, guides, noise, branches)
	if ctx.Err() != nil {
		joinBackground()
		return SectionData{}
	}
	foreground = contourRockGrid(foreground, func(p geom.V) float64 { return reliefHeight(p, guides, noise, branches) })
	if ctx.Err() != nil {
		joinBackground()
		return SectionData{}
	}
	polishRockContours(foreground, guides, func(p geom.V) float64 { return reliefHeight(p, guides, noise, branches) })
	joinBackground()
	if ctx.Err() != nil {
		return SectionData{}
	}
	shadeRockGrids(background, foreground, backgroundNoise, orientation)
	if ctx.Err() != nil {
		return SectionData{}
	}
	topology := newRockTopology(foreground)
	vegetationGrid := topology.Grid
	// Retain the terrain after each cut so vegetation keeps the same sequential
	// damage semantics while collision can be published before it is generated.
	type cutStage struct {
		cut  RockCut
		grid RockGrid
	}
	var cuts []cutStage
	for _, hole := range holes {
		if ctx.Err() != nil {
			return SectionData{}
		}
		cut, err := CutFromHole(TranslateHoleY(hole, top))
		if err != nil {
			continue
		}
		grid, _, changed := cut.grid(topology.Grid, top)
		if changed {
			topologyCuts := append(append([]RockCut(nil), topology.Cuts...), cut)
			topology = CarvedTopology(grid, topologyCuts, top)
		}
		cuts = append(cuts, cutStage{cut, topology.Grid})
	}
	data := SectionData{Orientation: orientation, ID: id, Background: background, Foreground: foreground, Guides: guides, Holes: holes, ForegroundTopology: topology, BackgroundNeighbors: backgroundNeighbors}
	if len(topology.Cuts) > 0 {
		data.Foreground = topology.Grid
	}
	if onTerrain != nil {
		onTerrain(data)
	}
	if ctx.Err() != nil {
		return SectionData{}
	}
	var plants Vegetation
	parallel.Run(
		func() {
			if ctx.Err() == nil {
				plants.Mushrooms = MushroomsForGuides(guides, vegetationGrid, orientation)
			}
		},
		func() {
			if ctx.Err() != nil {
				return
			}
			field := newVineTerrainWithWorkspace(background, foreground, false, fields)
			defer field.release()
			if ctx.Err() == nil {
				plants.Vines = generateVinesInBandContext(ctx, field, rand.New(rand.NewSource(SectionSeed(seed^0x76696e6573, id))), 0, SectionHeight, 5)
			}
		},
		func() {
			if ctx.Err() == nil {
				plants.ForegroundVines = generateForegroundVinesContext(ctx, foreground, guides, rand.New(rand.NewSource(SectionSeed(seed^0x73757266616365, id))), frontFields)
			}
		},
	)
	if ctx.Err() != nil {
		return SectionData{}
	}
	for _, stage := range cuts {
		plants, _ = stage.cut.vegetation(plants, stage.grid, top)
	}
	data.Vines, data.ForegroundVines, data.Mushrooms = plants.Vines, plants.ForegroundVines, plants.Mushrooms
	data.vegetationCuts = plants.Cuts
	return data
}
