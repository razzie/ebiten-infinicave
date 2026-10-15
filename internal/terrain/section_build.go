package terrain

import (
	"context"
	"image/color"
	"math/rand"
	"time"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
)

// SectionBuild owns immutable shape snapshots between independently scheduled
// geometry, material, and decoration phases, plus immutable appearance recipes.
type SectionBuild struct {
	timings               GenerationTimings
	seed                  int64
	data                  SectionData
	raw                   RockGrid
	uncut                 *RockTopology
	parents, uncutParents []int
	cuts                  []RockCut
	vegetationGrid        RockGrid
	materialized          bool
	branches              []BranchPath
}

func (s *SectionBuild) Data() SectionData { return s.data }

// MaterializeForeground prepares the final visible appearance without building
// background geometry or fitting vegetation. Previously published topology
// remains immutable; full Materialize later binds the compatibility material
// used by synchronous generation and decoration placement.
func (s *SectionBuild) MaterializeForeground(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if s.data.ForegroundBake != nil {
		return true
	}
	bake := PrepareRockAppearance(s.uncut)
	if ctx.Err() != nil {
		return false
	}
	topology := s.data.ForegroundTopology
	if len(topology.Cuts) == 0 {
		topology = bake
	}
	s.data.ForegroundBake = bake
	s.data.ForegroundTopology = topology
	return true
}

// Materialize keeps the original shading inputs and operation order. The
// source-face map carries material onto inset/cut fragments without rebuilding
// boundaries or changing the already published collision snapshot.
func (s *SectionBuild) Materialize(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if s.materialized {
		return true
	}
	start := time.Now()
	defer func() { s.timings.Material = time.Since(start) }()
	noise := NewPerlin(rand.New(rand.NewSource(s.seed ^ 0x62617365)))
	backgroundVines := backgroundPathVines(s.seed, s.data.ID, s.data.Orientation)
	noise.OffsetY = SectionTop(s.data.ID)
	seeds := worldSeedsInRange(s.seed^0x62617365, noise.OffsetY, noise, BackgroundMinX, BackgroundMaxX)
	if ctx.Err() != nil {
		return false
	}
	background := newRockGridInRange(seeds, func(geom.V) color.NRGBA { return color.NRGBA{A: 255} }, BackgroundMinX, BackgroundMaxX)
	if ctx.Err() != nil {
		return false
	}
	neighbors := shapeRockGrid(background, nil, noise)
	foreground := append(RockGrid(nil), s.raw...)
	shadeRockGrids(background, foreground, noise, s.data.Orientation)
	var supportVines []Vine
	for owner := s.data.ID - 2; owner <= s.data.ID+2; owner++ {
		vines := backgroundPathVines(s.seed, owner, s.data.Orientation)
		dy := SectionTop(owner) - SectionTop(s.data.ID)
		for i := range vines {
			for j := range vines[i].Points {
				vines[i].Points[j].P.Y += dy
			}
		}
		supportVines = append(supportVines, vines...)
	}
	supportBackgroundVines(background, supportVines)
	if ctx.Err() != nil {
		return false
	}
	apply := func(grid RockGrid, parents []int) RockGrid {
		result := append(RockGrid(nil), grid...)
		for i := range result {
			source := foreground[parents[i]]
			result[i].Color, result[i].Normal = source.Color, source.Normal
			result[i].Shadow, result[i].Ambient = source.Shadow, source.Ambient
		}
		return result
	}
	topology := *s.data.ForegroundTopology
	topology.Grid = apply(topology.Grid, s.parents)
	if ctx.Err() != nil {
		return false
	}
	s.vegetationGrid = apply(s.uncut.Grid, s.uncutParents)
	base := *s.uncut
	base.Grid = s.vegetationGrid
	if s.data.ForegroundBake != nil {
		// Visible appearance depends on foreground topology, not background
		// material. Reuse the immutable early shading instead of repeating it.
		base.Appearance = s.data.ForegroundBake.Appearance
	}
	s.data.ForegroundBake = PrepareRockAppearance(&base)
	if len(topology.Cuts) == 0 {
		topology = *s.data.ForegroundBake
	}
	s.data.Background, s.data.BackgroundNeighbors = background, neighbors
	s.data.Vines = backgroundVines
	s.data.Foreground = foreground
	s.data.ForegroundTopology = &topology
	if len(topology.Cuts) > 0 {
		s.data.Foreground = topology.Grid
	}
	s.materialized = true
	return true
}

