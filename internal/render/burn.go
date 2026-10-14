package render

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const burnDuration = .7

type burnPatch struct {
	image  *ebiten.Image
	min    geom.V
	pixels int
	grass  bool
	age    float64
}

// BurnDissolve owns cropped decoration snapshots. Geometry and cached layers
// are already edited; these images exist only for the short visual transition.
type BurnDissolve struct {
	shader  *ebiten.Shader
	patches []burnPatch
}

func NewBurnDissolve() (*BurnDissolve, error) {
	shader, err := ebiten.NewShader(burnShaderSource)
	if err != nil {
		return nil, err
	}
	return &BurnDissolve{shader: shader}, nil
}

// Add takes ownership of an image at an internal world-space origin.
func (r *BurnDissolve) Add(img *ebiten.Image, min geom.V, pixels int, grass bool) {
	r.patches = append(r.patches, burnPatch{image: img, min: min, pixels: pixels, grass: grass})
}

func (r *BurnDissolve) Update() {
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	alive := r.patches[:0]
	for _, patch := range r.patches {
		patch.age += 1 / float64(tps)
		if patch.age >= burnDuration {
			patch.image.Deallocate()
		} else {
			alive = append(alive, patch)
		}
	}
	clear(r.patches[len(alive):])
	r.patches = alive
}

func (r *BurnDissolve) Draw(dst *ebiten.Image, cameraY float64, view RenderTransform, grass bool) {
	for _, patch := range r.patches {
		if patch.grass != grass {
			continue
		}
		size := patch.image.Bounds().Size()
		y := (patch.min.Y - cameraY) * float64(view.Pixels)
		scale := float64(view.Pixels) / float64(patch.pixels)
		if y+float64(size.Y)*scale <= 0 || y >= float64(dst.Bounds().Max.Y) {
			continue
		}
		op := &ebiten.DrawRectShaderOptions{
			Images: [4]*ebiten.Image{patch.image},
			Uniforms: map[string]any{
				"Progress": float32(patch.age / burnDuration),
				"Origin":   []float32{float32(patch.min.X), float32(patch.min.Y)},
				"Pixels":   float32(patch.pixels),
			},
		}
		op.GeoM.Scale(scale, scale)
		op.GeoM.Translate(view.OffsetX+patch.min.X*float64(view.Pixels), y)
		dst.DrawRectShader(size.X, size.Y, r.shader, op)
	}
}

func (r *BurnDissolve) Clear() {
	for _, patch := range r.patches {
		patch.image.Deallocate()
	}
	r.patches = nil
}

func (r *BurnDissolve) Close() {
	r.Clear()
	if r.shader != nil {
		r.shader.Deallocate()
		r.shader = nil
	}
}
