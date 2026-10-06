package infinicave

import (
	"image/color"
	"math"
	"math/rand"
	"sort"
)

// RockCell is shared by rendering and vine growth; colors belong to entire
// Voronoi faces, not a second approximation of the original noise field.
type RockCell struct {
	Center          V
	Polygon         []V
	Color           color.NRGBA
	Z               float64 // height toward the camera at the control point
	Normal          V3
	Raised          bool
	Shadow, Ambient float64
}

type RockGrid []RockCell

func newRockGrid(seeds []V, colorAt func(V) color.NRGBA) RockGrid {
	grid := make(RockGrid, 0, len(seeds))
	cells := voronoiCells(seeds)
	for i, p := range seeds {
		clr := colorAt(p)
		if clr.A != 0 {
			grid = append(grid, RockCell{Center: p, Polygon: cells[i], Color: clr})
		}
	}
	return grid
}

const (
	vineFieldStep   = 2.0
	vineFieldWidth  = int(generationWidth / vineFieldStep)
	vineFieldHeight = int(generationHeight / vineFieldStep)
	// Prefer charcoal faces, allowing only shallow trips across voids/ridges.
	vineVoidTone        = 3.0
	vineLightTone       = 72.0
	vineMaxIntrusion    = 18.0
	vineForegroundTouch = 8.0
	vineForegroundTuck  = 24.0
	vineMaxExcursion    = 65.0
	vineMinTrunkLength  = 320.0
	vineMinTrunkSpan    = 190.0
	vineMinBranchLength = 85.0
	vineMinTwigLength   = 60.0
	vineThinRadius      = 3.0
	vineMaxRadius       = 4.5
	vineThickLength     = 700.0
	vineFullWidthLength = 1200.0
)

// A small CPU field composites both grids in drawing order. Its clearance
// measures distance from light faces and black voids, including vine width.
type VineTerrain struct {
	// Surface growth stays entirely on the raised rock instead of tucking
	// beneath it or making shallow excursions across the background.
	onForeground bool
	clearance    []float64
	// Distance back to dark rock inside a black or bright face.
	unsupported []float64
	// Distance to the actual background cell edges, independent of face tone.
	borders []float64
	// Exact polygon segments and their junctions for fine seam-following roots.
	edges *vineEdgeGraph
	// Distance to and depth inside foreground cells for shallow edge overlap.
	foreground, foregroundInside []float64
}

func newVineTerrain(background, foreground RockGrid) *VineTerrain {
	tones := make([]float64, vineFieldWidth*vineFieldHeight)
	foregroundDistance := make([]float64, len(tones))
	foregroundInside := make([]float64, len(tones))
	for i := range foregroundDistance {
		foregroundDistance[i] = math.Inf(1)
	}
	for gridIndex, grid := range []RockGrid{background, foreground} {
		for _, cell := range grid {
			if len(cell.Polygon) < 3 {
				continue
			}
			minY, maxY := float64(generationHeight), 0.0
			for _, p := range cell.Polygon {
				minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			}
			alpha := float64(cell.Color.A) / 255
			tone := .30*float64(cell.Color.R) + .59*float64(cell.Color.G) + .11*float64(cell.Color.B)
			// Curved guide cuts can make concave faces with multiple spans.
			var crossings []float64
			for y := max(0, int(minY/vineFieldStep)); y < min(vineFieldHeight, int(maxY/vineFieldStep)+1); y++ {
				py := (float64(y) + .5) * vineFieldStep
				crossings = crossings[:0]
				for i, a := range cell.Polygon {
					b := cell.Polygon[(i+1)%len(cell.Polygon)]
					if (a.Y <= py && b.Y > py) || (b.Y <= py && a.Y > py) {
						x := a.X + (b.X-a.X)*(py-a.Y)/(b.Y-a.Y)
						crossings = append(crossings, x)
					}
				}
				sort.Float64s(crossings)
				for span := 0; span+1 < len(crossings); span += 2 {
					left, right := crossings[span], crossings[span+1]
					for x := max(0, int(math.Ceil(left/vineFieldStep-.5))); x < min(vineFieldWidth, int(math.Ceil(right/vineFieldStep-.5))); x++ {
						i := y*vineFieldWidth + x
						tones[i] = lerp(tones[i], tone, alpha)
						if gridIndex == 1 {
							foregroundDistance[i] = 0
							foregroundInside[i] = math.Inf(1)
						}
					}
				}
			}
		}
	}
	field := &VineTerrain{clearance: make([]float64, len(tones)), unsupported: make([]float64, len(tones)), borders: newVineBorders(background), foreground: foregroundDistance, foregroundInside: foregroundInside}
	field.edges = newVineEdgeGraph(background)
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			i := y*vineFieldWidth + x
			if tones[i] > vineVoidTone && tones[i] < vineLightTone {
				field.clearance[i] = float64(min(x+1, y+1, vineFieldWidth-x, vineFieldHeight-y)) * vineFieldStep
			} else {
				field.unsupported[i] = math.Inf(1)
			}
		}
	}
	// Chamfer distance, with a conservative correction when sampled below.
	chamfer := [][]float64{field.clearance, field.unsupported, field.foreground, field.foregroundInside}
	parallelFor(len(chamfer), func(n int) {
		distances := chamfer[n]
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
							distances[i] = math.Min(distances[i], distances[ny*vineFieldWidth+nx]+d)
						}
					}
				}
			}
		}
	})
	return field
}

