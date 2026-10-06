package infinicave

import (
	_ "embed"

	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed fog.kage
var fogShaderSource []byte

type fogRenderer struct {
	shader *ebiten.Shader
	time   float64
}

func newFogRenderer() (*fogRenderer, error) {
	shader, err := ebiten.NewShader(fogShaderSource)
	if err != nil {
		return nil, err
	}
	return &fogRenderer{shader: shader}, nil
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
func (r *fogRenderer) drawSection(dst *ebiten.Image, top, cameraY float64) {
	pixels := dst.Bounds().Dx()
	op := &ebiten.DrawRectShaderOptions{Uniforms: map[string]any{
		"Pixels": float32(pixels),
		"Top":    float32(top),
		"Time":   float32(r.time),
	}}
	op.GeoM.Translate(0, (top-cameraY)*float64(pixels))
	dst.DrawRectShader(pixels, pixels, r.shader, op)
}
