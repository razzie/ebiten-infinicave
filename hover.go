package infinicave

import (
	_ "embed"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	guideHoverRadius = .006
	hoverPadding     = .016
)

//go:embed hover.kage
var hoverShaderSource []byte

type terrainFace struct {
	poly     []V
	min, max V
	block    int
	key      [2]int64 // stable world-space interior point
}

// Retain the same clipped faces used for rendering and derive collision
// boundaries from their union. Geometry belongs to its section, regardless
// of whether the application draws hover highlights.
type terrainGeometry struct {
	grid       RockGrid // final inset faces, retained for terrain edits
	cuts       []rockCut
	vegetation vegetationGeometry
	faces      []terrainFace
	guides     []Guide
	blocks     [][]int
	top        float64
	collision  CollisionGeometry
}

func polygonBounds(poly []V) (lo, hi V) {
	lo, hi = poly[0], poly[0]
	for _, p := range poly[1:] {
		lo = V{math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)}
		hi = V{math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)}
	}
	return
}

func prepareTerrainGeometry(data sectionData, tolerance float64) *terrainGeometry {
	h := &terrainGeometry{guides: data.guides, top: sectionTop(data.id), vegetation: sectionVegetation(data)}
	var grid RockGrid
	source := data.foregroundTopology
	if source == nil {
		source = newRockTopology(data.foreground)
	}
	h.cuts = source.cuts
	for _, cell := range source.grid {
		if !cell.Raised || cell.Color.A == 0 {
			continue
		}
		lo, hi := polygonBounds(cell.Polygon)
		center := faceCenter(cell.Polygon)
		key := [2]int64{int64(math.Round(center.X * 1e6)), int64(math.Round((center.Y + sectionTop(data.id)) * 1e6))}
		h.faces = append(h.faces, terrainFace{poly: cell.Polygon, min: lo, max: hi, block: -1, key: key})
		grid = append(grid, cell)
	}
	// Shared edges connect an entire formation, including partial borders
	// left by guide cuts. A corner contact alone keeps two blocks separate.
	neighbors := source.neighbors
	if len(grid) != len(source.grid) {
		neighbors = rockNeighbors(grid)
	}
	h.grid = grid
	for i := range h.faces {
		if h.faces[i].block >= 0 {
			continue
		}
		block := len(h.blocks)
		queue := []int{i}
		h.faces[i].block = block
		for next := 0; next < len(queue); next++ {
			for _, j := range neighbors[queue[next]] {
				if h.faces[j].block < 0 {
					h.faces[j].block = block
					queue = append(queue, j)
				}
			}
		}
		h.blocks = append(h.blocks, queue)
	}
	h.collision = prepareCollisionGeometry(data.id, grid, neighbors, h, tolerance)
	return h
}

type hoverTarget struct {
	geometry *terrainGeometry
	index    int
	guide    bool
}

func (h *terrainGeometry) hit(p V) hoverTarget {
	if h == nil || p.X < foregroundScreenInset || p.X > generationWidth-foregroundScreenInset {
		return hoverTarget{}
	}
	// A crest can share an edge with two rock faces. Prefer the guide so
	// pointing at it never also fills an adjacent face.
	if i, pr := nearestGuide(p, h.guides); i >= 0 && pr.Dist <= guideHoverRadius {
		return hoverTarget{geometry: h, index: i, guide: true}
	}
	for _, face := range h.faces {
		if p.X < face.min.X || p.X > face.max.X || p.Y < face.min.Y || p.Y > face.max.Y {
			continue
		}
		if insideFace(p, face.poly) {
			return hoverTarget{geometry: h, index: face.block}
		}
		// insideFace intentionally excludes boundaries for guide splitting;
		// hover selection includes them to avoid flickering along cell seams.
		for j, a := range face.poly {
			if guideSegmentsDistance2(p, p, a, face.poly[(j+1)%len(face.poly)]) <= 1e-18 {
				return hoverTarget{geometry: h, index: face.block}
			}
		}
	}
	return hoverTarget{}
}

