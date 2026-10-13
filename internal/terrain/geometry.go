package terrain

import (
	"math"
	"slices"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type Face struct {
	Poly     []geom.V
	Holes    [][]geom.V
	Min, Max geom.V
	block    int
	Key      [2]int64 // stable world-space interior point
}

// Structural loops are independent of the original material facets. Carving
// replaces loops and attachment state while retaining the immutable bake data.
type Geometry struct {
	guideSupport [][][32]byte
	decorations  *decorationState
	Shapes       []RockShape   // exact editable loops; Grid/Topology retain the bake recipe
	source       *RockTopology // stable shape identity across material binding and cuts
	Grid         RockGrid      // original inset material facets, retained for baking
	Topology     *RockTopology // immutable adjacency and boundaries shared with rendering
	Cuts         []RockCut
	Vegetation   Vegetation
	Faces        []Face
	Guides       []Guide
	Blocks       [][]int
	Top          float64
	Collision    CollisionGeometry
}

func PrepareTerrainGeometry(data SectionData, tolerance float64) *Geometry {
	h := &Geometry{Guides: data.Guides, Top: SectionTop(data.ID), Vegetation: SectionVegetation(data)}
	var grid RockGrid
	source := data.ForegroundTopology
	if source == nil {
		source = NewRockTopology(data.Foreground)
	}
	h.source = source
	h.Cuts = source.Cuts
	for _, cell := range source.Grid {
		if !cell.Raised || cell.Color.A == 0 {
			continue
		}
		lo, hi := geom.PolygonBounds(cell.Polygon)
		center := geom.PolygonCenter(cell.Polygon)
		key := [2]int64{int64(math.Round(center.X * 1e6)), int64(math.Round((center.Y + SectionTop(data.ID)) * 1e6))}
		h.Faces = append(h.Faces, Face{Poly: cell.Polygon, Min: lo, Max: hi, block: -1, Key: key})
		grid = append(grid, cell)
	}
	// Shared edges connect an entire formation, including partial borders
	// left by guide cuts. A corner contact alone keeps two blocks separate.
	neighbors := source.neighbors
	if len(grid) != len(source.Grid) {
		neighbors = RockNeighbors(grid)
		source = &RockTopology{Grid: grid, neighbors: neighbors, Cuts: source.Cuts, Top: source.Top}
		source.prepareBoundary()
	}
	h.Grid = grid
	h.Topology = source
	h.decorations = newDecorationState(h.Vegetation.Mushrooms, source.SpiderWebs)
	for i := range h.Faces {
		if h.Faces[i].block >= 0 {
			continue
		}
		block := len(h.Blocks)
		queue := []int{i}
		h.Faces[i].block = block
		for next := 0; next < len(queue); next++ {
			for _, j := range neighbors[queue[next]] {
				if h.Faces[j].block < 0 {
					h.Faces[j].block = block
					queue = append(queue, j)
				}
			}
		}
		h.Blocks = append(h.Blocks, queue)
	}
	h.Collision = prepareCollisionGeometry(data.ID, grid, source.allBoundary, h, tolerance)
	// Compact structural geometry to union loops. Appearance cells no longer
	// determine edit cost or query identity granularity.
	var shapes []RockShape
	for block := range h.Blocks {
		var edges []RockEdge
		for _, e := range source.allBoundary {
			if h.Faces[e.Cell].block == block {
				edges = append(edges, e)
			}
		}
		loops, ok := TraceCollisionLoops(edges)
		if !ok {
			for _, i := range h.Blocks[block] {
				loops = append(loops, h.Faces[i].Poly)
			}
		}
		shapes = append(shapes, SplitLoops(loops)...)
	}
	h.setShapes(shapes)
	h.prepareGuideSupport()
	if len(h.Cuts) > 0 {
		h.tagAuthoredCutEdges()
	}
	return h
}

func (h *Geometry) tagAuthoredCutEdges() {
	var edges []RockEdge
	for _, cut := range h.Cuts {
		poly := cut.localPolygon(h.Top)
		for i, p := range poly {
			edges = append(edges, RockEdge{A: p, B: poly[(i+1)%len(poly)]})
		}
	}
	index := geom.NewBoundsIndex(edgeBounds(edges, 1e-8))
	for i := range h.Shapes {
		for j, e := range h.Shapes[i].edges {
			p := geom.LerpVector(e.A, e.B, .5)
			index.Query(geom.Bounds{Min: p, Max: p}, func(k int) bool {
				c := edges[k]
				if geom.SegmentDistanceSquared(p, p, c.A, c.B) < 1e-16 {
					h.Shapes[i].edges[j].Cell = -1
					return false
				}
				return true
			})
		}
	}
}

func (h *Geometry) setShapes(shapes []RockShape) {
	h.Shapes = shapes
	h.Faces = nil
	h.Blocks = nil
	for i, s := range shapes {
		poly := s.Loops[0]
		// A canonical boundary point is independent of shade and resolution.
		ordered := geom.OrderPolygon(poly)
		p := geom.LerpVector(ordered[0], ordered[1], .5).Add(ordered[1].Sub(ordered[0]).Norm().Perp().Mul(1e-6))
		key := [2]int64{int64(math.Round(p.X * 1e7)), int64(math.Round((p.Y + h.Top) * 1e7))}
		h.Faces = append(h.Faces, Face{Poly: poly, Holes: s.Loops[1:], Min: s.Min, Max: s.Max, Key: key, block: i})
		h.Blocks = append(h.Blocks, []int{i})
	}
}

func (h *Geometry) CutEdges() []RockEdge {
	var edges []RockEdge
	for _, s := range h.Shapes {
		for _, e := range s.edges {
			if e.Cell < 0 {
				edges = append(edges, e)
			}
		}
	}
	return edges
}

func (h *Geometry) CutEdgesInBounds(bounds geom.Bounds) []RockEdge {
	var edges []RockEdge
	for _, s := range h.Shapes {
		s.index.Query(bounds, func(i int) bool {
			if e := s.edges[i]; e.Cell < 0 {
				edges = append(edges, e)
			}
			return true
		})
	}
	return edges
}

func (h *Geometry) shapeCollision(id int64, tolerance float64) CollisionGeometry {
	c := CollisionGeometry{ID: -id, Top: h.Top, Origin: geom.V{Y: h.Top}, Min: geom.V{Y: h.Top}, Max: geom.V{X: Width, Y: h.Top + SectionHeight}}
	for _, s := range h.Shapes {
		loops := s.Loops
		if tolerance > 0 {
			candidate := make([][]geom.V, len(loops))
			for i, p := range loops {
				candidate[i] = simplifyCollisionLoop(p, tolerance)
			}
			if compatibleCollisionLoops(loops, candidate) {
				loops = candidate
			}
		}
		for _, p := range loops {
			out := make([]geom.V, len(p))
			for i, v := range p {
				out[i] = v.Add(geom.V{Y: h.Top})
			}
			c.Polygons = append(c.Polygons, out)
		}
	}
	return c
}

// BindTerrainMaterial reuses collision and connected components from the early
// opaque snapshot. Shading changes attributes, never polygon occupancy.
func BindTerrainMaterial(early *Geometry, data SectionData) *Geometry {
	h := *early
	h.Topology = data.ForegroundTopology
	h.Grid = h.Topology.Grid
	return &h
}

// BindTerrainVegetation attaches immutable decoration data and its support
// index on the worker before publishing the completed section.
func BindTerrainVegetation(early *Geometry, data SectionData) *Geometry {
	h := *BindTerrainMaterial(early, data)
	h.Vegetation = SectionVegetation(data)
	h.decorations = newDecorationState(h.Vegetation.Mushrooms, h.Topology.SpiderWebs)
	return &h
}

// SameTerrain ignores shading and vegetation but includes runtime edits.
func SameTerrain(a, b *Geometry) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.source == nil || b.source == nil {
		return a.Topology == b.Topology && sameTerrainCuts(a.Cuts, b.Cuts)
	}
	return a.source == b.source && sameTerrainCuts(a.Cuts, b.Cuts)
}

func sameTerrainCuts(a, b []RockCut) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 || &a[0] == &b[0] {
		return true
	}
	for i, cut := range a {
		other := b[i]
		if cut.Min != other.Min || cut.Max != other.Max || len(cut.Poly) != len(other.Poly) {
			return false
		}
		if len(cut.Poly) > 0 && &cut.Poly[0] == &other.Poly[0] {
			continue
		}
		if !slices.Equal(cut.Poly, other.Poly) {
			return false
		}
	}
	return true
}
