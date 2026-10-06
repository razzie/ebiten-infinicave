package infinicave

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Width is the fixed width of the cave in scene units.
const Width = 1

// SectionHeight is the height of one square streamed section in scene units.
const SectionHeight = 1

// Config controls generation and rendering. Start with DefaultConfig to use
// the viewer's appearance. A zero Config is valid and disables texture, fog, and bats.
type Config struct {
	Seed int64
	// Texture is surface grain strength, from 0 (disabled) to 16.
	Texture float64
	// Fog enables moving mist between the background and foreground layers.
	// Diagnostic views disable fog regardless of this setting.
	Fog bool
	// Bats enables occasional animated bats flying across the viewport.
	// Bats use world coordinates and are hidden outside ViewShaded.
	Bats bool
	// View selects the material. Diagnostic views disable texture, fog, and vegetation.
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
	return Config{Texture: 8, Fog: true, Bats: true, View: ViewShaded}
}

func (c Config) validate() error {
	if math.IsNaN(c.Texture) || math.IsInf(c.Texture, 0) || c.Texture < 0 || c.Texture > 16 {
		return fmt.Errorf("infinicave: texture must be between 0 and 16")
	}
	if c.View < ViewShaded || c.View > ViewShadows {
		return fmt.Errorf("infinicave: invalid terrain view %v", c.View)
	}
	if math.IsNaN(c.CollisionTolerance) || math.IsInf(c.CollisionTolerance, 0) || c.CollisionTolerance < 0 {
		return fmt.Errorf("infinicave: collision tolerance must be finite and nonnegative")
	}
	return nil
}

// Viewport describes the region to render, always Width scene units wide.
// World Y is negative above the starting floor at zero. For a viewport at the
// floor, use Y = -Height. Velocity is scene units per tick and only controls
// prefetching; the caller owns camera movement.
type Viewport struct {
	Y        float64
	Height   float64
	Velocity float64
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
	world                *world
	material             *ebiten.Shader
	vineMaterial         *ebiten.Shader
	fog                  *fogRenderer
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
	g := &Scene{texture: config.Texture, view: config.View, collisionTolerance: config.CollisionTolerance, loadSection: config.LoadSection, onCollisionReady: config.OnCollisionReady}
	if g.view != ViewShaded {
		g.texture = 0
	}
	var err error
	g.material, err = ebiten.NewShader(materialShaderSource)
	if err != nil {
		return nil, fmt.Errorf("infinicave: compile rock shader: %w", err)
	}
	g.vineMaterial, err = ebiten.NewShader(vineShaderSource)
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile vine shader: %w", err)
	}
	if config.Fog && g.view == ViewShaded {
		g.fog, err = newFogRenderer()
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile fog shader: %w", err)
		}
	}
	g.world = newWorld(config.Seed, g.view, g.collisionTolerance, g.loadSection)
	if config.Bats && g.view == ViewShaded {
		g.bats = newBatFlock(config.Seed)
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
	if g.closed || !viewport.valid() {
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
// Pass the native screen width before Update. Resizing retains generation,
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
// scaling uniformly so Width scene units fill its width. Use an aspect ratio of
// Width:viewport.Height and the same viewport as Update.
// Missing terrain is left untouched; bats may fly across unloaded areas. Draw does
// not add hover, loading text, or UI, and does nothing for an invalid viewport
// or a closed Scene.
func (g *Scene) Draw(dst *ebiten.Image, viewport Viewport) {
	if !g.closed && viewport.valid() {
		g.world.draw(dst, viewport.Y, viewport.Height, g.fog)
		if g.bats != nil {
			g.bats.draw(dst, viewport)
		}
	}
}

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
	g.world = newWorld(seed, g.view, g.collisionTolerance, g.loadSection)
	g.world.pixels = pixels
	if g.bats != nil {
		g.bats = newBatFlock(seed)
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
	if g.world != nil {
		g.world.close()
	}
	if g.fog != nil {
		g.fog.shader.Deallocate()
	}
	if g.vineMaterial != nil {
		g.vineMaterial.Deallocate()
	}
	if g.material != nil {
		g.material.Deallocate()
	}
}
