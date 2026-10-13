package infinicave

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func (g *Scene) drawShapeRims(dst *ebiten.Image, geometry *terrain.Geometry, pixels int, rect image.Rectangle) {
	if geometry == nil || len(geometry.Cuts) == 0 {
		return
	}
	bounds := geom.Bounds{Min: V{X: float64(rect.Min.X)/float64(pixels) - .01, Y: float64(rect.Min.Y)/float64(pixels) - .01}, Max: V{X: float64(rect.Max.X)/float64(pixels) + .01, Y: float64(rect.Max.Y)/float64(pixels) + .01}}
	m := render.PrepareShapeRimsInBounds(geometry, g.view, bounds)
	m = render.RegionMesh(m, referenceRegion(rect, pixels, image.Pt(0, -pixels)))
	if len(m.Indices) == 0 {
		return
	}
	m = render.ScaleRegionMesh(m, pixels, image.Pt(0, -pixels))
	g.drawGridFaces(dst, m, terrain.SectionWindowTop(-geometry.Collision.ID))
}