func (w *world) hoverAt(cursor V, cameraY float64, height float64) hoverTarget {
	if cursor.X < 0 || cursor.X >= generationWidth || cursor.Y < 0 || cursor.Y >= height {
		return hoverTarget{}
	}
	// Match world's rounded rendering origin, including during camera glides.
	p := cursor.Add(V{Y: rasterAlignedY(cameraY)})
	if p.Y >= 0 {
		return hoverTarget{}
	}
	id := max(0, int64(math.Ceil(-p.Y/sectionHeight))-1)
	if section := w.sections[id]; section != nil {
		top := sectionTop(id)
		return section.geometry.hit(p.Sub(V{Y: top}))
	}
	return hoverTarget{}
}

type hoverRenderer struct {
	shader    *ebiten.Shader
	target    hoverTarget
	image     *ebiten.Image
	origin    V // world coordinates
	revision  uint64
	low, high int64
}

// Overlapping generation windows share world-space faces. Follow those faces
// between cached sections so a formation stays lit across section boundaries.
func (w *world) hoverPolygons(target hoverTarget, low, high int64) [][]V {
	if target.guide {
		g := target.geometry.guides[target.index]
		poly := make([]V, len(g.Pts))
		for i, p := range g.Pts {
			poly[i] = p.Add(V{Y: target.geometry.top})
		}
		return [][]V{poly}
	}
	keys := make(map[[2]int64]bool)
	selected := make(map[hoverTarget]bool)
	add := func(t hoverTarget) {
		selected[t] = true
		for _, i := range t.geometry.blocks[t.index] {
			keys[t.geometry.faces[i].key] = true
		}
	}
	add(target)
	ids := make([]int64, 0, len(w.sections))
	for id := range w.sections {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for changed := true; changed; {
		changed = false
		for _, id := range ids {
			h := w.sections[id].geometry
			if h == nil {
				continue
			}
			for block, faces := range h.blocks {
				t := hoverTarget{geometry: h, index: block}
				if selected[t] {
					continue
				}
				for _, i := range faces {
					if keys[h.faces[i].key] {
						add(t)
						changed = true
						break
					}
				}
			}
		}
	}
	var polygons [][]V
	for _, id := range ids {
		if id < max(0, low-1) || id > high+1 {
			continue
		}
		h := w.sections[id].geometry
		if h == nil {
			continue
		}
		for _, face := range h.faces {
			if !selected[hoverTarget{geometry: h, index: face.block}] {
				continue
			}
			// Use each section's visible band once; padded overlaps must not
			// brighten the tint or expose geometry outside the loaded terrain.
			poly := clipHalfPlane(face.poly, V{0, -1}, 0)
			poly = clipHalfPlane(poly, V{0, 1}, SectionHeight)
			if len(poly) < 3 {
				continue
			}
			worldPoly := make([]V, len(poly))
			for i, p := range poly {
				worldPoly[i] = p.Add(V{Y: h.top})
			}
			worldPoly = clipHalfPlane(worldPoly, V{0, -1}, -(sectionTop(high) - hoverPadding))
			worldPoly = clipHalfPlane(worldPoly, V{0, 1}, math.Min(0, sectionTop(low)+generationWidth+hoverPadding))
			if len(worldPoly) >= 3 {
				polygons = append(polygons, worldPoly)
			}
		}
	}
	return polygons
}

func newHoverRenderer() (*hoverRenderer, error) {
	shader, err := ebiten.NewShader(hoverShaderSource)
	if err != nil {
		return nil, err
	}
	return &hoverRenderer{shader: shader}, nil
}

func (r *hoverRenderer) clear() {
	if r.image != nil {
		r.image.Deallocate()
		r.image = nil
	}
	r.target = hoverTarget{}
}

func (r *hoverRenderer) close() {
	r.clear()
	r.shader.Deallocate()
}

func (r *hoverRenderer) selectTarget(target hoverTarget, w *world, low, high int64) {
	if target == r.target && (target.geometry == nil || (r.revision == w.revision && r.low == low && r.high == high)) {
		return
	}
	r.clear()
	if target.geometry == nil {
		return
	}
	r.target = target
	r.revision, r.low, r.high = w.revision, low, high
	polygons := w.hoverPolygons(target, low, high)
	if len(polygons) == 0 {
		return
	}
	lo, hi := polygonBounds(polygons[0])
	for _, poly := range polygons[1:] {
		a, b := polygonBounds(poly)
		lo = V{math.Min(lo.X, a.X), math.Min(lo.Y, a.Y)}
		hi = V{math.Max(hi.X, b.X), math.Max(hi.Y, b.Y)}
	}
	r.origin = V{math.Floor((lo.X-hoverPadding)*rasterPixelsPerUnit+1e-9) / rasterPixelsPerUnit,
		math.Floor((lo.Y-hoverPadding)*rasterPixelsPerUnit+1e-9) / rasterPixelsPerUnit}
	width := max(1, int(math.Ceil((hi.X+hoverPadding-r.origin.X)*rasterPixelsPerUnit-1e-9)))
	height := max(1, int(math.Ceil((hi.Y+hoverPadding-r.origin.Y)*rasterPixelsPerUnit-1e-9)))
	mask := ebiten.NewImage(width, height)
	defer mask.Deallocate()
	var path vector.Path
	for _, poly := range polygons {
		path.MoveTo(float32((poly[0].X-r.origin.X)*rasterPixelsPerUnit), float32((poly[0].Y-r.origin.Y)*rasterPixelsPerUnit))
		for _, p := range poly[1:] {
			path.LineTo(float32((p.X-r.origin.X)*rasterPixelsPerUnit), float32((p.Y-r.origin.Y)*rasterPixelsPerUnit))
		}
		if !target.guide {
			path.Close()
		}
	}
	if target.guide {
		var stroke vector.Path
		stroke.AddStroke(&path, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{
			Width: 2, LineCap: vector.LineCapRound, LineJoin: vector.LineJoinRound,
		}})
		path = stroke
	}
	vector.FillPath(mask, &path, &vector.FillOptions{FillRule: vector.FillRuleNonZero}, &vector.DrawPathOptions{AntiAlias: true})

	// Blur only when the hovered object changes, then reuse the finished
	// overlay as both the cursor and camera move within that object.
	blur := ebiten.NewImage(width, height)
	defer blur.Deallocate()
	blur.DrawRectShader(width, height, r.shader, &ebiten.DrawRectShaderOptions{
		Images:   [4]*ebiten.Image{mask},
		Uniforms: map[string]any{"Final": float32(0)},
	})
	strength := []float32{.10, .16}
	if target.guide {
		strength = []float32{.7, .7}
	}
	r.image = ebiten.NewImage(width, height)
	r.image.DrawRectShader(width, height, r.shader, &ebiten.DrawRectShaderOptions{
		Images:   [4]*ebiten.Image{mask, blur},
		Uniforms: map[string]any{"Final": float32(1), "Strength": strength},
	})
}

// DrawHover draws an optional highlight at a cursor in viewport-local scene units.
// Convert image pixel coordinates by multiplying by Width / screen.Bounds().Dx().
// Call it after Draw. The caller controls whether the cursor is active or focused.
func (g *Scene) DrawHover(screen *ebiten.Image, viewport Viewport, x, y float64) {
	if g.closed || g.highlight == nil || !viewport.valid() {
		return
	}
	target := g.world.hoverAt(V{x, y}, viewport.Y, viewport.Height)
	low, high := visibleSections(rasterAlignedY(viewport.Y), viewport.Height)
	g.highlight.selectTarget(target, g.world, low, high)
	if g.highlight.image != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(g.highlight.origin.X*rasterPixelsPerUnit, (g.highlight.origin.Y-rasterAlignedY(viewport.Y))*rasterPixelsPerUnit)
		scale := float64(screen.Bounds().Dx()) / rasterPixelsPerUnit
		op.GeoM.Scale(scale, scale)
		screen.DrawImage(g.highlight.image, op)
	}
}
