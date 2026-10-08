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
	heights   []float64
	light     geom.V3
	direction geom.V
	rise      float64
	probes    []shadowProbe
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
	d.direction = (geom.V{X: d.light.X, Y: d.light.Y}).Norm()
	d.rise = d.light.Z / math.Hypot(d.light.X, d.light.Y)
	for distance := .008; distance <= .184; distance += .004 {
		d.probes = append(d.probes, shadowProbe{offset: d.direction.Mul(distance), rise: distance * d.rise, softness: .005 + distance*.05})
	}
	fillFloat64(d.heights, -.016)
	crossings := make([]float64, 0, 16)
	for _, grid := range grids {
		for _, c := range grid {
			minY, maxY := float64(generationMaxY), float64(GenerationMinY)
			for _, p := range c.Polygon {
				minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			}

			for y := max(0, int((minY-GenerationMinY)/rockDepthStep)); y < min(rockDepthHeight, int((maxY-GenerationMinY)/rockDepthStep)+1); y++ {
				py := GenerationMinY + (float64(y)+.5)*rockDepthStep
				crossings = crossings[:0]
				for j, a := range c.Polygon {
					b := c.Polygon[(j+1)%len(c.Polygon)]
					// Shared edges on a scan line belong to the same face in
					// every padded window, despite translation roundoff.
					if (a.Y <= py+1e-10 && b.Y > py+1e-10) || (b.Y <= py+1e-10 && a.Y > py+1e-10) {
						crossings = append(crossings, a.X+(b.X-a.X)*(py-a.Y)/(b.Y-a.Y))
					}
				}
				sort.Float64s(crossings)
				for j := 0; j+1 < len(crossings); j += 2 {
					first := max(0, int(math.Ceil(crossings[j]/rockDepthStep-.5-1e-9)))
					last := min(rockDepthWidth, int(math.Ceil(crossings[j+1]/rockDepthStep-.5-1e-9)))
					if first < last {
						rasterDepthSpan(d.heights[y*rockDepthWidth+first:y*rockDepthWidth+last], first, py, c)
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
	// Equivalent padded windows can differ by a few floating-point bits at
	// a texel boundary. Resolve those ties identically before indexing.
	x := min(rockDepthWidth-1, int(math.Floor(p.X/rockDepthStep+1e-9)))
	y := min(rockDepthHeight-1, int(math.Floor((p.Y-GenerationMinY)/rockDepthStep+1e-9)))
	return d.heights[y*rockDepthWidth+x]
}

type shadowProbe struct {
	offset         geom.V
	rise, softness float64
}

func (d *rockDepth) visibility(p geom.V, z float64) float64 {
	visible := 1.0
	for _, probe := range d.probes {
		blocker := d.at(p.Add(probe.offset)) - z - probe.rise
		// A small bias avoids self-shadow acne; increasing softness models a
		// finite light source and leaves a sharper contact near the blocker.
		visible = math.Min(visible, 1-geom.Smoothstep(.002, probe.softness, blocker))
		if visible == 0 {
			break
		}
	}
	return visible
}

var ambientDirections = func() (directions [8]geom.V) {
	for i := range directions {
		angle := float64(i) * math.Pi / 4
		directions[i] = geom.V{X: math.Cos(angle), Y: math.Sin(angle)}
	}
	return
}()

var ambientProbes = func() (probes [8][3]struct {
	offset   geom.V
	distance float64
}) { for i, direction := range ambientDirections {
	for j, distance := range [...]float64{.008, .020, .040} {
		probes[i][j].offset = direction.Mul(distance)
		probes[i][j].distance = distance
	}
}; return }()

func (d *rockDepth) ambient(p geom.V, z float64) float64 {
	occlusion := 0.0
	for _, probes := range ambientProbes {
		horizon := 0.0
		for _, probe := range probes {
			horizon = math.Max(horizon, (d.at(p.Add(probe.offset))-z-.003)/probe.distance)
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
