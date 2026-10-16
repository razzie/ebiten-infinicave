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
	antialiasing         AntialiasingMode
	fxaa                 *render.FXAARenderer
	shape                terrain.ShapeOptions
	orientation          Orientation
	forceGuidesUp        bool
	retainSections       int
	lookaheadSections    int
	orientedTarget       *ebiten.Image
	world                *world
	material             *ebiten.Shader
	backgroundFade       *ebiten.Shader
	vineMaterial         *ebiten.Shader
	fog                  *render.FogRenderer
	background           *render.BackgroundRenderer
	disableBackground    bool
	bats                 *render.BatFlock
	decorationBurn       *render.BurnDissolve
	appearanceFade       *render.FadeRenderer
	geometryRevisionBase uint64 // revisions accumulated across world resets
	texture              float64
	view                 View
	collisionTolerance   float64
	spiderWebMaxSize     float64
	spiderWebDensity     float64
	mushroomDensity      float64
	rockGrowth           render.RockGrowthOptions
	loadSection          SectionLoader
	onCollisionReady     func(CollisionGeometry)
	closed               bool
	materialsWarm        bool
}

// NewScene validates config, compiles embedded shaders, and starts generation.
// It does not change window settings, read input, or start an Ebitengine loop.
func NewScene(config Config) (*Scene, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	g := &Scene{orientation: config.Orientation, forceGuidesUp: config.ForceGuidesUp, texture: config.Texture, view: config.View, collisionTolerance: config.CollisionTolerance, spiderWebMaxSize: config.SpiderWebMaxSize, spiderWebDensity: config.SpiderWebDensity, mushroomDensity: config.MushroomDensity, rockGrowth: render.RockGrowthOptions{Moss: config.MossDensity, Hanging: config.HangingGrowthDensity, Grass: config.GrassDensity}, loadSection: config.LoadSection, onCollisionReady: config.OnCollisionReady}
	g.antialiasing = config.Antialiasing
	g.shape = config.shapeOptions()
	g.retainSections, g.lookaheadSections = config.RetainSections, config.LookaheadSections
	g.disableBackground = config.DisableBackground
	if g.view != ViewShaded {
		g.texture = 0
	}
	var err error
	if (config.AppearanceFade || config.AppearanceBurn) && g.view == ViewShaded {
		g.appearanceFade, err = render.NewFadeRenderer()
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile appearance fade shader: %w", err)
		}
	}
	if config.DecorationBurn && g.view == ViewShaded {
		g.decorationBurn, err = render.NewBurnDissolve()
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile decoration burn shader: %w", err)
		}
	}
	if g.antialiasing == AntialiasingFXAA {
		g.fxaa, err = render.NewFXAARenderer()
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile FXAA shader: %w", err)
		}
	}
	g.material, err = ebiten.NewShader(render.MaterialShaderSource)
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile rock shader: %w", err)
	}
	if !g.disableBackground {
		g.backgroundFade, err = ebiten.NewShader(render.BackgroundFadeShaderSource)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile background fade shader: %w", err)
		}
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
	if !g.disableBackground && g.view == ViewShaded && (config.BackgroundBlur > 0 || config.ShadowOpacity > 0) {
		effects := render.BackgroundConfig{BackgroundBlur: config.BackgroundBlur, ShadowOpacity: config.ShadowOpacity, ShadowBlur: config.ShadowBlur}
		effects.ShadowOffset = terrain.InternalPoint(g.orientation, config.ShadowOffset)
		g.background, err = render.NewBackgroundRenderer(effects)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("infinicave: compile background shaders: %w", err)
		}
	}
	g.world = newWorld(config.Seed, g.view, g.collisionTolerance, g.spiderWebMaxSize, g.spiderWebDensity, g.mushroomDensity, g.forceGuidesUp, terrain.OrientedSectionLoader(g.orientation, g.loadSection), g.rockGrowth, g.shape, g.orientation)
	g.world.disableBackground = g.disableBackground
	g.world.appearanceFade = g.appearanceFade
	g.world.antialiasing = g.antialiasing
	g.world.retainSections, g.world.lookaheadSections = g.retainSections, g.lookaheadSections
	if config.BatsPerMinute > 0 && g.view == ViewShaded {
		g.bats = render.NewBatFlock(config.Seed, config.BatsPerMinute, g.orientation)
	}
	return g, nil
}

