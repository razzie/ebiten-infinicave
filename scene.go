package infinicave

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Width is the fixed cross-axis span of the cave in scene units: its width in
// Vertical scenes and its height in Horizontal scenes.
const Width = 1

// SectionHeight is the edge length of one square streamed section in scene units.
const SectionHeight = 1

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
		ShadowBlur: .008, ShadowOffset: V{.018, .025}, Fog: true, BatsPerMinute: 7.5, View: ViewShaded}
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

// Viewport describes the region to render. Vertical scenes use Y and Height,
// with Y <= -Height and a starting viewport at Y = -Height. Horizontal scenes
// use X and Width, with X >= 0 and a starting viewport at X = 0. The other
// axis spans [0, 1]. Velocity is movement along world Y or X in scene units
// per tick and controls prefetching; the caller owns camera movement.
type Viewport struct {
	Y        float64
	Height   float64
	Velocity float64
	// Horizontal scenes use X and Width instead of Y and Height. X is the
	// left edge, must be nonnegative, and positive Velocity moves rightward.
	X, Width float64
}

func (v Viewport) valid() bool {
	return v.Height > 0 && !math.IsNaN(v.Height) && !math.IsInf(v.Height, 0) &&
		!math.IsNaN(v.Y) && !math.IsInf(v.Y, 0) &&
		v.Y <= -v.Height && !math.IsNaN(v.Velocity) && !math.IsInf(v.Velocity, 0)
}

// Scene streams and renders an infinite cave. Create it with NewScene and
// release it with Close. All Scene methods must run on the Ebitengine game
// goroutine; generation and mesh preparation run in background workers.
// The zero value is not usable.
type Scene struct {
	orientation          Orientation
	orientedTarget       *ebiten.Image
	world                *world
	material             *ebiten.Shader
	backgroundFade       *ebiten.Shader
	vineMaterial         *ebiten.Shader
	fog                  *fogRenderer
	background           *backgroundRenderer
	bats                 *batFlock
	geometryRevisionBase uint64 // revisions accumulated across world resets
	texture              float64
	view                 View
	collisionTolerance   float64
	loadSection          SectionLoader
	onCollisionReady     func(CollisionGeometry)
	closed               bool
}

// NewScene validates config, compiles embedded shaders, and starts generation.
// It does not change window settings, read input, or start an Ebitengine loop.
func NewScene(config Config) (*Scene, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	g := &Scene{orientation: config.Orientation, texture: config.Texture, view: config.View, collisionTolerance: config.CollisionTolerance, loadSection: config.LoadSection, onCollisionReady: config.OnCollisionReady}
	if g.view != ViewShaded {
		g.texture = 0
	}
	var err error
	g.material, err = ebiten.NewShader(materialShaderSource)
	if err != nil {
		return nil, fmt.Errorf("infinicave: compile rock shader: %w", err)
	}
	g.backgroundFade, err = ebiten.NewShader(backgroundFadeShaderSource)
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile background fade shader: %w", err)
	}
	g.vineMaterial, err = ebiten.NewShader(vineShaderSource)
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile vine shader: %w", err)
	}
	if config.Fog && g.view == ViewShaded {
		g.fog, err = newFogRenderer(g.orientation)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile fog shader: %w", err)
		}
	}
	if g.view == ViewShaded && (config.BackgroundBlur > 0 || config.ShadowOpacity > 0) {
		effects := config
		effects.ShadowOffset = g.orientation.internal(config.ShadowOffset)
		g.background, err = newBackgroundRenderer(effects)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile background shaders: %w", err)
		}
	}
	g.world = newWorld(config.Seed, g.view, g.collisionTolerance, g.orientation.sectionLoader(g.loadSection), g.orientation)
	if config.BatsPerMinute > 0 && g.view == ViewShaded {
		g.bats = newBatFlock(config.Seed, config.BatsPerMinute, g.orientation)
	}
	return g, nil
}

// Update uploads prepared meshes, requests missing sections, and evicts distant
// sections, and advances enabled ambient animations. Call once per game tick.
// It never waits for generation and returns true when all layers needed by
// viewport are ready. Collision becomes available
// before vegetation generation and mesh preparation; complete terrain uploads
// before vegetation. OnCollisionReady runs here as soon as generated polygons
// arrive. An invalid viewport or a closed Scene returns false without doing work.
func (g *Scene) Update(viewport Viewport) bool {
	viewport, valid := g.orientation.viewport(viewport)
	if g.closed || !valid {
		return false
	}
	w := g.world
	w.receiveCollision(g)
	if g.closed || g.world != w { // The callback may close or reset the scene.
		return false
	}
	if g.fog != nil {
		g.fog.update()
	}
	if g.bats != nil {
		g.bats.update(viewport)
	}
	g.world.viewport = viewport
	if u := g.world.upload; u != nil && !meshVisible(u.data, viewport) {
		g.world.deferUpload()
	}
	g.world.receive(g)
	ready := g.world.ensure(viewport.Y, viewport.Height, viewport.Velocity)
	g.world.prune(viewport.Y, viewport.Height, viewport.Velocity)
	return ready
}

