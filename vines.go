package main

import (
	"image/color"
	"math"
	"math/rand"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
)

// RockCell is shared by rendering and vine growth; colors belong to entire
// Voronoi faces, not a second approximation of the original noise field.
type RockCell struct {
	Center  V
	Polygon []V
	Color   color.NRGBA
}

type RockGrid []RockCell

func newRockGrid(seeds []V, colorAt func(V) color.NRGBA) RockGrid {
	grid := make(RockGrid, 0, len(seeds))
	for i, p := range seeds {
		clr := colorAt(p)
		if clr.A != 0 {
			grid = append(grid, RockCell{p, voronoiCell(i, seeds), clr})
		}
	}
	return grid
}

const (
	vineFieldStep   = 2.0
	vineFieldWidth  = int(W / vineFieldStep)
	vineFieldHeight = int(H / vineFieldStep)
	// Match the quiet black pockets and reserve gray/ivory faces for ridges.
	vineVoidTone        = 3.0
	vineLightTone       = 72.0
	vineMinTrunkLength  = 320.0
	vineMinTrunkSpan    = 190.0
	vineMinBranchLength = 85.0
	vineMinTwigLength   = 60.0
)

// A small CPU field composites both grids in drawing order. Its clearance
// measures distance from light faces and black voids, including vine width.
type VineTerrain struct {
	clearance []float64
}

func newVineTerrain(background, foreground RockGrid) *VineTerrain {
	tones := make([]float64, vineFieldWidth*vineFieldHeight)
	for _, grid := range []RockGrid{background, foreground} {
		for _, cell := range grid {
			if len(cell.Polygon) < 3 {
				continue
			}
			minY, maxY := float64(H), 0.0
			for _, p := range cell.Polygon {
				minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			}
			alpha := float64(cell.Color.A) / 255
			tone := .30*float64(cell.Color.R) + .59*float64(cell.Color.G) + .11*float64(cell.Color.B)
			// Convex polygons have a single span on each scanline.
			for y := max(0, int(minY/vineFieldStep)); y < min(vineFieldHeight, int(maxY/vineFieldStep)+1); y++ {
				py := (float64(y) + .5) * vineFieldStep
				left, right := float64(W), 0.0
				for i, a := range cell.Polygon {
					b := cell.Polygon[(i+1)%len(cell.Polygon)]
					if (a.Y <= py && b.Y > py) || (b.Y <= py && a.Y > py) {
						x := a.X + (b.X-a.X)*(py-a.Y)/(b.Y-a.Y)
						left, right = math.Min(left, x), math.Max(right, x)
					}
				}
				for x := max(0, int(math.Ceil(left/vineFieldStep-.5))); x < min(vineFieldWidth, int(math.Ceil(right/vineFieldStep-.5))); x++ {
					i := y*vineFieldWidth + x
					tones[i] = lerp(tones[i], tone, alpha)
				}
			}
		}
	}
	field := &VineTerrain{make([]float64, len(tones))}
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			i := y*vineFieldWidth + x
			if tones[i] > vineVoidTone && tones[i] < vineLightTone {
				field.clearance[i] = float64(min(x+1, y+1, vineFieldWidth-x, vineFieldHeight-y)) * vineFieldStep
			}
		}
	}
	// Chamfer distance, with a conservative correction when sampled below.
	for _, dir := range []int{1, -1} {
		for yy := 0; yy < vineFieldHeight; yy++ {
			y := yy
			if dir < 0 {
				y = vineFieldHeight - 1 - yy
			}
			for xx := 0; xx < vineFieldWidth; xx++ {
				x := xx
				if dir < 0 {
					x = vineFieldWidth - 1 - xx
				}
				i := y*vineFieldWidth + x
				for _, delta := range [][2]int{{-dir, 0}, {0, -dir}, {-dir, -dir}, {dir, -dir}} {
					nx, ny := x+delta[0], y+delta[1]
					if nx >= 0 && nx < vineFieldWidth && ny >= 0 && ny < vineFieldHeight {
						d := vineFieldStep
						if delta[0] != 0 && delta[1] != 0 {
							d *= math.Sqrt2
						}
						field.clearance[i] = math.Min(field.clearance[i], field.clearance[ny*vineFieldWidth+nx]+d)
					}
				}
			}
		}
	}
	return field
}

func (f *VineTerrain) space(p V) float64 {
	x, y := int(math.Floor(p.X/vineFieldStep)), int(math.Floor(p.Y/vineFieldStep))
	if x < 0 || y < 0 || x >= vineFieldWidth || y >= vineFieldHeight {
		return 0
	}
	return math.Max(0, f.clearance[y*vineFieldWidth+x]*.92-3)
}

type VinePoint struct {
	P      V
	Radius float64
}

type Vine struct {
	Points []VinePoint
	Depth  int
}

func rotateV(v V, angle float64) V {
	c, s := math.Cos(angle), math.Sin(angle)
	return V{v.X*c - v.Y*s, v.X*s + v.Y*c}
}

