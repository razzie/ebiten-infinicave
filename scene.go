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
// the viewer's appearance. A zero Config is valid and disables texture.
type Config struct {
	Seed int64
	// Texture is surface grain strength, from 0 (disabled) to 16.
	Texture float64
	// Study selects a full cave (StudyNone) or an isolated rock shape.
	Study Study
	// View selects the material. Diagnostic views disable texture and vegetation.
	View View
	// CollisionTolerance is the polygon simplification tolerance in scene units.
	// Zero preserves exact collision geometry. Larger values reduce vertices and
	// collision cost at the expense of accuracy. Rendering remains detailed.
	CollisionTolerance float64
}

// DefaultConfig returns the viewer's appearance with a deterministic seed of 0.
func DefaultConfig() Config {
	return Config{Texture: 8, View: ViewShaded}
}

func (c Config) validate() error {
	if math.IsNaN(c.Texture) || math.IsInf(c.Texture, 0) || c.Texture < 0 || c.Texture > 16 {
		return fmt.Errorf("infinicave: texture must be between 0 and 16")
	}
	if c.Study < StudyNone || c.Study > StudyCurl {
		return fmt.Errorf("infinicave: invalid study %v", c.Study)
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

// generation converts scene units to the generator's fixed raster resolution.
func (v Viewport) generation() Viewport {
	return Viewport{Y: v.Y * generationWidth, Height: v.Height * generationWidth, Velocity: v.Velocity * generationWidth}
}

// Scene streams and renders an infinite cave. Create it with NewScene and
// release it with Close. All Scene methods must run on the Ebitengine game
// goroutine; generation and mesh preparation run in background workers.
// The zero value is not usable.
type Scene struct {
	world              *world
	material           *ebiten.Shader
	vineMaterial       *ebiten.Shader
	highlight          *hoverRenderer
	texture            float64
	study              Study
	view               View
	collisionTolerance float64
	closed             bool
}

// NewScene validates config, compiles embedded shaders, and starts generation.
// It does not change window settings, read input, or start an Ebitengine loop.
func NewScene(config Config) (*Scene, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	g := &Scene{texture: config.Texture, study: config.Study, view: config.View, collisionTolerance: config.CollisionTolerance * generationWidth}
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
	g.highlight, err = newHoverRenderer()
	if err != nil {
		g.Close()
		return nil, fmt.Errorf("infinicave: compile hover shader: %w", err)
	}
	g.world = newWorld(config.Seed, g.study, g.view, g.collisionTolerance)
	return g, nil
}

// Update uploads prepared meshes, requests missing sections, and evicts distant
// sections. Call once per game tick. It never waits for generation and returns
// true when all layers needed by viewport are ready. An invalid viewport or a
// closed Scene returns false without doing work.
func (g *Scene) Update(viewport Viewport) bool {
	if g.closed || !viewport.valid() {
		return false
	}
	g.world.receive(g)
	viewport = viewport.generation()
	ready := g.world.ensure(viewport.Y, viewport.Height, viewport.Velocity)
	g.world.prune(viewport.Y, viewport.Height, viewport.Velocity)
	return ready
}

// Draw draws available terrain and vegetation into dst, scaling uniformly so
// Width scene units fill its width. Use a destination with aspect ratio
// Width:viewport.Height and the same viewport as Update.
// Missing sections and areas outside the cave are left untouched. Draw does
// not add hover, loading text, or UI, and does nothing for an invalid viewport
// or a closed Scene.
func (g *Scene) Draw(dst *ebiten.Image, viewport Viewport) {
	if !g.closed && viewport.valid() {
		viewport = viewport.generation()
		g.world.draw(dst, viewport.Y, viewport.Height)
	}
}

// Reset replaces the world with a new seed, keeping rendering settings.
// The next Update begins loading again. Reset does nothing after Close.
func (g *Scene) Reset(seed int64) {
	if g.closed {
		return
	}
	g.world.close()
	if g.highlight != nil {
		g.highlight.clear()
	}
	g.world = newWorld(seed, g.study, g.view, g.collisionTolerance)
}

// Close stops background work and releases all GPU resources. It is safe to
// call repeatedly. An in-progress CPU generation finishes in the background
// before its worker exits; Close does not wait for it.
func (g *Scene) Close() {
	if g.closed {
		return
	}
	g.closed = true
	if g.world != nil {
		g.world.close()
	}
	if g.highlight != nil {
		g.highlight.close()
	}
	if g.vineMaterial != nil {
		g.vineMaterial.Deallocate()
	}
	if g.material != nil {
		g.material.Deallocate()
	}
}
