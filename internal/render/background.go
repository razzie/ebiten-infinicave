package render

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type BackgroundRenderer struct {
	blur, composite                   *ebiten.Shader
	background, mask                  *ebiten.Image
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
		shadowSoftness: config.ShadowBlur, Offset: config.ShadowOffset}
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
	return r, nil
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
	width := dst.Bounds().Dx()
	// Include the Gaussian kernel and the downsampling/reconstruction filters.
	// Neighboring bands prevent viewport edges from clipping soft shadows.
	padding := int(math.Ceil(3*math.Max(r.softness, r.shadowSoftness)*float64(view.Pixels))) + 2
	paddedY := y - float64(padding)/float64(view.Pixels)
	paddedHeight := height + 2*float64(padding)/float64(view.Pixels)
	if visible, ok := w.(interface{ BackgroundVisible(float64, float64) bool }); ok && !visible.BackgroundVisible(paddedY, paddedHeight) {
		return
	}
	fullHeight := dst.Bounds().Dy() + 2*padding
	EnsureEffectImage(&r.background, width, fullHeight)
	EnsureEffectImage(&r.mask, width, fullHeight)
	r.background.Clear()
	r.mask.Clear()
	w.DrawBackground(r.background, paddedY, paddedHeight, view)
	if r.opacity > 0 {
		w.DrawForegroundRocks(r.mask, paddedY-r.Offset.Y, paddedHeight, r.Offset.X, view)
	}
	background := r.backgroundBlur.apply(r.background, r.softness*float64(view.Pixels), r.blur)
	shadow := r.mask
	if r.opacity > 0 {
		shadow = r.shadowBlur.apply(r.mask, r.shadowSoftness*float64(view.Pixels), r.blur)
	}
	op := &ebiten.DrawRectShaderOptions{
		Images:   [4]*ebiten.Image{background, shadow, r.background},
		Uniforms: map[string]any{"Opacity": float32(r.opacity)},
	}
	op.GeoM.Translate(0, -float64(padding))
	dst.DrawRectShader(width, fullHeight, r.composite, op)
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
	for _, img := range []*ebiten.Image{r.background, r.mask} {
		if img != nil {
			img.Deallocate()
		}
	}
	for _, shader := range []*ebiten.Shader{r.blur, r.composite} {
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
	ShadowOffset                              geom.V
}

// BackgroundLayers supplies the current cached terrain and shadow silhouette.
type BackgroundLayers interface {
	DrawBackground(*ebiten.Image, float64, float64, RenderTransform)
	DrawForegroundRocks(*ebiten.Image, float64, float64, float64, RenderTransform)
}