// Update uploads prepared meshes, requests missing sections, and evicts distant
// sections, and advances enabled ambient animations. Call once per game tick.
// It never waits for generation and returns true when all layers needed by
// viewport are ready. Collision becomes available
// before background generation, shading, and vegetation. Foreground recipes
// arrive early; available vegetation images publish independently of terrain
// uploads. OnCollisionReady runs here as soon as generated polygons
// arrive. An invalid viewport or a closed Scene returns false without doing work.
func (g *Scene) Update(viewport Viewport) bool {
	viewport, valid := orientedViewport(g.orientation, viewport)
	if g.closed || !valid {
		return false
	}
	w := g.world
	g.flushCarveImages()
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
	if g.decorationBurn != nil {
		g.decorationBurn.Update()
	}
	for _, section := range w.sections {
		section.foregroundReveal.Update()
		for i := range section.growth {
			section.growth[i].reveal.Update()
		}
		section.backgroundReveal.Update()
		for i := range section.plants {
			section.plants[i].reveal.Update()
		}
		section.grassReveal.Update()
	}
	g.world.viewport = viewport
	if u := g.world.upload; u != nil && !g.world.meshUploadEligible(u.data, viewport) {
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
// queries, and runtime cuts, and rerasterizes sections contributing to the
// viewport, followed by neighbors and directional lookahead when enabled.
// Other retained sections keep their images until needed.
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
	g.DrawWithPasses(dst, viewport, RenderPasses{})
}

// DrawWithPasses renders like Draw with optional custom passes around the
// foreground rocks. Foreground vines, mushrooms, and webs precede AboveRocks;
// foreground grass patches and blades follow it. Rear grass blades precede
// the rocks. Bats draw last.
// Callbacks run once per draw, including across unloaded terrain, and never
// run for invalid viewports or a closed Scene. Drawing does not animate them;
// advance custom effects in the caller's Update.
func (g *Scene) DrawWithPasses(dst *ebiten.Image, viewport Viewport, passes RenderPasses) {
	publicViewport := viewport
	viewport, valid := orientedViewport(g.orientation, viewport)
	if g.closed || !valid {
		return
	}
	composition := dst
	g.flushCarveImages()
	if g.fxaa != nil {
		composition = g.fxaa.Begin(dst)
	}
	g.warmMaterials(composition)
	target := composition
	if g.orientation == Horizontal {
		// Reuse the section-frame compositor at native resolution. A quarter
		// turn is an exact pixel mapping; transparent gaps preserve dst.
		size := composition.Bounds().Size()
		render.EnsureEffectImage(&g.orientedTarget, size.Y, size.X)
		g.orientedTarget.Clear()
		target = g.orientedTarget
	}
	context := newRenderContext(target, publicViewport, g.orientation, viewport.Y)
	context.Antialiasing = g.antialiasing
	g.world.draw(target, viewport.Y, viewport.Height, g.fog, g.background, passes, context, g.decorationBurn)
	if g.bats != nil {
		g.bats.DrawWithAntialiasing(target, viewport, g.antialiasing.GeometryAA())
	}
	if g.orientation == Horizontal {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.SetElement(0, 0, 0)
		op.GeoM.SetElement(0, 1, -1)
		op.GeoM.SetElement(1, 0, 1)
		op.GeoM.SetElement(1, 1, 0)
		op.GeoM.Translate(float64(composition.Bounds().Dx()), 0)
		composition.DrawImage(target, op)
	}
	if g.fxaa != nil {
		g.fxaa.Draw(dst)
	}
}

// Orientation returns the scene's fixed scrolling orientation.
func (g *Scene) Orientation() Orientation { return g.orientation }

// Reset replaces the world with a new seed, keeping rendering and streaming
// settings, the section content loader, and collision callback. Enabled bats restart
// their arrival sequence with the new seed.
// The next Update begins loading again. Reset does nothing after Close.
func (g *Scene) Reset(seed int64) {
	if g.closed {
		return
	}
	pixels := g.world.renderWidth()
	if g.decorationBurn != nil {
		g.decorationBurn.Clear()
	}
	g.geometryRevisionBase = g.GeometryRevision() + 1
	g.world.close()
	g.world = newWorld(seed, g.view, g.collisionTolerance, g.spiderWebMaxSize, g.spiderWebDensity, g.mushroomDensity, g.forceGuidesUp, terrain.OrientedSectionLoader(g.orientation, g.loadSection), g.rockGrowth, g.shape, g.orientation)
	g.world.disableBackground = g.disableBackground
	g.world.appearanceFade = g.appearanceFade
	g.world.antialiasing = g.antialiasing
	g.world.retainSections, g.world.lookaheadSections = g.retainSections, g.lookaheadSections
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
	if g.appearanceFade != nil {
		g.appearanceFade.Close()
		g.appearanceFade = nil
	}
	if g.decorationBurn != nil {
		g.decorationBurn.Close()
		g.decorationBurn = nil
	}
	if g.fxaa != nil {
		g.fxaa.Close()
		g.fxaa = nil
	}
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

// SceneReadiness reports independently drawable layers for a viewport.
// Disabled layers count as ready. It does not request or wait for generation.
type SceneReadiness struct {
	Collision, Foreground, Background, Decorations bool
}

// Readiness reports current layer availability. Call on the game goroutine.
// Foreground recipes arrive before background and decoration recipes. Uploads
// can finish independently; Update returns whether all layers needed by the
// viewport have finished.
func (g *Scene) Readiness(viewport Viewport) SceneReadiness {
	viewport, valid := orientedViewport(g.orientation, viewport)
	if g.closed || !valid || g.world == nil {
		return SceneReadiness{}
	}
	w := g.world
	ready := SceneReadiness{Collision: true, Foreground: true, Background: true, Decorations: true}
	low, high := visibleSections(viewport.Y, viewport.Height)
	for id := low; id <= high; id++ {
		s := w.sections[id]
		if s == nil || s.geometry == nil {
			ready.Collision = ready.Collision && w.collision[id] != nil
		}
		if s == nil || s.foreground == nil || s.renderWidth() != w.renderWidth() {
			ready.Foreground = false
		}
		if s == nil || s.grassPending || s.grassWidth() != w.renderWidth() || (s.mesh != nil && !s.growthReady(w.renderWidth())) {
			ready.Decorations = false
		}
		if !g.disableBackground && (s == nil || s.terrain == nil || s.backgroundWidth() != w.renderWidth()) {
			ready.Background = false
		}
	}
	// Until ownership migrates to shared references, neighboring padded recipes
	// may supply plants to this viewport. Unknown recipes are still pending.
	for id := max(0, low-1); id <= high+1; id++ {
		s := w.sections[id]
		if s != nil && s.mesh != nil && !s.mesh.TerrainOnly && !meshVisible(*s.mesh, viewport) {
			continue
		}
		if s == nil || s.vegetationPending || s.vegetationWidth() != w.renderWidth() {
			ready.Decorations = false
		}
	}
	return ready
}

// RenderPass draws custom content once per valid DrawWithPasses call, on the
// game goroutine, including while terrain is loading and in diagnostic views.
// A pass must only draw: do not update, reset, carve, or close the Scene here.
type RenderPass func(RenderContext)

// RenderPasses inserts custom content into the scene's back-to-front order.
// Nil callbacks skip their passes. Passes do not participate in background
// blur or shadow masks. Bats draw after both passes and all decorations.
type RenderPasses struct {
	// BehindRocks runs after background effects and fog, before foreground rocks.
	BehindRocks RenderPass
	// AboveRocks runs after rocks, mushrooms, webs, and foreground vines,
	// before foreground grass patches and blades.
	AboveRocks RenderPass
}

// RenderContext maps public world coordinates to the current drawing target.
// Horizontal scenes draw into a temporary rotated target; always use
// WorldToTarget rather than assuming Dst has the final screen orientation.
type RenderContext struct {
	// Antialiasing selects the scene's geometry policy. Custom triangle/path
	// draws should use GeometryAA(); FXAA includes both passes in its output.
	Antialiasing AntialiasingMode
	// Dst is borrowed for this callback; do not retain or deallocate it.
	Dst *ebiten.Image
	// Viewport uses public world axes, adjusted to the renderer's pixel alignment.
	Viewport    Viewport
	Orientation Orientation
	// WorldToTarget includes camera translation, native scale, centering, and
	// orientation. Concat it after a sprite's own placement in world units.
	WorldToTarget ebiten.GeoM
	// Min and Max bound the whole target in world units, including side margins.
	Min, Max V
}

func newRenderContext(dst *ebiten.Image, viewport Viewport, orientation Orientation, y float64) RenderContext {
	view := render.TargetTransform(dst)
	y = render.RenderAlignedY(y, view.Pixels)
	c := RenderContext{Dst: dst, Viewport: viewport, Orientation: orientation}
	if orientation == Horizontal {
		c.Viewport.X = -(y + viewport.Width)
		// (world X, world Y) -> (world Y, -world X) in the section frame.
		c.WorldToTarget.SetElement(0, 0, 0)
		c.WorldToTarget.SetElement(0, 1, 1)
		c.WorldToTarget.SetElement(1, 0, -1)
		c.WorldToTarget.SetElement(1, 1, 0)
	} else {
		c.Viewport.Y = y
	}
	c.WorldToTarget.Translate(0, -y)
	c.WorldToTarget.Scale(float64(view.Pixels), float64(view.Pixels))
	c.WorldToTarget.Translate(view.OffsetX, 0)
	inverse := c.WorldToTarget
	inverse.Invert()
	bounds := dst.Bounds()
	x0, y0 := inverse.Apply(float64(bounds.Min.X), float64(bounds.Min.Y))
	x1, y1 := inverse.Apply(float64(bounds.Max.X), float64(bounds.Max.Y))
	c.Min, c.Max = V{X: min(x0, x1), Y: min(y0, y1)}, V{X: max(x0, x1), Y: max(y0, y1)}
	return c
}