// Growth can cross a narrow gap, but the entire ribbon must remain near dark
// rock. The window edges remain hard boundaries.
func (f *VineTerrain) growthSpace(p V) float64 {
	if f.onForeground {
		return f.space(p)
	}
	x, y := int(math.Floor(p.X/vineFieldStep)), int(math.Floor(p.Y/vineFieldStep))
	if x < 0 || y < 0 || x >= vineFieldWidth || y >= vineFieldHeight {
		return 0
	}
	i := y*vineFieldWidth + x
	space := f.clearance[i]*.92 - f.unsupported[i] - 3 + vineMaxIntrusion
	foregroundSpace := f.foreground[i] + vineForegroundTouch
	if f.foreground[i] == 0 {
		foregroundSpace = vineForegroundTouch - f.foregroundInside[i]
	}
	space = math.Min(space, foregroundSpace)
	edge := math.Min(math.Min(p.X, generationWidth-p.X), math.Min(p.Y, generationHeight-p.Y))
	return math.Max(0, math.Min(space, edge))
}

func (f *VineTerrain) space(p V) float64 {
	x, y := int(math.Floor(p.X/vineFieldStep)), int(math.Floor(p.Y/vineFieldStep))
	if x < 0 || y < 0 || x >= vineFieldWidth || y >= vineFieldHeight {
		return 0
	}
	return math.Max(0, f.clearance[y*vineFieldWidth+x]*.92-3)
}

func (f *VineTerrain) foregroundDepthAt(p V) (float64, bool) {
	x, y := int(math.Floor(p.X/vineFieldStep)), int(math.Floor(p.Y/vineFieldStep))
	if x < 0 || y < 0 || x >= vineFieldWidth || y >= vineFieldHeight {
		return 0, false
	}
	i := y*vineFieldWidth + x
	if f.foreground[i] != 0 {
		return 0, false
	}
	return f.foregroundInside[i], true
}

type VinePoint struct {
	P      V
	Radius float64
}

type Vine struct {
	Points []VinePoint
	Depth  int
	// Parent and Joint identify the exact attachment on the preceding stem.
	// Trunks have Parent == -1.
	Parent int
	Joint  int
	// Fine offshoots trace polygon seams exactly, including their corners.
	EdgeAligned bool
	// Foreground stems use a richer material and are drawn over the rock.
	Foreground bool
}

func rotateV(v V, angle float64) V {
	c, s := math.Cos(angle), math.Sin(angle)
	return V{v.X*c - v.Y*s, v.X*s + v.Y*c}
}

