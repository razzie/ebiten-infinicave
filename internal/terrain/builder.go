package terrain

import (
	"context"
	"math/rand"
	"time"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
)

// Builder retains reusable generation workspaces and a bounded guide cache.
// A builder has one caller at a time; concurrent sections use separate builders.
type Builder struct {
	// Shape selects branch growth and fine silhouette detail.
	Shape ShapeOptions
	// ForceGuidesUp selects one overall upward-facing side per guide.
	ForceGuidesUp bool
	// MushroomDensity is the cluster retention probability; zero disables mushrooms.
	MushroomDensity float64
	// SpiderWebMaxSize bounds decoration span; zero uses the default.
	SpiderWebMaxSize float64
	// SpiderWebDensity is the eligible-pocket probability; zero disables webs.
	SpiderWebDensity float64
	seed             int64
	loadSection      SectionLoader
	guides           *guideCache
}

func NewSectionBuilder(seed int64, loadSection SectionLoader, orientation ...Orientation) *Builder {
	return &Builder{MushroomDensity: DefaultMushroomDensity, SpiderWebDensity: DefaultSpiderWebDensity, seed: seed, loadSection: loadSection, guides: newGuideCache(seed, orientation...)}
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
	ForegroundBake         *RockTopology
	VegetationBake         *Vegetation
	Shape                  ShapeOptions
	Orientation            Orientation
	ID                     int64
	Background, Foreground RockGrid
	Vines                  []Vine
	ForegroundVines        []Vine
	Mushrooms              []MushroomGroup
	Guides                 []Guide
	Holes                  []Hole
	ForegroundTopology     *RockTopology
	BackgroundNeighbors    [][]int
}

func BuildSection(seed, id int64) SectionData {
	return NewSectionBuilder(seed, nil).Build(id)
}

func (b *Builder) Begin(ctx context.Context, id int64) (s *SectionBuild) {
	return b.BeginWithoutGuides(ctx, id, nil)
}

// BeginWithoutGuides excludes permanently destroyed formation sources before
// projection preparation, branch growth, foreground shaping, and decoration.
// retired is an immutable snapshot owned by the requesting world.
func (b *Builder) BeginWithoutGuides(ctx context.Context, id int64, retired map[[32]byte]bool) (s *SectionBuild) {
	start := time.Now()
	defer func() {
		if s != nil {
			s.timings.Geometry = time.Since(start)
		}
	}()
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
		guides = guideCache.windowWithoutGuides(id, retired)
	}
	guides = liveGuides(guides, top, retired)
	if ctx.Err() != nil {
		return nil
	}
	for i := range guides {
		if b.ForceGuidesUp {
			guides[i].faceUp(orientation)
		}
		guides[i].prepareProjection()
	}
	var foreground RockGrid
	var segments []BranchSegment
	if len(guides) > 0 {
		seeds := artisticRockSeeds(worldSeeds(seed, top, noise), guides, seed, top)
		if ctx.Err() != nil {
			return nil
		}
		segments = generateBranchesWithOptions(guides, noise, rand.New(rand.NewSource(seed)), b.Shape)
		branches := newShapedBranchField(segments, b.Shape.BranchingStrategy)
		foreground = guideRockFaces(seeds, guides)
		if ctx.Err() != nil {
			return nil
		}
		shapeReliefGrid(foreground, guides, noise, branches)
		if ctx.Err() != nil {
			return nil
		}
		foreground = contourRockGrid(foreground, func(p geom.V) float64 { return reliefHeight(p, guides, noise, branches) }, guides)
		if ctx.Err() != nil {
			return nil
		}
		polishRockContours(foreground, guides, func(p geom.V) float64 { return reliefHeight(p, guides, noise, branches) })
		trimRockNeedles(foreground, guides)
		cleanRockJunctions(foreground)
	}

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
	s = &SectionBuild{seed: seed, raw: foreground, uncut: topology, uncutParents: parents, branches: branchPaths(segments)}
	s.data = SectionData{Orientation: orientation, ID: id, Foreground: foreground, Guides: guides, Holes: holes}
	s.data.Shape = b.Shape
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
		s.cuts = append(s.cuts, cut)
	}
	s.data.ForegroundTopology = topology
	s.parents = parents
	if len(topology.Cuts) > 0 {
		s.data.Foreground = topology.Grid
	}
	return s
}

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
	// The uncut snapshot contains the foreground's exact guides and branches.
	// Authored holes and later carving only remove the front layer.
	footprint := newRockFootprint(s.raw)
	footprint.worldTop = SectionTop(s.data.ID)
	noise.OffsetY = SectionTop(s.data.ID)
	seeds := worldSeedsInRange(s.seed^0x62617365, noise.OffsetY, noise, BackgroundMinX, BackgroundMaxX)
	// A world-keyed subset gives rear formations slightly larger cells without
	// changing their silhouette or introducing section-local sampling seams.
	coarse := seeds[:0]
	for _, p := range seeds {
		if SiteRandom(cellSeed(s.seed^0x636f61727365, p, noise.OffsetY), 0, 0, 0) >= .30 {
			coarse = append(coarse, p)
		}
	}
	seeds = coarse
	if ctx.Err() != nil {
		return false
	}
	background := newBackgroundRockGrid(s.seed, seeds, footprint)
	if ctx.Err() != nil {
		return false
	}
	// Route stems and leaves against the actual silhouette, including padding holes.
	footprint.background = newRockFootprint(background)
	backgroundVines := backgroundPathVines(s.seed, s.data.ID, s.data.Orientation, footprint)
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

// GenerationTimings excludes application collision callbacks and GPU work.
// Timers belong to the worker-owned build, not deterministic section data.
type GenerationTimings struct{ Geometry, Material, Decoration time.Duration }

func (s *SectionBuild) Timings() GenerationTimings { return s.timings }
