package infinicave

import (
	"fmt"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Config controls generation, streaming, and rendering. Start with DefaultConfig to use
// the viewer's appearance. A zero Config disables texture, background effects,
// fog, bats, mushrooms, spider webs, moss, grass, hanging growth, and fine edge detail.
type Config struct {
	Seed int64
	// Orientation fixes the scrolling axis for the scene's lifetime. The zero
	// value is Vertical. Horizontal grows rightward and keeps plants upright.
	Orientation Orientation
	// RetainSections keeps published sections this many sections beyond
	// each end of the viewport along the scrolling axis (up/down or left/right).
	// It must be nonnegative; the default is 3. This does not request new sections.
	// Immediate neighbors needed for vegetation, directional lookahead, and an
	// active GPU upload remain retained even outside this range.
	RetainSections int
	// LookaheadSections requests this many extra sections beyond the immediate
	// neighbor in the direction of travel. It must be nonnegative; the default
	// is 4. Zero disables lookahead, and stationary viewports never look ahead.
	// Lookahead sections stay retained while moving toward them. The count does
	// not depend on speed, and Update readiness excludes these extra sections.
	// When enabled, it also uploads all layers of immediate neighbors and
	// lookahead sections before they become visible, after visible uploads.
	// Requests and unpublished results share a bounded queue; lookahead fills
	// as capacity permits. Pending results outside the request window are discarded.
	LookaheadSections int
	// ForceGuidesUp selects the overall upward-facing exposed side of each guide,
	// regardless of point order or BrightSign, in both orientations. The rock side
	// stays continuous through bends and curls. False preserves authored sides.
	ForceGuidesUp bool
	// BranchingStrategy selects recursive longer branches (the default),
	// simple short spurs, or reconnecting loops with irregular enclosed openings.
	BranchingStrategy BranchingStrategy
	// BranchMinThickness and BranchMaxThickness bound nominal branch thickness
	// in scene units (the cave cross-axis spans 1). Soft shoulders and tapered
	// tips can extend beyond or narrow below these bounds. Each zero endpoint
	// uses its default: 0.048 minimum, 0.08 maximum. Equal bounds fix thickness.
	// Values must be finite and between 0 and 1; the resolved minimum must not
	// exceed the maximum. Limbs below terrain sampling width may stop growing.
	BranchMinThickness float64
	BranchMaxThickness float64
	// SpikeDensity controls the probability of small teeth on exposed rock
	// edges, from 0 (disabled) to 1. The viewer default is 0.65.
	SpikeDensity float64
	// EdgeRoughness controls shallow angular chips on exposed rock edges,
	// from 0 (disabled) to 1. The viewer default is 0.75.
	// Both edge settings decorate the rendered edge without adding vertices
	// to structural polygons or changing collision and query geometry.
	EdgeRoughness float64
	// Texture is surface grain strength, from 0 (disabled) to 16.
	Texture float64
	// DisableBackground skips rendering and GPU uploads for background rocks
	// and vines, including blur and foreground shadows. False keeps them enabled.
	// Fog is controlled separately by Fog. Generation and geometry are unchanged.
	DisableBackground bool
	// BackgroundBlur is Gaussian blur softness in scene units (0 disables it,
	// maximum 0.05). Background rocks and vines are blurred together.
	BackgroundBlur float64
	// ShadowOpacity controls foreground rock shadows on the background, from
	// 0 (disabled) to 1. Shadows follow the current silhouette, including cuts.
	ShadowOpacity float64
	// ShadowBlur is shadow softness in scene units, from 0 (hard) to 0.05.
	ShadowBlur float64
	// ShadowOffset projects rock shadows in scene units; positive Y is down.
	// Each component must be between -1 and 1.
	ShadowOffset V
	// Fog is moving mist strength between the background and foreground layers.
	// It must be finite and nonnegative; 0 disables it, 1 is the default strength,
	// and values above 1 produce denser mist.
	// Mist appears across unloaded bands too. Diagnostic views disable fog.
	Fog float64
	// BatsPerMinute is the average number of animated bat arrivals per minute.
	// It must be finite and nonnegative; zero disables bats.
	// Bats use world coordinates and are hidden outside ViewShaded.
	BatsPerMinute float64
	// MushroomDensity is the probability of retaining a mushroom cluster,
	// from 0 (disabled) to 1 (all eligible groups). The viewer default is 0.5. Placement remains
	// deterministic for a seed; lower density preserves surviving clusters.
	MushroomDensity float64
	// MossDensity controls green and yellow moss on upward-facing foreground
	// facets, from 0 (disabled) to 1 (maximum coverage). The default is 0.72.
	// Lower density preserves the placement and shape of surviving patches.
	MossDensity float64
	// GrassDensity controls yellow or green grass tufts and their ground
	// patches beside guides on upward-facing foreground facets, from
	// 0 (disabled) to 1 (maximum coverage).
	// The default is 0.35. Lower density preserves surviving tufts.
	GrassDensity float64
	// HangingGrowthDensity is the occurrence probability for cave bulbs and
	// lichen on eligible exposed undersides, from 0 (disabled) to 1.
	// The default is 0.28. Lower density preserves surviving growth.
	HangingGrowthDensity float64
	// SpiderWebMaxSize limits the distance between any two web attachments,
	// including support strands, in scene units (the cave cross-axis spans 1).
	// It must be finite and nonnegative. Zero uses the default of 0.15.
	SpiderWebMaxSize float64
	// SpiderWebDensity is the probability of considering an eligible rock
	// pocket for a web, from 0 (disabled) to 1 (all eligible pockets).
	// The viewer default is 0.35. Placement remains deterministic for a seed.
	SpiderWebDensity float64
	// View selects the material. Diagnostic views disable texture, background
	// effects, fog, and vegetation.
	View View
	// CollisionTolerance is the polygon simplification tolerance in scene units.
	// Zero preserves exact collision geometry. Larger values reduce vertices and
	// collision cost at the expense of accuracy. Rendering remains detailed.
	CollisionTolerance float64
	// LoadSection supplies authored guides and holes for each section.
	// Nil uses the default random generator.
	LoadSection SectionLoader
	// OnCollisionReady receives foreground collision polygons without waiting
	// for background generation, shading, vegetation, or render meshes.
	// Scene calls it during Update on the game goroutine, including for prefetched
	// sections. The geometry owns its slices and includes authored holes, stored
	// runtime cuts, and CollisionTolerance. Nil disables notifications.
	// Evicted sections notify again when regenerated; Reset retains the callback.
	// Runtime edits do not notify; use CarveResult.SectionIDs to refresh collisions.
	// GenerateSectionWithConfig calls it synchronously on the calling goroutine.
	OnCollisionReady func(CollisionGeometry)
}

// DefaultConfig returns the viewer's appearance with a deterministic seed of 0.
func DefaultConfig() Config {
	return Config{
		Orientation:          Vertical,
		RetainSections:       3,
		LookaheadSections:    4,
		ForceGuidesUp:        true,
		BranchingStrategy:    BranchingRecursive,
		BranchMinThickness:   terrain.DefaultShapeOptions().BranchMinThickness,
		BranchMaxThickness:   terrain.DefaultShapeOptions().BranchMaxThickness,
		SpikeDensity:         terrain.DefaultShapeOptions().SpikeDensity,
		EdgeRoughness:        terrain.DefaultShapeOptions().EdgeRoughness,
		Texture:              8,
		BackgroundBlur:       .002,
		ShadowOpacity:        .65,
		ShadowBlur:           .008,
		ShadowOffset:         V{X: .018, Y: .025},
		Fog:                  2,
		BatsPerMinute:        7.5,
		MushroomDensity:      terrain.DefaultMushroomDensity,
		MossDensity:          render.DefaultMossDensity,
		GrassDensity:         render.DefaultGrassDensity,
		HangingGrowthDensity: render.DefaultHangingGrowthDensity,
		SpiderWebMaxSize:     terrain.DefaultSpiderWebMaxSize,
		SpiderWebDensity:     terrain.DefaultSpiderWebDensity,
		View:                 ViewShaded,
	}
}

func (c Config) validate() error {
	if c.RetainSections < 0 || c.LookaheadSections < 0 {
		return fmt.Errorf("infinicave: retain sections and lookahead sections must be nonnegative")
	}
	for _, value := range []float64{c.BranchMinThickness, c.BranchMaxThickness} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("infinicave: branch thickness must be between 0 and 1 (0 uses the default)")
		}
	}
	lo, hi := c.shapeOptions().BranchThicknessBounds()
	if lo > hi {
		return fmt.Errorf("infinicave: minimum branch thickness must not exceed maximum branch thickness")
	}
	if c.BranchingStrategy > BranchingLoops {
		return fmt.Errorf("infinicave: invalid branching strategy %v", c.BranchingStrategy)
	}
	for _, setting := range []struct {
		name  string
		value float64
	}{{"spike density", c.SpikeDensity}, {"edge roughness", c.EdgeRoughness}} {
		if math.IsNaN(setting.value) || math.IsInf(setting.value, 0) || setting.value < 0 || setting.value > 1 {
			return fmt.Errorf("infinicave: %s must be between 0 and 1", setting.name)
		}
	}
	if c.Orientation != Vertical && c.Orientation != Horizontal {
		return fmt.Errorf("infinicave: invalid orientation %v", c.Orientation)
	}
	if math.IsNaN(c.Texture) || math.IsInf(c.Texture, 0) || c.Texture < 0 || c.Texture > 16 {
		return fmt.Errorf("infinicave: texture must be between 0 and 16")
	}
	if c.View < ViewShaded || c.View > ViewShadows {
		return fmt.Errorf("infinicave: invalid terrain view %v", c.View)
	}
	for _, setting := range []struct {
		name         string
		value, limit float64
	}{{"background blur", c.BackgroundBlur, .05}, {"shadow blur", c.ShadowBlur, .05}, {"shadow opacity", c.ShadowOpacity, 1}} {
		if math.IsNaN(setting.value) || math.IsInf(setting.value, 0) || setting.value < 0 || setting.value > setting.limit {
			return fmt.Errorf("infinicave: %s must be between 0 and %g", setting.name, setting.limit)
		}
	}
	if math.IsNaN(c.Fog) || math.IsInf(c.Fog, 0) || c.Fog < 0 {
		return fmt.Errorf("infinicave: fog strength must be finite and nonnegative")
	}
	for _, offset := range []float64{c.ShadowOffset.X, c.ShadowOffset.Y} {
		if math.IsNaN(offset) || math.IsInf(offset, 0) || math.Abs(offset) > 1 {
			return fmt.Errorf("infinicave: shadow offset components must be between -1 and 1")
		}
	}
	if math.IsNaN(c.CollisionTolerance) || math.IsInf(c.CollisionTolerance, 0) || c.CollisionTolerance < 0 {
		return fmt.Errorf("infinicave: collision tolerance must be finite and nonnegative")
	}
	if math.IsNaN(c.BatsPerMinute) || math.IsInf(c.BatsPerMinute, 0) || c.BatsPerMinute < 0 {
		return fmt.Errorf("infinicave: bats per minute must be finite and nonnegative")
	}
	if math.IsNaN(c.SpiderWebMaxSize) || math.IsInf(c.SpiderWebMaxSize, 0) || c.SpiderWebMaxSize < 0 {
		return fmt.Errorf("infinicave: spider web maximum size must be finite and nonnegative")
	}
	if math.IsNaN(c.SpiderWebDensity) || math.IsInf(c.SpiderWebDensity, 0) || c.SpiderWebDensity < 0 || c.SpiderWebDensity > 1 {
		return fmt.Errorf("infinicave: spider web density must be between 0 and 1")
	}
	if math.IsNaN(c.MushroomDensity) || math.IsInf(c.MushroomDensity, 0) || c.MushroomDensity < 0 || c.MushroomDensity > 1 {
		return fmt.Errorf("infinicave: mushroom density must be between 0 and 1")
	}
	for _, setting := range []struct {
		name  string
		value float64
	}{{"moss", c.MossDensity}, {"grass", c.GrassDensity}, {"hanging growth", c.HangingGrowthDensity}} {
		if math.IsNaN(setting.value) || math.IsInf(setting.value, 0) || setting.value < 0 || setting.value > 1 {
			return fmt.Errorf("infinicave: %s density must be between 0 and 1", setting.name)
		}
	}
	return nil
}

func (c Config) shapeOptions() terrain.ShapeOptions {
	return terrain.ShapeOptions{BranchingStrategy: c.BranchingStrategy, BranchMinThickness: c.BranchMinThickness, BranchMaxThickness: c.BranchMaxThickness, SpikeDensity: c.SpikeDensity, EdgeRoughness: c.EdgeRoughness}
}
