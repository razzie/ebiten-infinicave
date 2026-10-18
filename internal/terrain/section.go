package terrain

import (
	"fmt"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Section holds generated geometry plus one section of padding on each side
// along the scrolling axis. Geometry uses ordinary X/Y scene units, local to
// the owned square [0, 1]². Add Origin to local points to get world coordinates.
// Padding spans local Y = -1 to 2 in Vertical scenes or local X = -1 to 2 in
// Horizontal scenes. Min/Max describe the owned world square; WindowOrigin
// starts the padded window. Vegetation may extend into padding and should be
// drawn once per owning section. Background cells and vines stay within -0.5 to 1.5
// across the bounded axis. Top and WindowTop are legacy Vertical metadata.
//
// Each generated Section owns its slices; callers may modify them. Rock grids
// describe visual faces; Collision contains boundaries for physics integration.
type Section struct {
	Orientation Orientation
	// Origin converts section-local points to world coordinates by addition.
	// Min/Max bound the owned square; WindowOrigin starts the padded window.
	Origin, WindowOrigin, Min, Max geom.V
	// ID is 0 at the starting edge, then -1, -2, ... upward or rightward.
	// Top is ID-1 in Vertical scenes and zero in Horizontal scenes.
	ID                     int64
	Top, WindowTop         float64
	Background, Foreground RockGrid
	Vines, ForegroundVines []Vine
	Mushrooms              []MushroomGroup
	Guides                 []Guide
	Holes                  []Hole
	// Collision contains foreground rock boundaries in world coordinates.
	Collision CollisionGeometry
}

func SectionTop(id int64) float64 { return -float64(id+1) * SectionHeight }

func SectionWindowTop(id int64) float64 { return SectionTop(id) - SectionHeight }

// SectionContent supplies authored content for a square cave section. Fields
// use section-local scene units, with the owned square spanning (0, 0) to (1, 1).
// The structure can grow to include other authored objects in future versions.
// Empty Guides produces no procedural foreground. Holes remove foreground rock
// after shaping. Hole bounds must fit -1 to 2 along the scrolling axis,
// including seam crossings (Y for Vertical, X for Horizontal);
// declare longer cuts as multiple section-local holes. Invalid holes are ignored.
type SectionContent struct {
	Guides []Guide
	Holes  []Hole
}

// SectionLoader loads content for one square section. IDs start at 0 at the
// starting edge and decrease in the direction of growth: -1, -2, ... . Points
// use ordinary section-local X/Y in the configured orientation.
// The loader owns its returned slices;
// generation copies them and recomputes guide S, Min, and Max.
// BrightSign defaults to 1; use -1 to reverse the lit side. A zero Seed receives
// a stable seed based on the world seed, section ID, and guide's slice index.
// Guides with nonfinite coordinates or fewer than two distinct consecutive
// points are ignored. Returning empty content does not request random guides.
//
// Generation also loads neighboring sections for seamless padding. IDs may be
// requested repeatedly and in any order, so return consistent content per ID.
// Scene serializes loader calls within one world and copies returned content
// before the next call. Synchronous generation calls it on the calling goroutine.
// Reset can overlap an old worker,
// so loaders sharing mutable state must be safe for concurrent calls.
type SectionLoader func(id int64) SectionContent

// Internal section indices increase upward; only the public loader sees IDs.
func loadedWorldContent(seed, id int64, load SectionLoader) SectionContent {
	var content SectionContent
	top := SectionTop(id)
	// Match the random generator's padding, including shoulders beyond it.
	for owner := id + 2; owner >= max(0, id-2); owner-- {
		sourceContent := load(-owner)
		for i, source := range sourceContent.Guides {
			g, ok := loadedGuide(source)
			if !ok {
				continue
			}
			if g.Seed == 0 {
				g.Seed = SectionSeed(SectionSeed(seed, owner), int64(i+1)) | 1
			}
			g.translateY(SectionTop(owner) - top)
			content.Guides = append(content.Guides, g)
		}
		for _, hole := range sourceContent.Holes {
			cut, err := CutFromHole(hole)
			if err != nil || cut.Min.Y < GenerationMinY || cut.Max.Y > generationMaxY {
				continue
			}
			content.Holes = append(content.Holes, TranslateHoleY(hole, SectionTop(owner)-top))
		}
	}
	return content
}

func loadedGuide(source Guide) (Guide, bool) {
	g := Guide{BrightSign: source.BrightSign, Seed: source.Seed}
	if g.BrightSign == 0 {
		g.BrightSign = 1
	}
	for _, p := range source.Pts {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return Guide{}, false
		}
		if len(g.Pts) == 0 || p != g.Pts[len(g.Pts)-1] {
			g.Pts = append(g.Pts, p)
		}
	}
	if len(g.Pts) < 2 {
		return Guide{}, false
	}
	g.S = make([]float64, len(g.Pts))
	g.Min, g.Max = g.Pts[0], g.Pts[0]
	for i, p := range g.Pts {
		if i > 0 {
			g.S[i] = g.S[i-1] + p.Sub(g.Pts[i-1]).Len()
		}
		g.Min.X, g.Min.Y = min(g.Min.X, p.X), min(g.Min.Y, p.Y)
		g.Max.X, g.Max.Y = max(g.Max.X, p.X), max(g.Max.Y, p.Y)
	}
	return g, true
}

