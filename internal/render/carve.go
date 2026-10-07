package render

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Erase the full ribbon and its shadow after rasterization/blur. Centerline
// clipping alone would leave wide vine edges protruding into the opening.
func EraseVineCutsAt(dst *ebiten.Image, bounds image.Rectangle, windowTop float64, cuts []terrain.RockCut, pixels int) {
	if dst == nil || bounds.Empty() {
		return
	}
	for _, cut := range cuts {
		// Clip before converting to float32 pixels, including very large cuts.
		lo := geom.V{X: float64(bounds.Min.X) / float64(pixels), Y: windowTop + float64(bounds.Min.Y)/float64(pixels)}
		hi := geom.V{X: float64(bounds.Max.X) / float64(pixels), Y: windowTop + float64(bounds.Max.Y)/float64(pixels)}
		poly := geom.ClipHalfPlane(cut.Poly, geom.V{X: -1, Y: 0}, -lo.X)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 1, Y: 0}, hi.X)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 0, Y: -1}, -lo.Y)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 0, Y: 1}, hi.Y)
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
