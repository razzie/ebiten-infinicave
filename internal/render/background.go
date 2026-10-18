package render

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type BackgroundRenderer struct {
	blur, composite, sweep, lighting  *ebiten.Shader
	background, mask, receiver        *ebiten.Image
	sweepWork                         [2]*ebiten.Image
	backgroundBlur, shadowBlur        blurBuffers
	softness, opacity, shadowSoftness float64
	Offset                            geom.V
}

// Each blur owns its buffers so different softness values never cause
// allocation churn. Large kernels run at reduced resolution with linear
// reconstruction; their sample spacing stays small instead of leaving bands.
type blurBuffers struct {
	work   [2]*ebiten.Image
	result *ebiten.Image
}

func NewBackgroundRenderer(config BackgroundConfig) (*BackgroundRenderer, error) {
	r := &BackgroundRenderer{softness: config.BackgroundBlur, opacity: config.ShadowOpacity,
		shadowSoftness: config.ShadowBlur, Offset: terrain.InternalPoint(config.Orientation, geom.V{Y: config.ShadowOffset})}
	var err error
	r.blur, err = ebiten.NewShader(blurShaderSource)
	if err != nil {
		return nil, err
	}
	r.composite, err = ebiten.NewShader(backgroundShaderSource)
	if err != nil {
		r.Close()
		return nil, err
	}
	r.sweep, err = ebiten.NewShader(shadowSweepShaderSource)
	if err != nil {
		r.Close()
		return nil, err
	}
	r.lighting, err = ebiten.NewShader(backgroundLightingShaderSource)
	if err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// DrawLighting bakes the direct-light share from the rendered facet normals.
// It uses the same face batches and raster policy as the material upload.
func (r *BackgroundRenderer) DrawLighting(dst *ebiten.Image, mesh TriangleMesh, light geom.V3) {
	dst.DrawTrianglesShader32(mesh.Vertices, mesh.Indices, r.lighting, &ebiten.DrawTrianglesShaderOptions{
		Uniforms: map[string]any{"Light": []float32{float32(light.X), float32(light.Y), float32(light.Z)}},
	})
}

func EnsureEffectImage(img **ebiten.Image, width, height int) {
	if *img != nil && (*img).Bounds().Size() == image.Pt(width, height) {
		return
	}
	if *img != nil {
		(*img).Deallocate()
	}
	*img = ebiten.NewImageWithOptions(image.Rect(0, 0, width, height), &ebiten.NewImageOptions{Unmanaged: true})
}

func (r *BackgroundRenderer) Draw(dst *ebiten.Image, w BackgroundLayers, y, height float64, view RenderTransform) {
	// Include the Gaussian kernel and the downsampling/reconstruction filters.
	// Include upstream casters on both axes, even outside the viewport.
	pixels := float64(view.Pixels)
	padding := math.Ceil(3*math.Max(r.softness, r.shadowSoftness)*pixels) + 2
	padX, padY := int(padding), int(padding)
	if r.opacity > 0 {
		padX += int(math.Ceil(math.Abs(r.Offset.X) * pixels))
		padY += int(math.Ceil(math.Abs(r.Offset.Y) * pixels))
	}
	paddedY := y - float64(padY)/pixels
	paddedHeight := height + 2*float64(padY)/pixels
	if visible, ok := w.(interface{ BackgroundVisible(float64, float64) bool }); ok && !visible.BackgroundVisible(paddedY, paddedHeight) {
		return
	}
	width, fullHeight := dst.Bounds().Dx()+2*padX, dst.Bounds().Dy()+2*padY
	view.OffsetX += float64(padX)
	EnsureEffectImage(&r.background, width, fullHeight)
	EnsureEffectImage(&r.mask, width, fullHeight)
	EnsureEffectImage(&r.receiver, width, fullHeight)
	r.background.Clear()
	r.mask.Clear()
	r.receiver.Clear()
	w.DrawBackground(r.background, paddedY, paddedHeight, view)
	if r.opacity > 0 {
		w.DrawForegroundSilhouette(r.mask, paddedY, paddedHeight, 0, view)
		if receivers, ok := w.(interface {
			DrawBackgroundLighting(*ebiten.Image, float64, float64, RenderTransform)
		}); ok {
			receivers.DrawBackgroundLighting(r.receiver, paddedY, paddedHeight, view)
		}
	}
	background := r.backgroundBlur.apply(r.background, r.softness*float64(view.Pixels), r.blur)
	shadow := r.mask
	if r.opacity > 0 {
		shadow = r.sweepShadow(r.mask, pixels)
		shadow = r.shadowBlur.apply(shadow, r.shadowSoftness*pixels, r.blur)
	}
	op := &ebiten.DrawRectShaderOptions{
		Images:   [4]*ebiten.Image{background, shadow, r.background, r.receiver},
		Uniforms: map[string]any{"Opacity": float32(r.opacity)},
	}
	op.GeoM.Translate(-float64(padX), -float64(padY))
	dst.DrawRectShader(width, fullHeight, r.composite, op)
}

// Sweep the silhouette continuously toward its projected edge. Each pass
// doubles the covered interval using max alpha, keeping thin casters opaque
// and preserving reveal opacity without darkening overlapping samples.
func (r *BackgroundRenderer) sweepShadow(src *ebiten.Image, pixels float64) *ebiten.Image {
	offset := r.Offset.Mul(pixels)
	length := math.Max(math.Abs(offset.X), math.Abs(offset.Y))
	if length == 0 {
		return src
	}
	samples := 2
	for float64(samples-1) < length {
		samples *= 2
	}
	step := offset.Mul(1 / float64(samples-1))
	size := src.Bounds().Size()
	for i := range r.sweepWork {
		EnsureEffectImage(&r.sweepWork[i], size.X, size.Y)
	}
	pass := 0
	for span := 1; span < samples; span *= 2 {
		dst := r.sweepWork[pass%2]
		dst.DrawRectShader(size.X, size.Y, r.sweep, &ebiten.DrawRectShaderOptions{
			Blend:    ebiten.BlendCopy,
			Images:   [4]*ebiten.Image{src},
			Uniforms: map[string]any{"Step": []float32{float32(step.X * float64(span)), float32(step.Y * float64(span))}},
		})
		src = dst
		pass++
	}
	return src
}

func (b *blurBuffers) apply(src *ebiten.Image, softness float64, shader *ebiten.Shader) *ebiten.Image {
	if softness == 0 {
		return src
	}
	size := src.Bounds().Size()
	scale := math.Min(1, 2/softness)
	width, height := max(1, int(math.Ceil(float64(size.X)*scale))), max(1, int(math.Ceil(float64(size.Y)*scale)))
	for i := range b.work {
		EnsureEffectImage(&b.work[i], width, height)
	}
	EnsureEffectImage(&b.result, size.X, size.Y)
	xScale, yScale := float64(width)/float64(size.X), float64(height)/float64(size.Y)
	op := &ebiten.DrawImageOptions{Blend: ebiten.BlendCopy, Filter: ebiten.FilterLinear}
	op.GeoM.Scale(xScale, yScale)
	b.work[0].DrawImage(src, op)
	for axis := 0; axis < 2; axis++ {
		step := []float32{float32(softness * xScale / 1.69), 0}
		if axis == 1 {
			step = []float32{0, float32(softness * yScale / 1.69)}
		}
		b.work[1-axis].DrawRectShader(width, height, shader, &ebiten.DrawRectShaderOptions{
			Blend:    ebiten.BlendCopy,
			Images:   [4]*ebiten.Image{b.work[axis]},
			Uniforms: map[string]any{"Step": step},
		})
	}
	op.GeoM.Reset()
	op.GeoM.Scale(1/xScale, 1/yScale)
	b.result.DrawImage(b.work[0], op)
	return b.result
}

func (b *blurBuffers) close() {
	for _, img := range []*ebiten.Image{b.work[0], b.work[1], b.result} {
		if img != nil {
			img.Deallocate()
		}
	}
}

func (r *BackgroundRenderer) Close() {
	r.backgroundBlur.close()
	r.shadowBlur.close()
	for _, img := range []*ebiten.Image{r.background, r.mask, r.receiver, r.sweepWork[0], r.sweepWork[1]} {
		if img != nil {
			img.Deallocate()
		}
	}
	for _, shader := range []*ebiten.Shader{r.blur, r.composite, r.sweep, r.lighting} {
		if shader != nil {
			shader.Deallocate()
		}
	}
}

func NewBackgroundImageAt(pixels int) *ebiten.Image {
	bounds := BackgroundRasterBounds(pixels)
	return ebiten.NewImageWithOptions(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), &ebiten.NewImageOptions{Unmanaged: true})
}

func BackgroundRasterBounds(pixels int) image.Rectangle {
	return scaleRasterBounds(image.Rect(int(terrain.BackgroundMinX*RasterPixelsPerUnit), 0, int(terrain.BackgroundMaxX*RasterPixelsPerUnit), RasterPixelsPerUnit), pixels)
}

// BackgroundConfig contains only the settings used by the background pass.
type BackgroundConfig struct {
	BackgroundBlur, ShadowOpacity, ShadowBlur float64
	ShadowOffset                              float64
	Orientation                               terrain.Orientation
}

// BackgroundLayers supplies the current cached terrain and shadow silhouette.
type BackgroundLayers interface {
	DrawBackground(*ebiten.Image, float64, float64, RenderTransform)
	DrawForegroundSilhouette(*ebiten.Image, float64, float64, float64, RenderTransform)
}