// Small bounded turns keep the centerline smooth. Looking ahead lets a tendril
// turn before reaching a pale ridge instead of clipping it at the boundary.
func growVine(field *VineTerrain, root, heading V, radius, reach, phase, curl float64, depth int) Vine {
	const step = 2.5
	v := Vine{Points: []VinePoint{{root, radius}}, Depth: depth}
	for s := 0.0; s < reach; s += step {
		u := s / reach
		r := radius * math.Pow(1-u, .8)
		// Trunks make broad sweeps before curling at the tips. Offshoots
		// can curl earlier without turning the main stem back on itself.
		curlStart := .65
		if depth > 0 {
			curlStart = .35
		}
		turn := .025*math.Sin(s/75+phase) + curl*.075*smoothstep(curlStart, 1, u)
		if depth > 0 {
			turn += curl * .006
		}
		preferred := rotateV(heading, turn)
		bestScore := math.Inf(-1)
		var next, direction V
		p := v.Points[len(v.Points)-1].P
		for j := -5; j <= 5; j++ {
			d := rotateV(preferred, float64(j)*.03)
			q := p.Add(d.Mul(step))
			space := field.space(q)
			if space < r+1.5 || field.space(lerpV(p, q, .5)) < r+1.5 {
				continue
			}
			near := field.space(q.Add(d.Mul(10)))
			far := field.space(q.Add(d.Mul(23)))
			score := -float64(j*j)*.018 - 2.5*math.Pow(math.Max(0, r+9-near)/12, 2) - math.Pow(math.Max(0, r+9-far)/18, 2)
			// Curl back toward the stem without repeatedly tracing over it.
			for k := 0; k < len(v.Points)-22; k += 3 {
				distance := q.Sub(v.Points[k].P).Len()
				if distance < r+v.Points[k].Radius+5 {
					score -= 5
				}
			}
			if score > bestScore {
				bestScore, next, direction = score, q, d
			}
		}
		if bestScore < -4 {
			break
		}
		v.Points = append(v.Points, VinePoint{next, r})
		heading = direction
	}
	// An obstacle can shorten growth. Always finish with a tapered tip.
	length := float64(len(v.Points)-1) * step
	if length == 0 {
		v.Points[0].Radius = 0
		return v
	}
	for i := range v.Points {
		remaining := length - float64(i)*step
		v.Points[i].Radius *= smoothstep(0, math.Min(38, length*.45), remaining)
	}
	return v
}

// Measure both arc length and spatial extent: a tightly coiled vine in a
// small pocket still only covers a few rocks, even if its centerline is long.
func (v Vine) extent() (length, span float64) {
	if len(v.Points) == 0 {
		return
	}
	lo, hi := v.Points[0].P, v.Points[0].P
	for i, p := range v.Points {
		lo.X, lo.Y = math.Min(lo.X, p.P.X), math.Min(lo.Y, p.P.Y)
		hi.X, hi.Y = math.Max(hi.X, p.P.X), math.Max(hi.Y, p.P.Y)
		if i > 0 {
			length += p.P.Sub(v.Points[i-1].P).Len()
		}
	}
	return length, hi.Sub(lo).Len()
}