// GeometryRevision returns a scene-wide version for cached foreground geometry.
// Compare it after Update and runtime edits before reusing Formation, Guide, or
// CollisionGeometry copies. A change means geometry or availability may differ,
// even if an object's ID is unchanged; refetch IDs to resolve merges or expiry.
// Early collision publication, queryable terrain publication, eviction, and
// terrain edits advance the version. Collision can become available before
// Query sees that section; query publication advances the version again.
// Reset and the first Close advance it, and it stays monotonic across Reset.
// Rendering, camera movement alone, resizing, and reads do not advance it.
// Versions are local to this Scene; their numeric difference is not an event count.
// Like other Scene methods, call it on the Ebitengine game goroutine.
func (g *Scene) GeometryRevision() uint64 {
	if g.closed || g.world == nil {
		return g.geometryRevisionBase
	}
	return g.geometryRevisionBase + g.world.revision + g.world.collisionRevision
}

// SetRenderWidth sets the number of cached pixels across one scene unit.
// Pass min(nativeWidth, nativeHeight) before Update. Resizing retains generation,
// queries, and runtime cuts, and rerasterizes only sections contributing to the
// viewport. Offscreen sections keep their images until they become visible.
// Nonpositive widths and calls after Close are ignored. The default is 1000.
func (g *Scene) SetRenderWidth(pixels int) {
	if g.closed || pixels <= 0 || pixels == g.world.renderWidth() {
		return
	}
	g.world.pixels = pixels
	g.world.deferUpload()
}

// Draw draws available terrain, fog, vegetation, and enabled bats into dst,
// scaling uniformly at min(dst width, dst height) pixels per scene unit. The
// cave is centered across its bounded axis. Background and fog extend from
// -0.5 to 1.5 across that axis and fade toward its edges. Use viewport.Height =
// target height / min(target width, target height) for Vertical, or viewport.Width
// = target width / min(target width, target height) for Horizontal, with the
// same viewport as Update. Horizontal scenes use a reusable native-size buffer
// to map the section-frame compositor into world orientation.
// Missing terrain is left untouched; bats may fly across unloaded areas. Draw does
// not add hover, loading text, or UI, and does nothing for an invalid viewport
// or a closed Scene.
func (g *Scene) Draw(dst *ebiten.Image, viewport Viewport) {
	viewport, valid := g.orientation.viewport(viewport)
	if g.closed || !valid {
		return
	}
	target := dst
	if g.orientation == Horizontal {
		// Reuse the section-frame compositor at native resolution. A quarter
		// turn is an exact pixel mapping; transparent gaps preserve dst.
		size := dst.Bounds().Size()
		ensureEffectImage(&g.orientedTarget, size.Y, size.X)
		g.orientedTarget.Clear()
		target = g.orientedTarget
	}
	g.world.draw(target, viewport.Y, viewport.Height, g.fog, g.background)
	if g.bats != nil {
		g.bats.draw(target, viewport)
	}
	if g.orientation == Horizontal {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.SetElement(0, 0, 0)
		op.GeoM.SetElement(0, 1, -1)
		op.GeoM.SetElement(1, 0, 1)
		op.GeoM.SetElement(1, 1, 0)
		op.GeoM.Translate(float64(dst.Bounds().Dx()), 0)
		dst.DrawImage(target, op)
	}
}

// Orientation returns the scene's fixed scrolling orientation.
func (g *Scene) Orientation() Orientation { return g.orientation }

// Reset replaces the world with a new seed, keeping rendering settings and
// the section content loader and collision callback. Enabled bats restart
// their arrival sequence with the new seed.
// The next Update begins loading again. Reset does nothing after Close.
func (g *Scene) Reset(seed int64) {
	if g.closed {
		return
	}
	pixels := g.world.renderWidth()
	g.geometryRevisionBase = g.GeometryRevision() + 1
	g.world.close()
	g.world = newWorld(seed, g.view, g.collisionTolerance, g.orientation.sectionLoader(g.loadSection), g.orientation)
	g.world.pixels = pixels
	if g.bats != nil {
		g.bats = newBatFlock(seed, g.bats.perMinute, g.orientation)
	}
}

// Close stops background work and releases all GPU resources. It is safe to
// call repeatedly. An in-progress CPU generation finishes in the background
// before its worker exits; Close does not wait for it.
func (g *Scene) Close() {
	if g.closed {
		return
	}
	g.geometryRevisionBase = g.GeometryRevision() + 1
	g.closed = true
	g.bats = nil
	if g.orientedTarget != nil {
		g.orientedTarget.Deallocate()
		g.orientedTarget = nil
	}
	if g.world != nil {
		g.world.close()
	}
	if g.fog != nil {
		g.fog.shader.Deallocate()
	}
	if g.background != nil {
		g.background.close()
	}
	if g.vineMaterial != nil {
		g.vineMaterial.Deallocate()
	}
	if g.backgroundFade != nil {
		g.backgroundFade.Deallocate()
	}
	if g.material != nil {
		g.material.Deallocate()
	}
}
