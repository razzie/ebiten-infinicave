package render

import "github.com/hajimehoshi/ebiten/v2"

const revealDuration = .7

// FadeRenderer blends arriving layers with their previously displayed image.
type FadeRenderer struct {
	shader *ebiten.Shader
}

func NewFadeRenderer() (*FadeRenderer, error) {
	shader, err := ebiten.NewShader(revealShaderSource)
	if err != nil {
		return nil, err
	}
	return &FadeRenderer{shader: shader}, nil
}

func (r *FadeRenderer) Close() {
	if r.shader != nil {
		r.shader.Deallocate()
		r.shader = nil
	}
}

// Reveal tracks an arriving cached layer. The zero value is fully revealed.
// Previous, when present, is owned by the transition. Independently published
// foreground products use separate clocks without retaining previous rocks.
type Reveal struct {
	Previous *ebiten.Image
	age      float64
	active   bool
}

// Start takes ownership of previous, which must match the new layer's size.
func (r *Reveal) Start(previous *ebiten.Image) {
	r.Clear()
	r.Previous = previous
	r.active = true
}

func (r *Reveal) Progress() float64 {
	if !r.active {
		return 1
	}
	return r.age / revealDuration
}

func (r *Reveal) Update() {
	if !r.active {
		return
	}
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	r.age += 1 / float64(tps)
	if r.age >= revealDuration {
		r.Clear()
	}
}

func (r *Reveal) Clear() {
	if r.Previous != nil {
		r.Previous.Deallocate()
	}
	*r = Reveal{}
}

// DrawReveal fades color and alpha together, preserving premultiplied colors.
// Existing pixels blend into the completed layer without losing opacity.
// progress may also include a supporting section's reveal for crossing plants.
func (r *FadeRenderer) DrawReveal(dst, layer *ebiten.Image, previous *ebiten.Image, progress float64, transform ebiten.GeoM) {
	if progress >= 1 || r == nil {
		dst.DrawImage(layer, &ebiten.DrawImageOptions{GeoM: transform})
		return
	}
	if previous == nil {
		op := &ebiten.DrawImageOptions{GeoM: transform}
		op.ColorScale.ScaleAlpha(float32(progress))
		dst.DrawImage(layer, op)
		return
	}
	size := layer.Bounds().Size()
	dst.DrawRectShader(size.X, size.Y, r.shader, &ebiten.DrawRectShaderOptions{
		GeoM:   transform,
		Images: [4]*ebiten.Image{layer, previous},
		Uniforms: map[string]any{
			"Progress": float32(progress),
		},
	})
}
