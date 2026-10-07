package infinicave

import (
	_ "embed"

	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed fog.kage
var fogShaderSource []byte

type fogRenderer struct {
	orientation Orientation
	shader      *ebiten.Shader
	time        float64
}

func newFogRenderer(orientation ...Orientation) (*fogRenderer, error) {
	shader, err := ebiten.NewShader(fogShaderSource)
	if err != nil {
		return nil, err
	}
	return &fogRenderer{orientation: optionalOrientation(orientation), shader: shader}, nil
}

func (r *fogRenderer) update() {
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	r.time += 1 / float64(tps)
}

// Only cover loaded section bands, preserving untouched gaps and the floor.
// Source coordinates and the section's world offset keep the noise continuous
// across seams, camera movement, resizing, and separate Draw calls in one tick.
func (r *fogRenderer) drawSection(dst *ebiten.Image, top, cameraY float64, view renderTransform) {
	pixels := view.pixels
	op := &ebiten.DrawRectShaderOptions{Uniforms: map[string]any{
		"Pixels":     float32(pixels),
		"MinX":       float32(backgroundMinX),
		"Top":        float32(top),
		"Time":       float32(r.time),
		"Horizontal": float32(r.orientation),
	}}
	op.GeoM.Translate(view.offsetX+backgroundMinX*float64(pixels), (top-cameraY)*float64(pixels))
	dst.DrawRectShader(2*pixels, pixels, r.shader, op)
}