// GenerationOptions contains the settings shared by synchronous generation and
// the scene's worker. Rendering settings are validated by the public facade.
type GenerationOptions struct {
	Shape              ShapeOptions
	Seed               int64
	Orientation        Orientation
	ForceGuidesUp      bool
	MushroomDensity    float64
	CollisionTolerance float64
	SpiderWebMaxSize   float64
	SpiderWebDensity   float64
	LoadSection        SectionLoader
	OnCollisionReady   func(CollisionGeometry)
}

// GenerateSection returns an owned, world-oriented section. The caller validates
// configuration before calling; section IDs are validated here.
func GenerateSection(config GenerationOptions, id int64) (Section, error) {
	if id > 0 || id == -1<<63 {
		return Section{}, fmt.Errorf("infinicave: section ID must be nonpositive and greater than the minimum int64")
	}
	// Streaming uses nonnegative indices internally in the direction of growth.
	index := -id
	var collision CollisionGeometry
	builder := NewSectionBuilder(config.Seed, OrientedSectionLoader(config.Orientation, config.LoadSection), config.Orientation)
	builder.ForceGuidesUp = config.ForceGuidesUp
	builder.Shape = config.Shape
	builder.MushroomDensity = config.MushroomDensity
	builder.SpiderWebMaxSize = config.SpiderWebMaxSize
	builder.SpiderWebDensity = config.SpiderWebDensity
	data := builder.BuildWithTerrain(index, func(data SectionData) {
		collision = OrientedCollision(config.Orientation, PrepareTerrainGeometry(data, config.CollisionTolerance).Collision)
		if config.OnCollisionReady != nil {
			config.OnCollisionReady(CopyCollisionGeometry(collision))
		}
	})
	section := Section{
		ID: id, Top: SectionTop(index), WindowTop: SectionWindowTop(index),
		Background: data.Background, Foreground: data.Foreground,
		Vines: data.Vines, ForegroundVines: data.ForegroundVines,
		Mushrooms: data.Mushrooms, Guides: data.Guides, Holes: data.Holes,
		Collision: collision,
	}
	return OrientedSection(config.Orientation, section), nil
}

const (
	BackgroundMinX = -.5
	BackgroundMaxX = 1.5
)

// Width is the fixed cross-axis span of the cave in scene units: its width in
// Vertical scenes and its height in Horizontal scenes.
const Width = 1

// SectionHeight is the edge length of one square streamed section in scene units.
const SectionHeight = 1

const (
	// All generation geometry uses scene units. The owned section is [0,1]²;
	// its generation window adds one unit of padding above and below.
	generationWidth       = Width
	GenerationMinY        = -SectionHeight
	generationMaxY        = 2 * SectionHeight
	GenerationHeight      = generationMaxY - GenerationMinY
	foregroundScreenInset = .018

	guideSpacing   = .020
	guideInfluence = .180
	noiseScale     = 3.4
)
