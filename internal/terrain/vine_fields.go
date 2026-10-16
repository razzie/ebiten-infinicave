package terrain

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Generation returns these large buffers after growth, never with a Section.
// The pool is safe when Reset overlaps an old generation worker.
var vineFieldBuffers = sync.Pool{New: func() any {
	return new([vineFieldWidth * vineFieldHeight]float64)
}}

// Streaming owns a workspace for its worker's lifetime. Keeping its six
// buffers here prevents unrelated GC cycles from discarding reusable fields.
// Standalone field builders use the GC-managed pool instead.
type vineWorkspace struct {
	free [][]float64
}

func (w *vineWorkspace) take() []float64 {
	if w == nil {
		return takeVineField()
	}
	if len(w.free) == 0 {
		return make([]float64, vineFieldWidth*vineFieldHeight)
	}
	i := len(w.free) - 1
	field := w.free[i]
	w.free[i] = nil
	w.free = w.free[:i]
	clear(field)
	return field
}

func (w *vineWorkspace) put(field []float64) {
	if len(field) != vineFieldWidth*vineFieldHeight {
		return
	}
	if w == nil {
		putVineField(field)
	} else {
		w.free = append(w.free, field)
	}
}

func takeVineField() []float64 {
	p := vineFieldBuffers.Get().(*[vineFieldWidth * vineFieldHeight]float64)
	clear(p[:])
	return p[:]
}

func putVineField(field []float64) {
	if len(field) == vineFieldWidth*vineFieldHeight {
		vineFieldBuffers.Put((*[vineFieldWidth * vineFieldHeight]float64)(field))
	}
}

func (f *VineTerrain) release() {
	for _, field := range [][]float64{f.clearance, f.unsupported, f.borders, f.foreground, f.foregroundInside} {
		f.workspace.put(field)
	}
	f.clearance, f.unsupported, f.borders, f.foreground, f.foregroundInside = nil, nil, nil, nil, nil
	f.edges = nil
}

// Visit covered scanline spans once. A reusable intersection buffer handles
// concave guide cuts without allocating per face or per raster sample.
func rasterVineSpans(grid RockGrid, visit func(cell RockCell, first, last int)) {
	crossings := make([]float64, 0, 16)
	for _, cell := range grid {
		if len(cell.Polygon) < 3 {
			continue
		}
		minY, maxY := float64(generationMaxY), float64(GenerationMinY)
		for _, p := range cell.Polygon {
			minY, maxY = min(minY, p.Y), max(maxY, p.Y)
		}
		for y := max(0, int((minY-GenerationMinY)/vineFieldStep)); y < min(vineFieldHeight, int((maxY-GenerationMinY)/vineFieldStep)+1); y++ {
			py := GenerationMinY + (float64(y)+.5)*vineFieldStep
			crossings = crossings[:0]
			for i, a := range cell.Polygon {
				b := cell.Polygon[(i+1)%len(cell.Polygon)]
				if (a.Y <= py && b.Y > py) || (b.Y <= py && a.Y > py) {
					crossings = append(crossings, a.X+(b.X-a.X)*(py-a.Y)/(b.Y-a.Y))
				}
			}
			sort.Float64s(crossings)
			for span := 0; span+1 < len(crossings); span += 2 {
				first := max(0, int(math.Ceil(crossings[span]/vineFieldStep-.5)))
				last := min(vineFieldWidth, int(math.Ceil(crossings[span+1]/vineFieldStep-.5)))
				if first < last {
					visit(cell, y*vineFieldWidth+first, y*vineFieldWidth+last)
				}
			}
		}
	}
}

// The same eight-neighbor chamfer as before, with explicit forward/backward
// scans. Only row endpoints need bounds checks; zero samples need no work.
func chamferVineField(d []float64) {
	const w, h = vineFieldWidth, vineFieldHeight
	const step, diagonal = vineFieldStep, vineFieldStep * math.Sqrt2
	for x := 1; x < w; x++ {
		d[x] = min(d[x], d[x-1]+step)
	}
	for y := 1; y < h; y++ {
		i := y * w
		d[i] = min(d[i], d[i-w]+step, d[i-w+1]+diagonal)
		for x := 1; x < w-1; x++ {
			i := y*w + x
			if d[i] != 0 {
				d[i] = min(d[i], d[i-1]+step, d[i-w]+step, d[i-w-1]+diagonal, d[i-w+1]+diagonal)
			}
		}
		i += w - 1
		d[i] = min(d[i], d[i-1]+step, d[i-w]+step, d[i-w-1]+diagonal)
	}
	for x := w - 2; x >= 0; x-- {
		i := (h-1)*w + x
		d[i] = min(d[i], d[i+1]+step)
	}
	for y := h - 2; y >= 0; y-- {
		i := y*w + w - 1
		d[i] = min(d[i], d[i+w]+step, d[i+w-1]+diagonal)
		for x := w - 2; x > 0; x-- {
			i := y*w + x
			if d[i] != 0 {
				d[i] = min(d[i], d[i+1]+step, d[i+w]+step, d[i+w+1]+diagonal, d[i+w-1]+diagonal)
			}
		}
		i = y * w
		d[i] = min(d[i], d[i+1]+step, d[i+w]+step, d[i+w+1]+diagonal)
	}
}

