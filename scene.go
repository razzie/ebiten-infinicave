package infinicave

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Width is the fixed cross-axis span of the cave in scene units.
const Width = terrain.Width

// SectionHeight is the edge length of one square streamed section in scene units.
const SectionHeight = terrain.SectionHeight

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
	fog                  *render.FogRenderer
	background           *render.BackgroundRenderer
	bats                 *render.BatFlock
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
	g.material, err = ebiten.NewShader(render.MaterialShaderSource)
	if err != nil {
		return nil, fmt.Errorf("infinicave: compile rock shader: %w", err)
	}
	g.backgroundFade, err = ebiten.NewShader(render.BackgroundFadeShaderSource)
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile background fade shader: %w", err)
	}
	g.vineMaterial, err = ebiten.NewShader(render.VineShaderSource)
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile vine shader: %w", err)
	}
	if config.Fog > 0 && g.view == ViewShaded {
		g.fog, err = render.NewFogRenderer(config.Fog, g.orientation)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile fog shader: %w", err)
		}
	}
	if g.view == ViewShaded && (config.BackgroundBlur > 0 || config.ShadowOpacity > 0) {
		effects := render.BackgroundConfig{BackgroundBlur: config.BackgroundBlur, ShadowOpacity: config.ShadowOpacity, ShadowBlur: config.ShadowBlur}
		effects.ShadowOffset = terrain.InternalPoint(g.orientation, config.ShadowOffset)
		g.background, err = render.NewBackgroundRenderer(effects)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile background shaders: %w", err)
		}
	}
	g.world = newWorld(config.Seed, g.view, g.collisionTolerance, terrain.OrientedSectionLoader(g.orientation, g.loadSection), g.orientation)
	if config.BatsPerMinute > 0 && g.view == ViewShaded {
		g.bats = render.NewBatFlock(config.Seed, config.BatsPerMinute, g.orientation)
	}
	return g, nil
}

// Update uploads prepared meshes, requests missing sections, and evicts distant
// sections, and advances enabled ambient animations. Call once per game tick.
// It never waits for generation and returns true when all layers needed by
// viewport are ready. Collision becomes available
// before background generation, shading, and vegetation; foreground images
// publish before background and vegetation. OnCollisionReady runs here as soon as generated polygons
// arrive. An invalid viewport or a closed Scene returns false without doing work.
func (g *Scene) Update(viewport Viewport) bool {
	viewport, valid := orientedViewport(g.orientation, viewport)
	if g.closed || !valid {
		return false
	}
	w := g.world
	w.receiveCollision(g)
	if g.closed || g.world != w { // The callback may close or reset the scene.
		return false
	}
	if g.fog != nil {
		g.fog.Update()
	}
	if g.bats != nil {
		g.bats.Update(viewport)
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
	g.world.deferParked()
}

// Draw draws available terrain, fog, vegetation, and enabled bats into dst,
// scaling uniformly at min(dst width, dst height) pixels per scene unit. The
// cave is centered across its bounded axis. Background and fog extend from
// -0.5 to 1.5 across that axis and fade toward its edges. Use viewport.Height =
// target height / min(target width, target height) for Vertical, or viewport.Width
// = target width / min(target width, target height) for Horizontal, with the
// same viewport as Update. Horizontal scenes use a reusable native-size buffer
// to map the section-frame compositor into world orientation.
// Fog appears immediately across unloaded bands; bats may fly across them. Draw does
// not add hover, loading text, or UI, and does nothing for an invalid viewport
// or a closed Scene.
func (g *Scene) Draw(dst *ebiten.Image, viewport Viewport) {
	viewport, valid := orientedViewport(g.orientation, viewport)
	if g.closed || !valid {
		return
	}
	target := dst
	if g.orientation == Horizontal {
		// Reuse the section-frame compositor at native resolution. A quarter
		// turn is an exact pixel mapping; transparent gaps preserve dst.
		size := dst.Bounds().Size()
		render.EnsureEffectImage(&g.orientedTarget, size.Y, size.X)
		g.orientedTarget.Clear()
		target = g.orientedTarget
	}
	g.world.draw(target, viewport.Y, viewport.Height, g.fog, g.background)
	if g.bats != nil {
		g.bats.Draw(target, viewport)
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
	g.world = newWorld(seed, g.view, g.collisionTolerance, terrain.OrientedSectionLoader(g.orientation, g.loadSection), g.orientation)
	g.world.pixels = pixels
	if g.bats != nil {
		g.bats = render.NewBatFlock(seed, g.bats.PerMinute, g.orientation)
	}
}

// Close stops background work and releases all GPU resources. It is safe to
// call repeatedly. Background generation stops at its next cancellation check;
// Close does not wait for its workers.
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
		g.fog.Close()
	}
	if g.background != nil {
		g.background.Close()
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
