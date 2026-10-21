package render

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type FogRenderer struct {
	Orientation  terrain.Orientation
	Strength     float64
	shader       *ebiten.Shader
	Time         float64
	Velocity     geom.V
	wispVelocity geom.V
	cloudOffset  geom.V
	wispOffset   geom.V
}

func NewFogRenderer(strength float64, orientation ...terrain.Orientation) (*FogRenderer, error) {
	shader, err := ebiten.NewShader(fogShaderSource)
	if err != nil {
		return nil, err
	}
	return &FogRenderer{
		Orientation: terrain.OptionalOrientation(orientation), Strength: strength, shader: shader,
		Velocity: geom.V{X: .018, Y: -.009}, wispVelocity: geom.V{X: -.011, Y: -.004},
	}, nil
}

// SetVelocity advects both noise layers together in world units per second.
// Retaining their accumulated offsets prevents jumps when wind changes.
func (r *FogRenderer) SetVelocity(velocity geom.V) {
	r.Velocity = velocity
	r.wispVelocity = velocity
}

func (r *FogRenderer) Update() {
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	dt := 1 / float64(tps)
	r.Time += dt
	r.cloudOffset = r.cloudOffset.Add(r.Velocity.Mul(dt))
	r.wispOffset = r.wispOffset.Add(r.wispVelocity.Mul(dt))
}

// Cover visible section bands immediately, including unloaded bands, preserving the floor.
// Source coordinates and the section's world offset keep the noise continuous
// across seams, camera movement, resizing, and separate Draw calls in one tick.
func (r *FogRenderer) DrawSection(dst *ebiten.Image, top, cameraY float64, view RenderTransform) {
	pixels := view.Pixels
	op := &ebiten.DrawRectShaderOptions{Uniforms: map[string]any{
		"Pixels":      float32(pixels),
		"MinX":        float32(terrain.BackgroundMinX),
		"Top":         float32(top),
		"CloudOffset": []float32{float32(r.cloudOffset.X), float32(r.cloudOffset.Y)},
		"WispOffset":  []float32{float32(r.wispOffset.X), float32(r.wispOffset.Y)},
		"Strength":    float32(min(r.Strength, math.MaxFloat32)),
		"Horizontal":  float32(r.Orientation),
	}}
	op.GeoM.Translate(view.OffsetX+terrain.BackgroundMinX*float64(pixels), (top-cameraY)*float64(pixels))
	dst.DrawRectShader(2*pixels, pixels, r.shader, op)
}

// Close releases the fog shader on the game goroutine.
func (r *FogRenderer) Close() {
	if r.shader != nil {
		r.shader.Deallocate()
		r.shader = nil
	}
}
