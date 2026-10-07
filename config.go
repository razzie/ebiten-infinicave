package infinicave

import (
	"fmt"
	"math"
)

// Config controls generation and rendering. Start with DefaultConfig to use
// the viewer's appearance. A zero Config disables texture, background effects,
// fog, and bats.
type Config struct {
	Seed int64
	// Orientation fixes the scrolling axis for the scene's lifetime. The zero
	// value is Vertical. Horizontal grows rightward and keeps plants upright.
	Orientation Orientation
	// Texture is surface grain strength, from 0 (disabled) to 16.
	Texture float64
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
	// Fog enables moving mist between the background and foreground layers.
	// Diagnostic views disable fog regardless of this setting.
	Fog bool
	// BatsPerMinute is the average number of animated bat arrivals per minute.
	// It must be finite and nonnegative; zero disables bats.
	// Bats use world coordinates and are hidden outside ViewShaded.
	BatsPerMinute float64
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
	// for vegetation generation or render meshes.
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
	return Config{Texture: 8, BackgroundBlur: .002, ShadowOpacity: .65,
		ShadowBlur: .008, ShadowOffset: V{X: .018, Y: .025}, Fog: true, BatsPerMinute: 7.5, View: ViewShaded}
}

func (c Config) validate() error {
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
	return nil
}