func newVineTerrainMode(background, foreground RockGrid, onForeground bool) *VineTerrain {
	return newVineTerrainWithWorkspace(background, foreground, onForeground, nil)
}

func newVineTerrainWithWorkspace(background, foreground RockGrid, onForeground bool, workspace *vineWorkspace) *VineTerrain {
	field := &VineTerrain{onForeground: onForeground, clearance: workspace.take(), edges: newVineEdgeGraph(background), workspace: workspace}
	for _, grid := range []RockGrid{background, foreground} {
		if len(grid) > 0 {
			field.orientation = grid[0].orientation
			break
		}
	}
	field.borders = vineBordersForEdges(field.edges, workspace)
	tones := workspace.take()
	defer workspace.put(tones)
	if !onForeground {
		field.unsupported, field.foreground, field.foregroundInside = workspace.take(), workspace.take(), workspace.take()
		fillFloat64(field.foreground, math.Inf(1))
	}
	for layer, grid := range []RockGrid{background, foreground} {
		rasterVineSpans(grid, func(cell RockCell, first, last int) {
			alpha := float64(cell.Color.A) / 255
			tone := .30*float64(cell.Color.R) + .59*float64(cell.Color.G) + .11*float64(cell.Color.B)
			if onForeground {
				if cell.Color.A != 0 {
					fillFloat64(tones[first:last], 36)
				}
			} else {
				blendVineTone(tones[first:last], tone, alpha)
				if layer == 1 {
					clear(field.foreground[first:last])
					fillFloat64(field.foregroundInside[first:last], math.Inf(1))
				}
			}
		})
	}
	classifyVineTones(field, tones)
	fields := [][]float64{field.clearance}
	if !onForeground {
		fields = append(fields, field.unsupported, field.foreground, field.foregroundInside)
	}
	parallelFor(len(fields), func(n int) { chamferVineField(fields[n]) })
	return field
}

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
	fillFloat64(distances, vineBorderRange)
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
			if x0 <= x1 {
				segmentDistanceSpan(distances[y*vineFieldWidth+x0:y*vineFieldWidth+x1+1], x0, GenerationMinY+(float64(y)+.5)*vineFieldStep, a, d, inverse, false, false)
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

const foregroundVineGuideClearance = .030

func newForegroundVineTerrain(foreground RockGrid, guides []Guide) *VineTerrain {
	return newForegroundVineTerrainWithWorkspace(foreground, guides, nil)
}

func newForegroundVineTerrainWithWorkspace(foreground RockGrid, guides []Guide, workspace *vineWorkspace) *VineTerrain {
	// Growth is based on the visible footprint, including its screen inset,
	// rather than rock brightness: shaded and lit faces both support vines.
	support := InsetForegroundGrid(foreground)
	field := newVineTerrainWithWorkspace(support, nil, true, workspace)
	// Restrict the same clearance field used by root placement, steering,
	// and seam-following twigs so the full family keeps off the crests.
	// The existing conservative sampler also reserves room for ribbon width.
	const reach = foregroundVineGuideClearance + vineBorderRange
	for _, guide := range guides {
		for j := 1; j < len(guide.Pts); j++ {
			a, b := guide.Pts[j-1], guide.Pts[j]
			d := b.Sub(a)
			length2 := d.Len2()
			if length2 < 1e-18 {
				continue
			}
			x0 := max(0, int(math.Floor((min(a.X, b.X)-reach)/vineFieldStep)))
			x1 := min(vineFieldWidth-1, int(math.Ceil((max(a.X, b.X)+reach)/vineFieldStep)))
			y0 := max(0, int(math.Floor((min(a.Y, b.Y)-reach-GenerationMinY)/vineFieldStep)))
			y1 := min(vineFieldHeight-1, int(math.Ceil((max(a.Y, b.Y)+reach-GenerationMinY)/vineFieldStep)))
			for y := y0; y <= y1; y++ {
				if x0 <= x1 {
					segmentDistanceSpan(field.clearance[y*vineFieldWidth+x0:y*vineFieldWidth+x1+1], x0, GenerationMinY+(float64(y)+.5)*vineFieldStep, a, d, length2, true, true)
				}
			}
		}
	}
	return field
}

func generateForegroundVines(foreground RockGrid, guides []Guide, rng *rand.Rand) []Vine {
	return generateForegroundVinesWithWorkspace(foreground, guides, rng, nil)
}

func generateForegroundVinesWithWorkspace(foreground RockGrid, guides []Guide, rng *rand.Rand, workspace *vineWorkspace) []Vine {
	return generateForegroundVinesContext(context.Background(), foreground, guides, rng, workspace)
}

func generateForegroundVinesContext(ctx context.Context, foreground RockGrid, guides []Guide, rng *rand.Rand, workspace *vineWorkspace) []Vine {
	field := newForegroundVineTerrainWithWorkspace(foreground, guides, workspace)
	defer field.release()
	vines := generateVinesInBandContext(ctx, field, rng, 0, SectionHeight, 6)
	for i := range vines {
		vines[i].Foreground = true
	}
	return vines
}
