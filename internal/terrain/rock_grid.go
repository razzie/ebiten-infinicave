package terrain

import (
	"image/color"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// RockCell is shared by rendering and vine growth; colors belong to entire
// Voronoi faces, not a second approximation of the original noise field.
type RockCell struct {
	orientation     Orientation // Lighting basis for the internal section frame.
	worldTop        float64     // Stable material origin across padded windows.
	Center          geom.V
	Polygon         []geom.V
	Color           color.NRGBA
	Z               float64 // height toward the camera at the control point
	Normal          geom.V3
	Raised          bool
	Shadow, Ambient float64
}

type RockGrid []RockCell

func newRockGrid(seeds []geom.V, colorAt func(geom.V) color.NRGBA) RockGrid {
	return newRockGridInRange(seeds, colorAt, 0, generationWidth)
}

func newRockGridInRange(seeds []geom.V, colorAt func(geom.V) color.NRGBA, minX, maxX float64) RockGrid {
	cells := voronoiCellsInRange(seeds, minX, maxX)
	grid := make(RockGrid, 0, len(seeds))
	for i, p := range seeds {
		clr := colorAt(p)
		if clr.A != 0 {
			grid = append(grid, RockCell{Center: p, Polygon: cells[i], Color: clr})
		}
	}
	return grid
}

func InsetForegroundGrid(grid RockGrid) RockGrid {
	clipped := make(RockGrid, 0, len(grid))
	for _, cell := range grid {
		poly := geom.ClipHalfPlane(cell.Polygon, geom.V{X: -1, Y: 0}, -foregroundScreenInset)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 1, Y: 0}, generationWidth-foregroundScreenInset)
		if len(poly) < 3 || geom.PolygonArea(poly) < 1e-15 {
			continue
		}
		cell.Polygon = poly
		if !geom.InsidePolygon(cell.Center, poly) {
			cell.Center = geom.PolygonCenter(poly)
		}
		clipped = append(clipped, cell)
	}
	return clipped
}

// RockOrientation returns the lighting basis of a generated rock face.
func RockOrientation(cell RockCell) Orientation { return cell.orientation }

// RockMaterialPoint supplies world coordinates for deterministic face detail.
func RockMaterialPoint(cell RockCell, p geom.V) geom.V {
	p.Y += cell.worldTop
	return WorldPoint(cell.orientation, p)
}

// WithRockOrientation selects a lighting basis for an internal diagnostic face.
func WithRockOrientation(cell RockCell, orientation Orientation) RockCell {
	cell.orientation = orientation
	return cell
}
