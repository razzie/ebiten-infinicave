package main

import (
	_ "embed"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave"
)

const (
	guideHoverRadius = .006
	hoverPadding     = .016
)

//go:embed hover.kage
var hoverShaderSource []byte

// Object identity excludes the hit point so moving within a formation reuses
// its overlay. GeometryRevision also catches growth and merges during streaming.
type hoverTarget struct {
	kind      infinicave.TargetMask
	formation infinicave.FormationID
	guide     infinicave.GuideID
}

type hoverRenderer struct {
	shader    *ebiten.Shader
	target    hoverTarget
	image     *ebiten.Image
	origin    infinicave.V // world coordinates
	revision  uint64
	pixels    int
	low, high int64
}

func newHoverRenderer() (*hoverRenderer, error) {
	shader, err := ebiten.NewShader(hoverShaderSource)
	if err != nil {
		return nil, err
	}
	return &hoverRenderer{shader: shader}, nil
}

func (r *hoverRenderer) clear() {
	if r.image != nil {
		r.image.Deallocate()
		r.image = nil
	}
	r.target = hoverTarget{}
}

func (r *hoverRenderer) close() {
	r.clear()
	r.shader.Deallocate()
}

func hoverAt(scene *infinicave.Scene, cursor infinicave.V, viewport infinicave.Viewport, pixels int) hoverTarget {
	if cursor.X < 0 || cursor.X >= infinicave.Width || cursor.Y < 0 || cursor.Y >= viewport.Height {
		return hoverTarget{}
	}
	point := cursor.Add(infinicave.V{Y: viewerCameraY(viewport.Y, pixels)})
	if point.Y >= 0 {
		return hoverTarget{}
	}
	result, err := scene.Query(infinicave.Ray{Origin: point}, infinicave.QueryOptions{GuideRadius: guideHoverRadius})
	if err != nil || !result.Found || !result.Complete {
		return hoverTarget{}
	}
	return hoverTarget{kind: result.Hit.Kind, formation: result.Hit.FormationID, guide: result.Hit.GuideID}
}

func (r *hoverRenderer) selectTarget(target hoverTarget, scene *infinicave.Scene, pixels int, low, high int64) {
	revision := scene.GeometryRevision()
	if target == r.target && (target.kind == 0 || (r.revision == revision && r.pixels == pixels && r.low == low && r.high == high)) {
		return
	}
	r.clear()
	var polygons [][]infinicave.V
	switch target.kind {
	case infinicave.TargetRock:
		formation, available := scene.Formation(target.formation)
		if !available {
			return
		}
		polygons = formation.Polygons
	case infinicave.TargetGuide:
		guide, available := scene.Guide(target.guide)
		if !available {
			return
		}
		polygons = [][]infinicave.V{guide.Points}
	default:
		return
	}
	r.target = target
	r.revision, r.pixels, r.low, r.high = revision, pixels, low, high
	r.render(polygons)
}

func (r *hoverRenderer) render(polygons [][]infinicave.V) {
	if len(polygons) == 0 {
		return
	}
	lo, hi := polygons[0][0], polygons[0][0]
	for _, poly := range polygons {
		for _, p := range poly {
			lo.X, lo.Y = math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)
			hi.X, hi.Y = math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)
		}
	}
	// Raster cropping preserves concave boundaries and hole winding, while
	// bounding GPU memory to the visible section bands and their glow margin.
	lo.Y = math.Max(lo.Y, -float64(r.high+1)*infinicave.SectionHeight)
	hi.Y = math.Min(hi.Y, math.Min(0, -float64(r.low)*infinicave.SectionHeight))
	if hi.Y < lo.Y {
		return
	}
	pixels := float64(r.pixels)
	r.origin = infinicave.V{X: math.Floor((lo.X-hoverPadding)*pixels+1e-9) / pixels,
		Y: math.Floor((lo.Y-hoverPadding)*pixels+1e-9) / pixels}
	width := max(1, int(math.Ceil((hi.X+hoverPadding-r.origin.X)*pixels-1e-9)))
	height := max(1, int(math.Ceil((hi.Y+hoverPadding-r.origin.Y)*pixels-1e-9)))
	mask := ebiten.NewImage(width, height)
	defer mask.Deallocate()
	var path vector.Path
	for _, poly := range polygons {
		path.MoveTo(float32((poly[0].X-r.origin.X)*pixels), float32((poly[0].Y-r.origin.Y)*pixels))
		for _, p := range poly[1:] {
			path.LineTo(float32((p.X-r.origin.X)*pixels), float32((p.Y-r.origin.Y)*pixels))
		}
		if r.target.kind == infinicave.TargetRock {
			path.Close()
		}
	}
	if r.target.kind == infinicave.TargetGuide {
		var stroke vector.Path
		stroke.AddStroke(&path, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{
			Width: 2, LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound,
		}})
		path = stroke
	}
	vector.FillPath(mask, &path, &vector.FillOptions{FillRule: vector.FillRuleNonZero}, &vector.DrawPathOptions{AntiAlias: true})

	// Blur only when the target, geometry, resolution, or visible bands change.
	blur := ebiten.NewImage(width, height)
	defer blur.Deallocate()
	blur.DrawRectShader(width, height, r.shader, &ebiten.DrawRectShaderOptions{
		Images:   [4]*ebiten.Image{mask},
		Uniforms: map[string]any{"Final": float32(0)},
	})
	strength := []float32{.10, .16}
	if r.target.kind == infinicave.TargetGuide {
		strength = []float32{.7, .7}
	}
	r.image = ebiten.NewImage(width, height)
	r.image.DrawRectShader(width, height, r.shader, &ebiten.DrawRectShaderOptions{
		Images:   [4]*ebiten.Image{mask, blur},
		Uniforms: map[string]any{"Final": float32(1), "Strength": strength},
	})
}

func (g *Game) drawHover(screen *ebiten.Image) {
	r := g.highlight
	if r == nil {
		return
	}
	if !ebiten.IsFocused() {
		r.clear()
		return
	}
	x, y := ebiten.CursorPositionF()
	pixels := g.renderPixels()
	scale := float64(infinicave.Width) / float64(pixels)
	viewport := g.viewport()
	cameraY := viewerCameraY(viewport.Y, pixels)
	low := max(0, int64(math.Floor(-(cameraY+viewport.Height)/infinicave.SectionHeight)))
	high := max(low, int64(math.Ceil(-cameraY/infinicave.SectionHeight))-1)
	target := hoverAt(g.scene, infinicave.V{X: (x - g.renderOffsetX()) * scale, Y: y * scale}, viewport, pixels)
	r.selectTarget(target, g.scene, pixels, low, high)
	if r.image != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(g.renderOffsetX()+r.origin.X*float64(pixels), (r.origin.Y-cameraY)*float64(pixels))
		screen.DrawImage(r.image, op)
	}
}
