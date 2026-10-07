package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const rockDepthStep = .004

const rockDepthWidth = int(generationWidth / rockDepthStep)

const rockDepthHeight = int(GenerationHeight / rockDepthStep)

type rockDepth struct {
	heights []float64
	light   geom.V3
}

// Each face remains one plane, including concave faces. Clamp extrapolation
// at long polygon tips so fitted slopes cannot create implausible spikes.
func RockDepthAt(c RockCell, p geom.V) float64 {
	if c.Normal.Z < .05 {
		return c.Z
	}
	d := p.Sub(c.Center)
	z := c.Z - (d.X*c.Normal.X+d.Y*c.Normal.Y)/c.Normal.Z
	if c.Raised {
		return geom.Clamp(z, math.Max(.001, c.Z-.028), c.Z+.028)
	}
	return geom.Clamp(z, c.Z-.004, c.Z+.004)
}

// Rasterize real faces into a shared height buffer. world-aligned samples and
// the generation padding keep shadow queries identical across section seams.
func newRockDepth(grids ...RockGrid) *rockDepth {
	d := &rockDepth{heights: make([]float64, rockDepthWidth*rockDepthHeight)}
	d.light = rockLight
	for _, grid := range grids {
		if len(grid) > 0 {
			light := InternalPoint(grid[0].orientation, geom.V{X: rockLight.X, Y: rockLight.Y})
			d.light = geom.V3{X: light.X, Y: light.Y, Z: rockLight.Z}
			break
		}
	}
	for i := range d.heights {
		d.heights[i] = -.016
	}
	for _, grid := range grids {
		for _, c := range grid {
			minY, maxY := float64(generationMaxY), float64(GenerationMinY)
			for _, p := range c.Polygon {
				minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			}
			var crossings []float64
			for y := max(0, int((minY-GenerationMinY)/rockDepthStep)); y < min(rockDepthHeight, int((maxY-GenerationMinY)/rockDepthStep)+1); y++ {
				py := GenerationMinY + (float64(y)+.5)*rockDepthStep
				crossings = crossings[:0]
				for j, a := range c.Polygon {
					b := c.Polygon[(j+1)%len(c.Polygon)]
					if (a.Y <= py && b.Y > py) || (b.Y <= py && a.Y > py) {
						crossings = append(crossings, a.X+(b.X-a.X)*(py-a.Y)/(b.Y-a.Y))
					}
				}
				sort.Float64s(crossings)
				for j := 0; j+1 < len(crossings); j += 2 {
					for x := max(0, int(math.Ceil(crossings[j]/rockDepthStep-.5))); x < min(rockDepthWidth, int(math.Ceil(crossings[j+1]/rockDepthStep-.5))); x++ {
						p := geom.V{X: (float64(x) + .5) * rockDepthStep, Y: py}
						index := y*rockDepthWidth + x
						d.heights[index] = math.Max(d.heights[index], RockDepthAt(c, p))
					}
				}
			}
		}
	}
	return d
}

func (d *rockDepth) at(p geom.V) float64 {
	if p.X < 0 || p.X >= generationWidth || p.Y < GenerationMinY || p.Y >= generationMaxY {
		return -.016
	}
	return d.heights[int((p.Y-GenerationMinY)/rockDepthStep)*rockDepthWidth+int(p.X/rockDepthStep)]
}

func (d *rockDepth) visibility(p geom.V, z float64) float64 {
	direction := (geom.V{X: d.light.X, Y: d.light.Y}).Norm()
	rise := d.light.Z / math.Hypot(d.light.X, d.light.Y)
	visible := 1.0
	for distance := .008; distance <= .184; distance += .004 {
		blocker := d.at(p.Add(direction.Mul(distance))) - z - distance*rise
		// A small bias avoids self-shadow acne; increasing softness models a
		// finite light source and leaves a sharper contact near the blocker.
		visible = math.Min(visible, 1-geom.Smoothstep(.002, .005+distance*.05, blocker))
		if visible == 0 {
			break
		}
	}
	return visible
}

func (d *rockDepth) ambient(p geom.V, z float64) float64 {
	occlusion := 0.0
	for i := 0; i < 8; i++ {
		angle := float64(i) * math.Pi / 4
		direction := geom.V{X: math.Cos(angle), Y: math.Sin(angle)}
		horizon := 0.0
		for _, distance := range []float64{.008, .020, .040} {
			horizon = math.Max(horizon, (d.at(p.Add(direction.Mul(distance)))-z-.003)/distance)
		}
		occlusion += geom.Clamp(horizon, 0, 1)
	}
	return 1 - .75*occlusion/8
}

func (d *rockDepth) illumination(c RockCell) (shadow, ambient float64) {
	// Sample within the face, averaging visibility without introducing fan
	// triangles into the lighting. The polygon receives one flat-shaded tone.
	points := []geom.V{c.Center}
	for _, p := range c.Polygon {
		q := geom.LerpVector(c.Center, p, .6)
		if geom.InsidePolygon(q, c.Polygon) {
			points = append(points, q)
		}
	}
	for _, p := range points {
		z := RockDepthAt(c, p)
		shadow += d.visibility(p, z)
		ambient += d.ambient(p, z)
	}
	return shadow / float64(len(points)), ambient / float64(len(points))
}
