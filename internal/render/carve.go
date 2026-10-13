package render

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Erase cut coverage directly from a finished texture, including shadows and
// surface decorations. Clip to the target image or dirty subimage before drawing.
func EraseCutsAt(dst *ebiten.Image, bounds image.Rectangle, windowTop float64, cuts []terrain.RockCut, pixels int) {
	if dst == nil || bounds.Empty() {
		return
	}
	target := dst.Bounds().Add(bounds.Min)
	lo := geom.V{X: float64(target.Min.X) / float64(pixels), Y: windowTop + float64(target.Min.Y)/float64(pixels)}
	hi := geom.V{X: float64(target.Max.X) / float64(pixels), Y: windowTop + float64(target.Max.Y)/float64(pixels)}
	// Clip beyond the subimage's edge so antialiasing cannot introduce a seam
	// along a dirty tile boundary inside an otherwise fully erased cut.
	pad := geom.V{X: 2 / float64(pixels), Y: 2 / float64(pixels)}
	for _, cut := range cuts {
		// Clip before converting to float32 pixels, including very large cuts.
		if cut.Max.X <= lo.X || cut.Min.X >= hi.X || cut.Max.Y <= lo.Y || cut.Min.Y >= hi.Y {
			continue
		}
		poly := geom.ClipHalfPlane(cut.Poly, geom.V{X: -1, Y: 0}, -lo.X+pad.X)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 1, Y: 0}, hi.X+pad.X)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 0, Y: -1}, -lo.Y+pad.Y)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 0, Y: 1}, hi.Y+pad.Y)
		if len(poly) < 3 {
			continue
		}
		var path vector.Path
		for i, p := range poly {
			x, y := float32(p.X*float64(pixels)-float64(bounds.Min.X)), float32((p.Y-windowTop)*float64(pixels)-float64(bounds.Min.Y))
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		path.Close()
		vector.FillPath(dst, &path, nil, &vector.DrawPathOptions{AntiAlias: true, Blend: ebiten.BlendDestinationOut})
	}
}
