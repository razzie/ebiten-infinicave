package render

import (
	_ "embed"

	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed shaders/fxaa.kage
var fxaaShaderSource []byte

// FXAARenderer filters a complete scene once at its final output resolution.
// All methods belong on the game goroutine.
type FXAARenderer struct {
	shader *ebiten.Shader
	target *ebiten.Image
}

func NewFXAARenderer() (*FXAARenderer, error) {
	shader, err := ebiten.NewShader(fxaaShaderSource)
	if err != nil {
		return nil, err
	}
	return &FXAARenderer{shader: shader}, nil
}

func (r *FXAARenderer) Begin(dst *ebiten.Image) *ebiten.Image {
	size := dst.Bounds().Size()
	EnsureEffectImage(&r.target, size.X, size.Y)
	r.target.Clear()
	return r.target
}

func (r *FXAARenderer) Draw(dst *ebiten.Image) {
	bounds := dst.Bounds()
	op := &ebiten.DrawRectShaderOptions{Images: [4]*ebiten.Image{r.target}}
	op.GeoM.Translate(float64(bounds.Min.X), float64(bounds.Min.Y))
	dst.DrawRectShader(bounds.Dx(), bounds.Dy(), r.shader, op)
}

func (r *FXAARenderer) Close() {
	if r.target != nil {
		r.target.Deallocate()
		r.target = nil
	}
	if r.shader != nil {
		r.shader.Deallocate()
		r.shader = nil
	}
}
