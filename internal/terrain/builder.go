package terrain

import (
	"image/color"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type Builder struct {
	seed        int64
	loadSection SectionLoader
	guides      *guideCache
	fields      vineWorkspace
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
	return buildSectionCached(b.seed, id, b.loadSection, b.guides, &b.fields, onTerrain)
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
}

func BuildSection(seed, id int64) SectionData {
	return NewSectionBuilder(seed, nil).Build(id)
}

func buildSectionCached(seed, id int64, loadSection SectionLoader, guideCache *guideCache, fields *vineWorkspace, onTerrain func(SectionData)) SectionData {
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
	backgroundSeeds := worldSeedsInRange(seed^0x62617365, top, backgroundNoise, BackgroundMinX, BackgroundMaxX)
	seeds := artisticRockSeeds(worldSeeds(seed, top, noise), guides, seed, top)
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(seed))))
	background := newRockGridInRange(backgroundSeeds, func(geom.V) color.NRGBA { return color.NRGBA{A: 255} }, BackgroundMinX, BackgroundMaxX)
	foreground := guideRockFaces(seeds, guides)
	shapeRockGrid(background, nil, backgroundNoise)
	shapeReliefGrid(foreground, guides, noise, branches)
	foreground = contourRockGrid(foreground, func(p geom.V) float64 {
		return reliefHeight(p, guides, noise, branches)
	})
	polishRockContours(foreground, guides, func(p geom.V) float64 {
		return reliefHeight(p, guides, noise, branches)
	})
	shadeRockGrids(background, foreground, backgroundNoise, orientation)
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
	data := SectionData{Orientation: orientation, ID: id, Background: background, Foreground: foreground, Guides: guides, Holes: holes, ForegroundTopology: topology}
	if len(topology.Cuts) > 0 {
		data.Foreground = topology.Grid
	}
	if onTerrain != nil {
		onTerrain(data)
	}
	mushrooms := MushroomsForGuides(guides, vegetationGrid, orientation)
	field := newVineTerrainWithWorkspace(background, foreground, false, fields)
	vines := generateVinesInBand(field, rand.New(rand.NewSource(SectionSeed(seed^0x76696e6573, id))), 0, SectionHeight, 5)
	field.release()
	foregroundVines := generateForegroundVinesWithWorkspace(foreground, guides, rand.New(rand.NewSource(SectionSeed(seed^0x73757266616365, id))), fields)
	plants := Vegetation{Vines: vines, ForegroundVines: foregroundVines, Mushrooms: mushrooms}
	for _, stage := range cuts {
		plants, _ = stage.cut.vegetation(plants, stage.grid, top)
	}
	data.Vines, data.ForegroundVines, data.Mushrooms = plants.Vines, plants.ForegroundVines, plants.Mushrooms
	data.vegetationCuts = plants.Cuts
	return data
}