// Small bounded turns keep the centerline smooth. Looking ahead lets a tendril
// turn before reaching a pale ridge instead of clipping it at the boundary.
func growVine(field *VineTerrain, root, heading V, radius, reach, phase, curl float64, depth int) Vine {
	const step = 2.5
	v := Vine{Points: []VinePoint{{root, radius}}, Depth: depth, Parent: -1}
	excursion := 0.0
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
		// Thick sections bend more gently, including when steering around
		// rock edges. Thin tips retain their tighter curls.
		flexibility := 1 / (1 + .08*math.Max(0, r-vineThinRadius))
		bestScore := math.Inf(-1)
		var next, direction V
		var nextExcursion float64
		p := v.Points[len(v.Points)-1].P
		border := field.borderDistance(p)
		for j := -5; j <= 5; j++ {
			d := rotateV(heading, (turn+float64(j)*.03)*flexibility)
			q := p.Add(d.Mul(step))
			space := field.growthSpace(q)
			if space < r+1.5 || field.growthSpace(lerpV(p, q, .5)) < r+1.5 {
				continue
			}
			trip := 0.0
			if field.space(q) < r+1.5 {
				trip = excursion + step
			}
			if trip > vineMaxExcursion {
				continue
			}
			near := field.growthSpace(q.Add(d.Mul(10)))
			far := field.growthSpace(q.Add(d.Mul(23)))
			score := -float64(j*j)*.018 - 2.5*math.Pow(math.Max(0, r+9-near)/12, 2) - math.Pow(math.Max(0, r+9-far)/18, 2)
			score -= .35 * trip / vineMaxExcursion
			// Look along the background seams before turning. A short horizon
			// rounds cell corners while keeping the stem close to their edges.
			nearBorder := field.borderDistance(q.Add(d.Mul(10)))
			farBorder := field.borderDistance(q.Add(d.Mul(20)))
			score += .035*(border*border-nearBorder*nearBorder) + .012*(border*border-farBorder*farBorder)
			// Curl back toward the stem without repeatedly tracing over it.
			for k := 0; k < len(v.Points)-22; k += 3 {
				distance := q.Sub(v.Points[k].P).Len()
				if distance < r+v.Points[k].Radius+5 {
					score -= 5
				}
			}
			if score > bestScore {
				bestScore, next, direction = score, q, d
				nextExcursion = trip
			}
		}
		if bestScore < -4 {
			break
		}
		v.Points = append(v.Points, VinePoint{next, r})
		heading = direction
		excursion = nextExcursion
	}
	if len(v.Points) > 1 {
		end := v.Points[len(v.Points)-1].P
		if _, inside := field.foregroundDepthAt(end); !inside {
			for distance := vineFieldStep; distance <= vineForegroundTuck; distance += vineFieldStep {
				q := end.Add(heading.Mul(distance))
				if _, inside := field.foregroundDepthAt(q); inside {
					v.Points = append(v.Points, VinePoint{q, 0})
					break
				}
			}
		}
	}
	// An obstacle can shorten growth. Always finish with a tapered tip.
	length := float64(len(v.Points)-1) * step
	if length == 0 {
		v.Points[0].Radius = 0
		return v
	}
	for i := range v.Points {
		remaining := length - float64(i)*step
		v.Points[i].Radius = math.Min(v.Points[i].Radius, vineTipRadius(radius, remaining))
	}
	return v
}

// Use completed length, not intended reach: a stem stopped by terrain must
// stay slender. Scale the whole profile to preserve its taper and cap each
// parent before growing children so forks inherit the finished thickness.
func (v *Vine) limitThickness() {
	length, _ := v.extent()
	radius := 0.0
	for _, p := range v.Points {
		radius = math.Max(radius, p.Radius)
	}
	if radius <= vineThinRadius {
		return
	}
	allowed := lerp(vineThinRadius, math.Min(radius, vineMaxRadius), smoothstep(vineThickLength, vineFullWidthLength, length))
	for i := range v.Points {
		v.Points[i].Radius *= allowed / radius
	}
}

