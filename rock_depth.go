package infinicave

import (
	"math"
	"sort"
)

const rockDepthStep = .004
const rockDepthWidth = int(generationWidth / rockDepthStep)
const rockDepthHeight = int(generationHeight / rockDepthStep)

type rockDepth struct {
	heights []float64
	light   V3
}

// Each face remains one plane, including concave faces. Clamp extrapolation
// at long polygon tips so fitted slopes cannot create implausible spikes.
func (c RockCell) depthAt(p V) float64 {
	if c.Normal.Z < .05 {
		return c.Z
	}
	d := p.Sub(c.Center)
	z := c.Z - (d.X*c.Normal.X+d.Y*c.Normal.Y)/c.Normal.Z
	if c.Raised {
		return clamp(z, math.Max(.001, c.Z-.028), c.Z+.028)
	}
	return clamp(z, c.Z-.004, c.Z+.004)
}

// Rasterize real faces into a shared height buffer. world-aligned samples and
// the generation padding keep shadow queries identical across section seams.
func newRockDepth(grids ...RockGrid) *rockDepth {
	d := &rockDepth{heights: make([]float64, rockDepthWidth*rockDepthHeight)}
	d.light = rockLight
	for _, grid := range grids {
		if len(grid) > 0 {
			light := grid[0].orientation.internal(V{rockLight.X, rockLight.Y})
			d.light = V3{light.X, light.Y, rockLight.Z}
			break
		}
	}
	for i := range d.heights {
		d.heights[i] = -.016
	}
	for _, grid := range grids {
		for _, c := range grid {
			minY, maxY := float64(generationMaxY), float64(generationMinY)
			for _, p := range c.Polygon {
				minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			}
			var crossings []float64
			for y := max(0, int((minY-generationMinY)/rockDepthStep)); y < min(rockDepthHeight, int((maxY-generationMinY)/rockDepthStep)+1); y++ {
				py := generationMinY + (float64(y)+.5)*rockDepthStep
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
						p := V{(float64(x) + .5) * rockDepthStep, py}
						index := y*rockDepthWidth + x
						d.heights[index] = math.Max(d.heights[index], c.depthAt(p))
					}
				}
			}
		}
	}
	return d
}

func (d *rockDepth) at(p V) float64 {
	if p.X < 0 || p.X >= generationWidth || p.Y < generationMinY || p.Y >= generationMaxY {
		return -.016
	}
	return d.heights[int((p.Y-generationMinY)/rockDepthStep)*rockDepthWidth+int(p.X/rockDepthStep)]
}

func (d *rockDepth) visibility(p V, z float64) float64 {
	direction := (V{d.light.X, d.light.Y}).Norm()
	rise := d.light.Z / math.Hypot(d.light.X, d.light.Y)
	visible := 1.0
	for distance := .008; distance <= .184; distance += .004 {
		blocker := d.at(p.Add(direction.Mul(distance))) - z - distance*rise
		// A small bias avoids self-shadow acne; increasing softness models a
		// finite light source and leaves a sharper contact near the blocker.
		visible = math.Min(visible, 1-smoothstep(.002, .005+distance*.05, blocker))
		if visible == 0 {
			break
		}
	}
	return visible
}

func (d *rockDepth) ambient(p V, z float64) float64 {
	occlusion := 0.0
	for i := 0; i < 8; i++ {
		angle := float64(i) * math.Pi / 4
		direction := V{math.Cos(angle), math.Sin(angle)}
		horizon := 0.0
		for _, distance := range []float64{.008, .020, .040} {
			horizon = math.Max(horizon, (d.at(p.Add(direction.Mul(distance)))-z-.003)/distance)
		}
		occlusion += clamp(horizon, 0, 1)
	}
	return 1 - .75*occlusion/8
}

func (d *rockDepth) illumination(c RockCell) (shadow, ambient float64) {
	// Sample within the face, averaging visibility without introducing fan
	// triangles into the lighting. The polygon receives one flat-shaded tone.
	points := []V{c.Center}
	for _, p := range c.Polygon {
		q := lerpV(c.Center, p, .6)
		if insideFace(q, c.Polygon) {
			points = append(points, q)
		}
	}
	for _, p := range points {
		z := c.depthAt(p)
		shadow += d.visibility(p, z)
		ambient += d.ambient(p, z)
	}
	return shadow / float64(len(points)), ambient / float64(len(points))
}
