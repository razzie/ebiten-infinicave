package main

import (
	_ "embed"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	guideHoverRadius = 6.0
	hoverPadding     = 16.0
)

//go:embed hover.kage
var hoverShaderSource []byte

type hoverFace struct {
	poly     []V
	min, max V
	block    int
	key      [2]int64 // stable world-space interior point
}

// Retain the same clipped faces used for rendering, without GPU readbacks or
// regenerating terrain while the mouse moves. Geometry belongs to its section.
type hoverGeometry struct {
	faces  []hoverFace
	guides []Guide
	blocks [][]int
	top    float64
}

func polygonBounds(poly []V) (lo, hi V) {
	lo, hi = poly[0], poly[0]
	for _, p := range poly[1:] {
		lo = V{math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)}
		hi = V{math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)}
	}
	return
}

func prepareHoverGeometry(data sectionData) *hoverGeometry {
	h := &hoverGeometry{guides: data.guides, top: sectionWindowTop(data.id)}
	var grid RockGrid
	for _, cell := range insetForegroundGrid(data.foreground) {
		if !cell.Raised || cell.Color.A == 0 {
			continue
		}
		lo, hi := polygonBounds(cell.Polygon)
		center := faceCenter(cell.Polygon)
		key := [2]int64{int64(math.Round(center.X * 1000)), int64(math.Round((center.Y + sectionWindowTop(data.id)) * 1000))}
		h.faces = append(h.faces, hoverFace{poly: cell.Polygon, min: lo, max: hi, block: -1, key: key})
		grid = append(grid, cell)
	}
	// Shared edges connect an entire formation, including partial borders
	// left by guide cuts. A corner contact alone keeps two blocks separate.
	neighbors := rockNeighbors(grid)
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
	return h
}

type hoverTarget struct {
	geometry *hoverGeometry
	index    int
	guide    bool
}

func (h *hoverGeometry) hit(p V) hoverTarget {
	if h == nil || p.X < foregroundScreenInset || p.X > W-foregroundScreenInset {
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
			if guideSegmentsDistance2(p, p, a, face.poly[(j+1)%len(face.poly)]) <= 1e-12 {
				return hoverTarget{geometry: h, index: face.block}
			}
		}
	}
	return hoverTarget{}
}

func (w *World) hoverAt(cursor V, cameraY float64, height int) hoverTarget {
	if cursor.X < 0 || cursor.X >= W || cursor.Y < 0 || cursor.Y >= float64(height) {
		return hoverTarget{}
	}
	// Match World's rounded rendering origin, including during camera glides.
	p := cursor.Add(V{Y: math.Round(cameraY)})
	if p.Y >= 0 {
		return hoverTarget{}
	}
	id := max(0, int64(math.Ceil(-p.Y/sectionHeight))-1)
	if section := w.sections[id]; section != nil {
		top := sectionWindowTop(id)
		return section.hover.hit(p.Sub(V{Y: top}))
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
func (w *World) hoverPolygons(target hoverTarget, low, high int64) [][]V {
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
			h := w.sections[id].hover
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
		h := w.sections[id].hover
		if h == nil {
			continue
		}
		for _, face := range h.faces {
			if !selected[hoverTarget{geometry: h, index: face.block}] {
				continue
			}
			// Use each section's visible band once; padded overlaps must not
			// brighten the tint or expose geometry outside the loaded terrain.
			poly := clipHalfPlane(face.poly, V{0, -1}, -W)
			poly = clipHalfPlane(poly, V{0, 1}, 2*W)
			if len(poly) < 3 {
				continue
			}
			worldPoly := make([]V, len(poly))
			for i, p := range poly {
				worldPoly[i] = p.Add(V{Y: h.top})
			}
			worldPoly = clipHalfPlane(worldPoly, V{0, -1}, -(sectionTop(high) - hoverPadding))
			worldPoly = clipHalfPlane(worldPoly, V{0, 1}, math.Min(0, sectionTop(low)+W+hoverPadding))
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

func (r *hoverRenderer) selectTarget(target hoverTarget, w *World, low, high int64) {
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
	r.origin = V{math.Floor(lo.X - hoverPadding), math.Floor(lo.Y - hoverPadding)}
	width := max(1, int(math.Ceil(hi.X+hoverPadding-r.origin.X)))
	height := max(1, int(math.Ceil(hi.Y+hoverPadding-r.origin.Y)))
	mask := ebiten.NewImage(width, height)
	defer mask.Deallocate()
	var path vector.Path
	for _, poly := range polygons {
		path.MoveTo(float32(poly[0].X-r.origin.X), float32(poly[0].Y-r.origin.Y))
		for _, p := range poly[1:] {
			path.LineTo(float32(p.X-r.origin.X), float32(p.Y-r.origin.Y))
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

func (g *Game) drawHover(screen *ebiten.Image) {
	if !ebiten.IsFocused() {
		g.highlight.clear()
		return
	}
	x, y := ebiten.CursorPositionF()
	target := g.world.hoverAt(V{x, y}, g.camera.Y, g.camera.Height)
	low, high := visibleSections(math.Round(g.camera.Y), g.camera.Height)
	g.highlight.selectTarget(target, g.world, low, high)
	if g.highlight.image != nil {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(g.highlight.origin.X, g.highlight.origin.Y-math.Round(g.camera.Y))
		screen.DrawImage(g.highlight.image, op)
	}
}
