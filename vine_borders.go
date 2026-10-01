package main

import "math"

// Beyond one cell's width, let the existing terrain steering find a seam.
// Exact segment distances in this narrow band avoid raster stair-step turns.
const vineBorderRange = 32.0

func newVineBorders(background RockGrid) []float64 {
	distances := make([]float64, vineFieldWidth*vineFieldHeight)
	for i := range distances {
		distances[i] = vineBorderRange * vineBorderRange
	}
	seen := make(map[[4]int64]bool)
	for _, cell := range background {
		for i, a := range cell.Polygon {
			b := cell.Polygon[(i+1)%len(cell.Polygon)]
			// Window clipping edges are not seams in the world tessellation.
			if (a.X == b.X && (a.X == 0 || a.X == W)) || (a.Y == b.Y && (a.Y == 0 || a.Y == H)) {
				continue
			}
			key := edgeKey(a, b)
			d := b.Sub(a)
			if seen[key] || d.Len2() < 1e-12 {
				continue
			}
			seen[key] = true
			invLength2 := 1 / d.Len2()
			minX := max(0, int(math.Floor((math.Min(a.X, b.X)-vineBorderRange)/vineFieldStep)))
			maxX := min(vineFieldWidth-1, int(math.Ceil((math.Max(a.X, b.X)+vineBorderRange)/vineFieldStep)))
			minY := max(0, int(math.Floor((math.Min(a.Y, b.Y)-vineBorderRange)/vineFieldStep)))
			maxY := min(vineFieldHeight-1, int(math.Ceil((math.Max(a.Y, b.Y)+vineBorderRange)/vineFieldStep)))
			for y := minY; y <= maxY; y++ {
				for x := minX; x <= maxX; x++ {
					p := V{(float64(x) + .5) * vineFieldStep, (float64(y) + .5) * vineFieldStep}
					t := clamp(p.Sub(a).Dot(d)*invLength2, 0, 1)
					distance2 := p.Sub(a.Add(d.Mul(t))).Len2()
					index := y*vineFieldWidth + x
					distances[index] = math.Min(distances[index], distance2)
				}
			}
		}
	}
	for i := range distances {
		distances[i] = math.Sqrt(distances[i])
	}
	return distances
}

// Bilinear sampling gives growth a smooth attraction across field pixels.
func (f *VineTerrain) borderDistance(p V) float64 {
	x := clamp(p.X/vineFieldStep-.5, 0, float64(vineFieldWidth-1))
	y := clamp(p.Y/vineFieldStep-.5, 0, float64(vineFieldHeight-1))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, vineFieldWidth-1), min(y0+1, vineFieldHeight-1)
	a := lerp(f.borders[y0*vineFieldWidth+x0], f.borders[y0*vineFieldWidth+x1], x-float64(x0))
	b := lerp(f.borders[y1*vineFieldWidth+x0], f.borders[y1*vineFieldWidth+x1], x-float64(x0))
	return lerp(a, b, y-float64(y0))
}
