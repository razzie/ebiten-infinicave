package infinicave

import (
	"math"
	"sort"
	"sync"
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
		minY, maxY := float64(generationMaxY), float64(generationMinY)
		for _, p := range cell.Polygon {
			minY, maxY = min(minY, p.Y), max(maxY, p.Y)
		}
		for y := max(0, int((minY-generationMinY)/vineFieldStep)); y < min(vineFieldHeight, int((maxY-generationMinY)/vineFieldStep)+1); y++ {
			py := generationMinY + (float64(y)+.5)*vineFieldStep
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
	field.borders = vineBordersForEdges(field.edges, workspace)
	tones := workspace.take()
	defer workspace.put(tones)
	if !onForeground {
		field.unsupported, field.foreground, field.foregroundInside = workspace.take(), workspace.take(), workspace.take()
		for i := range field.foreground {
			field.foreground[i] = math.Inf(1)
		}
	}
	for layer, grid := range []RockGrid{background, foreground} {
		rasterVineSpans(grid, func(cell RockCell, first, last int) {
			alpha := float64(cell.Color.A) / 255
			tone := .30*float64(cell.Color.R) + .59*float64(cell.Color.G) + .11*float64(cell.Color.B)
			for i := first; i < last; i++ {
				if onForeground {
					if cell.Color.A != 0 {
						tones[i] = 36
					}
				} else {
					tones[i] = lerp(tones[i], tone, alpha)
					if layer == 1 {
						field.foreground[i], field.foregroundInside[i] = 0, math.Inf(1)
					}
				}
			}
		})
	}
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			i := y*vineFieldWidth + x
			if tones[i] > vineVoidTone && tones[i] < vineLightTone {
				field.clearance[i] = float64(min(x+1, y+1, vineFieldWidth-x, vineFieldHeight-y)) * vineFieldStep
			} else if !onForeground {
				field.unsupported[i] = math.Inf(1)
			}
		}
	}
	fields := [][]float64{field.clearance}
	if !onForeground {
		fields = append(fields, field.unsupported, field.foreground, field.foregroundInside)
	}
	parallelFor(len(fields), func(n int) { chamferVineField(fields[n]) })
	return field
}
