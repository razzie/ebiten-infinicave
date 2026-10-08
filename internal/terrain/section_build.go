package terrain

import (
	"context"
	"image/color"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
)

// SectionBuild owns immutable shape snapshots between independently scheduled
// geometry, material, and decoration phases. Reusable fields belong to Builder.
type SectionBuild struct {
	seed                  int64
	data                  SectionData
	raw                   RockGrid
	uncut                 *RockTopology
	parents, uncutParents []int
	cuts                  []sectionCutStage
	vegetationGrid        RockGrid
	growthGrid            RockGrid
	materialized          bool
}

type sectionCutStage struct {
	cut     RockCut
	grid    RockGrid
	parents []int
}

func (s *SectionBuild) Data() SectionData { return s.data }

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
	noise := NewPerlin(rand.New(rand.NewSource(s.seed ^ 0x62617365)))
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
	s.vegetationGrid = apply(s.uncut.Grid, s.uncutParents)
	for i := range s.cuts {
		s.cuts[i].grid = apply(s.cuts[i].grid, s.cuts[i].parents)
	}
	s.growthGrid = foreground
	s.data.Background, s.data.BackgroundNeighbors = background, neighbors
	s.data.Foreground = foreground
	s.data.ForegroundTopology = &topology
	if len(topology.Cuts) > 0 {
		s.data.Foreground = topology.Grid
	}
	s.materialized = true
	return true
}

// Decorate can be canceled at stage and vine-trial boundaries, then retried
// deterministically on another builder. It never exposes incomplete plants.
func (b *Builder) Decorate(ctx context.Context, s *SectionBuild) bool {
	if ctx.Err() != nil || !s.materialized {
		return false
	}
	data := s.data
	var plants Vegetation
	parallel.Run(
		func() {
			if ctx.Err() == nil {
				plants.Mushrooms = MushroomsForGuides(data.Guides, s.vegetationGrid, data.Orientation)
			}
		},
		func() {
			if ctx.Err() != nil {
				return
			}
			field := newVineTerrainWithWorkspace(data.Background, s.growthGrid, false, &b.fields)
			defer field.release()
			if ctx.Err() == nil {
				plants.Vines = generateVinesInBandContext(ctx, field, rand.New(rand.NewSource(SectionSeed(s.seed^0x76696e6573, data.ID))), 0, SectionHeight, 5)
			}
		},
		func() {
			if ctx.Err() == nil {
				plants.ForegroundVines = generateForegroundVinesContext(ctx, s.growthGrid, data.Guides, rand.New(rand.NewSource(SectionSeed(s.seed^0x73757266616365, data.ID))), &b.frontFields)
			}
		},
	)
	if ctx.Err() != nil {
		return false
	}
	for _, stage := range s.cuts {
		plants, _ = stage.cut.vegetation(plants, stage.grid, SectionTop(data.ID))
	}
	if ctx.Err() != nil {
		return false
	}
	s.data.Vines, s.data.ForegroundVines, s.data.Mushrooms = plants.Vines, plants.ForegroundVines, plants.Mushrooms
	s.data.vegetationCuts = plants.Cuts
	return true
}