// A thick stem needs a long taper even when terrain cuts its growth short.
// Cap the width rather than multiplying it so joining halves preserves it.
func vineTipRadius(radius, distance float64) float64 {
	return radius * smoothstep(0, math.Max(80, radius*18), distance)
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
	return generateVinesInBand(field, rng, 20, generationHeight-20, 5*generationHeight/generationWidth)
}

func generateVinesInBand(field *VineTerrain, rng *rand.Rand, bottom, top float64, count int) []Vine {
	minLength, minSpan := vineMinTrunkLength, vineMinTrunkSpan
	radiusLow, radiusHigh := 6.0, 9.0
	forks := 5
	if field.onForeground {
		minLength, minSpan = 190, 115
		radiusLow, radiusHigh = 3.2, 4.5
		forks = 2
	}
	var vines []Vine
	for rootIndex := 0; rootIndex < count; rootIndex++ {
		type candidate struct {
			p     V
			score float64
		}
		var candidates []candidate
		for attempt := 0; attempt < 240; attempt++ {
			p := V{20 + rng.Float64()*(generationWidth-40), bottom + rng.Float64()*(top-bottom)}
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
			if distance > 65 {
				candidates = append(candidates, candidate{p, math.Min(space, 40) - distance*.12 - 2*field.borderDistance(p)})
			}
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		angle := -math.Pi/2 + lerp(-.9, .9, rng.Float64())
		heading := V{math.Cos(angle), math.Sin(angle)}
		radius := lerp(radiusLow, radiusHigh, rng.Float64())
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
				v, joint := joinVineHalves(a, b)
				length, span := v.extent()
				if length < minLength || span < minSpan {
					continue
				}
				score := length*.6 + span + candidate.score*.3
				if score > best {
					trunk, rootAt, best = v, joint, score
				}
			}
			if len(tried) >= 10 {
				break
			}
		}
		if best == 0 {
			continue
		}
		trunk.limitThickness()
		trunkIndex := len(vines)
		vines = append(vines, trunk)
		// Invest in long, attached limbs before starting another independent
		// trunk. Short blocked offshoots are discarded with their descendants.
		for fork := 0; fork < forks; fork++ {
			u := (float64(fork) + lerp(.25, .75, rng.Float64())) / float64(forks)
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
			branch.limitThickness()
			branch.Parent, branch.Joint = trunkIndex, i
			branchIndex := len(vines)
			vines = append(vines, branch)
			if fork%2 == 0 && branchLength > 150 {
				j := len(branch.Points) / 2
				b := branch.Points[j]
				d := branch.Points[j+1].P.Sub(branch.Points[j-1].P).Norm()
				twig := growVine(field, b.P, rotateV(d, -side*.4), b.Radius*.6, lerp(140, 220, rng.Float64()), phase, -side, 2)
				if length, _ := twig.extent(); length >= vineMinTwigLength {
					twig.limitThickness()
					twig.Parent, twig.Joint = branchIndex, j
					vines = append(vines, twig)
				}
			}
		}
	}
	return addVineEdgeBranches(field, vines, rng)
}

// Join two growth directions at one full-width root. If either direction is
// blocked immediately, its zero-width tip must not pinch the surviving stem.
func joinVineHalves(a, b Vine) (Vine, int) {
	v := Vine{Parent: -1}
	for i := len(b.Points) - 1; i > 0; i-- {
		v.Points = append(v.Points, b.Points[i])
	}
	joint := len(v.Points)
	v.Points = append(v.Points, a.Points...)
	v.Points[joint].Radius = math.Max(a.Points[0].Radius, b.Points[0].Radius)
	// Both ends are actual tips, including when one half never grew.
	length, _ := v.extent()
	radius := 0.0
	for _, p := range v.Points {
		radius = math.Max(radius, p.Radius)
	}
	along := 0.0
	for i := range v.Points {
		if i > 0 {
			along += v.Points[i].P.Sub(v.Points[i-1].P).Len()
		}
		if length == 0 {
			v.Points[i].Radius = 0
		} else {
			v.Points[i].Radius = math.Min(v.Points[i].Radius, vineTipRadius(radius, math.Min(along, length-along)))
		}
	}
	return v, joint
}
