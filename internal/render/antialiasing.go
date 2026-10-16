package render

import (
	_ "embed"
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
)

// AntialiasingMode selects geometry antialiasing or a final scene filter.
type AntialiasingMode uint8

const (
	AntialiasingFull AntialiasingMode = iota
	AntialiasingOff
	AntialiasingFXAA
)

func (m AntialiasingMode) String() string {
	switch m {
	case AntialiasingFull:
		return "full"
	case AntialiasingOff:
		return "off"
	case AntialiasingFXAA:
		return "fxaa"
	default:
		return fmt.Sprintf("AntialiasingMode(%d)", m)
	}
}

func ParseAntialiasingMode(value string) (AntialiasingMode, error) {
	switch value {
	case "", "full":
		return AntialiasingFull, nil
	case "off":
		return AntialiasingOff, nil
	case "fxaa":
		return AntialiasingFXAA, nil
	default:
		return AntialiasingFull, fmt.Errorf("infinicave: unknown antialiasing mode %q: use full, off, or fxaa", value)
	}
}

// GeometryAA reports whether triangle/path draws should enable antialiasing.
func (m AntialiasingMode) GeometryAA() bool { return m == AntialiasingFull }

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