func generateVines(field *VineTerrain, rng *rand.Rand) []Vine {
	var vines []Vine
	for rootIndex := 0; rootIndex < 5; rootIndex++ {
		type candidate struct {
			p     V
			score float64
		}
		var candidates []candidate
		for attempt := 0; attempt < 240; attempt++ {
			p := V{20 + rng.Float64()*(W-40), 20 + rng.Float64()*(H-40)}
			space := field.space(p)
			if space < 15 {
				continue
			}
			distance := 260.0
			for _, vine := range vines {
				for i := 0; i < len(vine.Points); i += 6 {
					distance = math.Min(distance, p.Sub(vine.Points[i].P).Len())
				}
			}
			if distance > 85 {
				candidates = append(candidates, candidate{p, math.Min(space, 40) + distance*.65})
			}
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		angle := -math.Pi/2 + lerp(-.9, .9, rng.Float64())
		heading := V{math.Cos(angle), math.Sin(angle)}
		radius := lerp(6, 9, rng.Float64())
		phase := rng.Float64() * 2 * math.Pi
		curl := 1.0
		if rng.Intn(2) == 0 {
			curl = -1
		}
		reachA, reachB := lerp(560, 820, rng.Float64()), lerp(420, 650, rng.Float64())
		trunk, rootAt, best := Vine{}, 0, 0.0
		var tried []V
		// Compare complete growth from several separated roots. An open root
		// alone does not guarantee a long dark corridor in either direction.
		for _, candidate := range candidates {
			near := false
			for _, p := range tried {
				if p.Sub(candidate.p).Len() < 65 {
					near = true
					break
				}
			}
			if near {
				continue
			}
			tried = append(tried, candidate.p)
			for trial := 0; trial < 4; trial++ {
				d := rotateV(heading, float64(trial)*math.Pi/4)
				a := growVine(field, candidate.p, d, radius, reachA, phase, curl, 0)
				b := growVine(field, candidate.p, d.Mul(-1), radius, reachB, phase+math.Pi, -curl, 0)
				v := Vine{}
				for i := len(b.Points) - 1; i > 0; i-- {
					v.Points = append(v.Points, b.Points[i])
				}
				v.Points = append(v.Points, a.Points...)
				length, span := v.extent()
				if length < vineMinTrunkLength || span < vineMinTrunkSpan {
					continue
				}
				score := length*.6 + span + candidate.score*.3
				if score > best {
					trunk, rootAt, best = v, len(b.Points)-1, score
				}
			}
			if len(tried) >= 10 {
				break
			}
		}
		if best == 0 {
			continue
		}
		vines = append(vines, trunk)
		// Invest in long, attached limbs before starting another independent
		// trunk. Short blocked offshoots are discarded with their descendants.
		for fork := 0; fork < 5; fork++ {
			u := (float64(fork) + lerp(.25, .75, rng.Float64())) / 5
			i := int(lerp(12, float64(len(trunk.Points)-13), u))
			base := trunk.Points[i]
			if base.Radius < 1.8 {
				continue
			}
			direction := trunk.Points[i+1].P.Sub(trunk.Points[i-1].P).Norm()
			side := 1.0
			if fork%2 == 0 {
				side = -1
			}
			if i < rootAt {
				direction = direction.Mul(-1)
			}
			// Try both sides so a blocked fork can spread into the open side.
			branch := Vine{}
			branchLength := 0.0
			reach := lerp(250, 420, rng.Float64())
			for _, sign := range []float64{side, -side} {
				v := growVine(field, base.P, rotateV(direction, sign*.32), base.Radius*.65, reach, phase+float64(fork), sign, 1)
				length, _ := v.extent()
				if length > branchLength {
					branch, branchLength, side = v, length, sign
				}
			}
			if branchLength < vineMinBranchLength {
				continue
			}
			vines = append(vines, branch)
			if fork%2 == 0 && branchLength > 150 {
				j := len(branch.Points) / 2
				b := branch.Points[j]
				d := branch.Points[j+1].P.Sub(branch.Points[j-1].P).Norm()
				twig := growVine(field, b.P, rotateV(d, -side*.4), b.Radius*.6, lerp(140, 220, rng.Float64()), phase, -side, 2)
				if length, _ := twig.extent(); length >= vineMinTwigLength {
					vines = append(vines, twig)
				}
			}
		}
	}
	return vines
}

// Shaded ribbons provide a dark outline, scarlet body and narrow longitudinal
// highlights. All shadows are drawn first, then twigs, branches and trunks so
// forks merge into their parent instead of leaving dark seams across it.
func drawVines(dst *ebiten.Image, vines []Vine) {
	white := ebiten.NewImage(1, 1)
	white.Fill(color.White)
	defer white.Deallocate()
	draw := func(vine Vine, shadow bool) {
		var vertices []ebiten.Vertex
		var indices []uint32
		bands := []float64{-1, -.78, -.46, -.2, .04, .3, .65, 1}
		colors := []color.NRGBA{{42, 0, 3, 255}, {116, 1, 8, 255}, {200, 4, 13, 255}, {245, 16, 23, 255}, {210, 5, 12, 255}, {155, 1, 8, 255}, {92, 0, 5, 255}, {34, 0, 3, 255}}
		if shadow {
			bands = []float64{-1, 1}
			colors = []color.NRGBA{{0, 0, 0, 125}, {0, 0, 0, 125}}
		}
		for i, point := range vine.Points {
			before, after := vine.Points[max(0, i-1)].P, vine.Points[min(len(vine.Points)-1, i+1)].P
			n := after.Sub(before).Norm().Perp()
			for j, band := range bands {
				r := point.Radius
				p := point.P
				if shadow {
					r *= 1.2
					p = p.Add(V{1.4, 2})
				}
				p = p.Add(n.Mul(band * r))
				clr := colors[j]
				a := float32(clr.A) / 255
				vertices = append(vertices, ebiten.Vertex{DstX: float32(p.X), DstY: float32(p.Y), SrcX: .5, SrcY: .5, ColorR: float32(clr.R) / 255 * a, ColorG: float32(clr.G) / 255 * a, ColorB: float32(clr.B) / 255 * a, ColorA: a})
				if i > 0 && j > 0 {
					k := uint32(len(vertices) - 1)
					b := uint32(len(bands))
					indices = append(indices, k-b-1, k-b, k, k-b-1, k, k-1)
				}
			}
		}
		dst.DrawTriangles32(vertices, indices, white, &ebiten.DrawTrianglesOptions{AntiAlias: true})
	}
	for _, vine := range vines {
		draw(vine, true)
	}
	for depth := 2; depth >= 0; depth-- {
		for _, vine := range vines {
			if vine.Depth == depth {
				draw(vine, false)
			}
		}
	}
}
