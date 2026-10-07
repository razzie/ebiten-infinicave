package infinicave

import (
	"image/color"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
)

type rockEdge struct {
	A, B V
	Cell int
}

// The final inset grid is immutable through mesh/collision preparation. Share
// its adjacency and boundary instead of rediscovering partial edges per layer.
type rockTopology struct {
	grid        RockGrid
	neighbors   [][]int
	boundary    []rockEdge
	allBoundary []rockEdge // includes generation-window edges for collision
	cuts        []rockCut
	top         float64
}

func newRockTopology(grid RockGrid) *rockTopology {
	t := &rockTopology{grid: insetForegroundGrid(grid)}
	t.neighbors = rockNeighbors(t.grid)
	t.prepareBoundary()
	return t
}

func (t *rockTopology) prepareBoundary() {
	t.allBoundary = rockBoundaryEdges(t.grid, t.neighbors, true)
	for _, edge := range t.allBoundary {
		if !rockWindowEdge(edge.A, edge.B) {
			t.boundary = append(t.boundary, edge)
		}
	}
}

func rockWindowEdge(a, b V) bool {
	return (a.X == b.X && (a.X == 0 || a.X == generationWidth)) ||
		(a.Y == b.Y && (a.Y == generationMinY || a.Y == generationMaxY))
}

// Subdivide partial shared edges before cancellation, so merged guide faces
// never grow a side wall through a neighboring rock. Corner contacts survive.
func exposedRockEdges(grid RockGrid) []rockEdge {
	return rockBoundaryEdges(grid, rockNeighbors(grid), false)
}

func rockBoundaryEdges(grid RockGrid, neighbors [][]int, includeWindow bool) []rockEdge {
	type entry struct {
		edge  rockEdge
		count int
	}
	edges := make(map[[4]int64]entry)
	for i, c := range grid {
		for j, a := range c.Polygon {
			b := c.Polygon[(j+1)%len(c.Polygon)]
			d := b.Sub(a)
			if d.Len2() < 1e-18 {
				continue
			}
			// Generation-window cuts have no physical thickness.
			if !includeWindow && rockWindowEdge(a, b) {
				continue
			}
			ts := []float64{0, 1}
			for _, k := range neighbors[i] {
				for _, p := range grid[k].Polygon {
					t := p.Sub(a).Dot(d) / d.Len2()
					if t > 0 && t < 1 && p.Sub(a.Add(d.Mul(t))).Len() < mergeTolerance {
						ts = append(ts, t)
					}
				}
			}
			sort.Float64s(ts)
			for k := 1; k < len(ts); k++ {
				p, q := a.Add(d.Mul(ts[k-1])), a.Add(d.Mul(ts[k]))
				if p.Sub(q).Len() < mergeTolerance {
					continue
				}
				key := edgeKey(p, q)
				e := edges[key]
				e.count++
				e.edge = rockEdge{p, q, i}
				edges[key] = e
			}
		}
	}
	keys := make([][4]int64, 0, len(edges))
	for key, e := range edges {
		if e.count == 1 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		for k := 0; k < 4; k++ {
			if keys[i][k] != keys[j][k] {
				return keys[i][k] < keys[j][k]
			}
		}
		return false
	})
	out := make([]rockEdge, 0, len(keys))
	for _, key := range keys {
		out = append(out, edges[key].edge)
	}
	return out
}

func appendRockQuad(vertices []ebiten.Vertex, indices []uint32, points [4]V, clr color.NRGBA) ([]ebiten.Vertex, []uint32) {
	first := uint32(len(vertices))
	for _, p := range points {
		vertices = append(vertices, ebiten.Vertex{
			DstX: float32(p.X * rasterPixelsPerUnit), DstY: float32((p.Y - generationMinY) * rasterPixelsPerUnit), SrcX: float32(p.X * rasterPixelsPerUnit), SrcY: float32((p.Y - generationMinY) * rasterPixelsPerUnit),
			ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255, ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
			Custom0: 2,
		})
	}
	return vertices, append(indices, first, first+1, first+2, first, first+2, first+3)
}

// A shallow oblique view exposes the drop below a silhouette. Draw walls
// before the top faces; their inward portions are naturally hidden by rock.
func appendRockWalls(vertices []ebiten.Vertex, indices []uint32, grid RockGrid, edges []rockEdge, viewMode View) ([]ebiten.Vertex, []uint32) {
	if len(grid) == 0 || !grid[0].Raised {
		return vertices, indices
	}
	for _, edge := range edges {
		c := grid[edge.Cell]
		outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
		view := c.orientation.internal(V{.045, .08})
		if outward.Dot(view) <= 0 {
			continue
		}
		za, zb := math.Max(0, c.depthAt(edge.A)+.008), math.Max(0, c.depthAt(edge.B)+.008)
		a := edge.A.Add(view.Mul(za))
		b := edge.B.Add(view.Mul(zb))
		normal := (V3{outward.X, outward.Y, .1}).Norm()
		clr := rockSurfaceColor(normal, c.Shadow*.65, c.Ambient*.55, c.orientation)
		clr = rockViewColor(RockCell{Color: clr}, viewMode)
		vertices, indices = appendRockQuad(vertices, indices, [4]V{edge.A, edge.B, b, a}, clr)
	}
	return vertices, indices
}

func rockViewColor(c RockCell, view View) color.NRGBA {
	switch view {
	case ViewHeight:
		v := uint8(math.Round(255 * clamp((c.Z+.012)/.112, 0, 1)))
		return color.NRGBA{v, v, v, 255}
	case ViewNormals:
		n := c.orientation.normal(c.Normal)
		return color.NRGBA{uint8(127.5 * (n.X + 1)), uint8(127.5 * (n.Y + 1)), uint8(127.5 * (n.Z + 1)), 255}
	case ViewShadows:
		v := uint8(math.Round(255 * c.Shadow))
		return color.NRGBA{v, v, v, 255}
	case ViewClay:
		v := uint8(math.Round(.30*float64(c.Color.R) + .59*float64(c.Color.G) + .11*float64(c.Color.B)))
		return color.NRGBA{v, v, v, c.Color.A}
	}
	return c.Color
}

// A restrained bevel belongs only to an exposed silhouette. Internal cell
// edges have neither a raised rim nor a lighting seam from triangulation.
func appendRockBevels(vertices []ebiten.Vertex, indices []uint32, grid RockGrid, edges []rockEdge, view View) ([]ebiten.Vertex, []uint32) {
	for _, edge := range edges {
		c := grid[edge.Cell]
		inward := edge.B.Sub(edge.A).Perp().Norm()
		width := math.Min(.00045, edge.B.Sub(edge.A).Len()*.06)
		a, b := edge.A.Add(inward.Mul(width)), edge.B.Add(inward.Mul(width))
		if !insideFace(lerpV(a, b, .5), c.Polygon) {
			continue
		}
		normal := (V3{c.Normal.X - inward.X*.5, c.Normal.Y - inward.Y*.5, c.Normal.Z}).Norm()
		clr := rockViewColor(RockCell{Color: rockSurfaceColor(normal, c.Shadow, c.Ambient, c.orientation)}, view)
		vertices, indices = appendRockQuad(vertices, indices, [4]V{edge.A, edge.B, b, a}, clr)
	}
	return vertices, indices
}