// Decorate can be canceled at stage and vine-trial boundaries, then retried
// deterministically on another builder. It never exposes incomplete decorations.
func (b *Builder) Decorate(ctx context.Context, s *SectionBuild) bool {
	return b.DecorateProducts(ctx, s, nil)
}

// DecorateProducts publishes immutable snapshots as each independent generator
// completes. publish may run concurrently; it must finish before returning.
// A canceled build can be retried and may publish a product again.
func (b *Builder) DecorateProducts(ctx context.Context, s *SectionBuild, publish func(VegetationParts, SectionData)) bool {
	start := time.Now()
	defer func() { s.timings.Decoration = time.Since(start) }()
	if ctx.Err() != nil || !s.materialized {
		return false
	}
	data := s.data
	plants := Vegetation{}
	var topology *RockTopology
	emit := func(part VegetationParts, vegetation Vegetation, webs *RockTopology) {
		if publish == nil || ctx.Err() != nil {
			return
		}
		snapshot := data
		bake := vegetation
		snapshot.VegetationBake = &bake
		for _, cut := range s.cuts {
			vegetation, _ = cut.vegetation(vegetation, SectionTop(data.ID))
		}
		snapshot.Vines, snapshot.ForegroundVines, snapshot.Mushrooms = vegetation.Vines, vegetation.ForegroundVines, vegetation.Mushrooms
		if webs != nil {
			baked := *data.ForegroundBake
			baked.SpiderWebs = webs.SpiderWebs
			snapshot.ForegroundBake = &baked
			snapshot.ForegroundTopology = webs
		}
		if ctx.Err() == nil {
			publish(part, snapshot)
		}
	}
	parallel.Run(
		func() {
			if ctx.Err() == nil {
				plants.Vines = data.Vines
				emit(BackgroundVinesPart, Vegetation{Vines: plants.Vines}, nil)
			}
		},
		func() {
			topology, _ = prepareSpiderWebsContext(ctx, data.ForegroundTopology, b.SpiderWebMaxSize, b.SpiderWebDensity)
			emit(WebsPart, Vegetation{}, topology)
		},
		func() {
			if ctx.Err() == nil {
				plants.Mushrooms = MushroomsForGuides(data.Guides, s.vegetationGrid, b.MushroomDensity, data.Orientation)
				emit(MushroomsPart, Vegetation{Mushrooms: plants.Mushrooms}, nil)
			}
		},
		func() {
			if ctx.Err() == nil {
				plants.ForegroundVines = foregroundPathVines(ctx, s.branches, data.ForegroundBake, data.Guides, s.seed, data.ID)
				emit(ForegroundVinesPart, Vegetation{ForegroundVines: plants.ForegroundVines}, nil)
			}
		},
	)
	if ctx.Err() != nil {
		return false
	}
	bake := plants
	baseWebs := topology
	s.data.VegetationBake = &bake
	for _, cut := range s.cuts {
		plants, _ = cut.vegetation(plants, SectionTop(data.ID))
	}
	if ctx.Err() != nil {
		return false
	}
	baked := *s.data.ForegroundBake
	baked.SpiderWebs = baseWebs.SpiderWebs
	s.data.ForegroundBake = &baked
	s.data.ForegroundTopology = topology
	s.data.Vines, s.data.ForegroundVines, s.data.Mushrooms = plants.Vines, plants.ForegroundVines, plants.Mushrooms
	return true
}
