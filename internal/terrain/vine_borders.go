package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Beyond one cell's width, let the existing terrain steering find a seam.
// Exact segment distances in this narrow band avoid raster stair-step turns.
const vineBorderRange = .032

func newVineBorders(background RockGrid) []float64 {
	return vineBordersForEdges(newVineEdgeGraph(background), nil)
}

// Seed exact distances in a narrow band around each seam, then propagate
// them in two linear scans. The graph still supplies exact twig geometry;
// the field is only a smooth steering attraction, capped at one cell width.
func vineBordersForEdges(graph *vineEdgeGraph, workspace *vineWorkspace) []float64 {
	distances := workspace.take()
	for i := range distances {
		distances[i] = vineBorderRange
	}
	const reach = 2 * vineFieldStep
	for _, edge := range graph.edges {
		a, b := graph.points[edge.a], graph.points[edge.b]
		d := b.Sub(a)
		inverse := 1 / d.Len2()
		x0 := max(0, int(math.Floor((min(a.X, b.X)-reach)/vineFieldStep)))
		x1 := min(vineFieldWidth-1, int(math.Ceil((max(a.X, b.X)+reach)/vineFieldStep)))
		y0 := max(0, int(math.Floor((min(a.Y, b.Y)-reach-GenerationMinY)/vineFieldStep)))
		y1 := min(vineFieldHeight-1, int(math.Ceil((max(a.Y, b.Y)+reach-GenerationMinY)/vineFieldStep)))
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				p := geom.V{X: (float64(x) + .5) * vineFieldStep, Y: GenerationMinY + (float64(y)+.5)*vineFieldStep}
				t := geom.Clamp(p.Sub(a).Dot(d)*inverse, 0, 1)
				distance := p.Sub(a.Add(d.Mul(t))).Len()
				i := y*vineFieldWidth + x
				distances[i] = min(distances[i], distance)
			}
		}
	}
	chamferVineField(distances)
	return distances
}

// Bilinear sampling gives growth a smooth attraction across field pixels.
func (f *VineTerrain) borderDistance(p geom.V) float64 {
	x := geom.Clamp(p.X/vineFieldStep-.5, 0, float64(vineFieldWidth-1))
	y := geom.Clamp((p.Y-GenerationMinY)/vineFieldStep-.5, 0, float64(vineFieldHeight-1))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, vineFieldWidth-1), min(y0+1, vineFieldHeight-1)
	a := geom.Lerp(f.borders[y0*vineFieldWidth+x0], f.borders[y0*vineFieldWidth+x1], x-float64(x0))
	b := geom.Lerp(f.borders[y1*vineFieldWidth+x0], f.borders[y1*vineFieldWidth+x1], x-float64(x0))
	return geom.Lerp(a, b, y-float64(y0))
}
