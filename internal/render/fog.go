package render

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type FogRenderer struct {
	Orientation terrain.Orientation
	shader      *ebiten.Shader
	Time        float64
}

func NewFogRenderer(orientation ...terrain.Orientation) (*FogRenderer, error) {
	shader, err := ebiten.NewShader(fogShaderSource)
	if err != nil {
		return nil, err
	}
	return &FogRenderer{Orientation: terrain.OptionalOrientation(orientation), shader: shader}, nil
}

func (r *FogRenderer) Update() {
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	r.Time += 1 / float64(tps)
}

// Only cover loaded section bands, preserving untouched gaps and the floor.
// Source coordinates and the section's world offset keep the noise continuous
// across seams, camera movement, resizing, and separate Draw calls in one tick.
func (r *FogRenderer) DrawSection(dst *ebiten.Image, top, cameraY float64, view RenderTransform) {
	pixels := view.Pixels
	op := &ebiten.DrawRectShaderOptions{Uniforms: map[string]any{
		"Pixels":     float32(pixels),
		"MinX":       float32(terrain.BackgroundMinX),
		"Top":        float32(top),
		"Time":       float32(r.Time),
		"Horizontal": float32(r.Orientation),
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
