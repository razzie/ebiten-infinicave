package render

import (
	"image"
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Prepared meshes contain only CPU data. A worker owns them until it sends
// the section through results; the game loop then owns all GPU resources.
type TriangleMesh struct {
	Vertices      []ebiten.Vertex
	Indices       []uint32
	fillRule      ebiten.FillRule
	premultiplied bool
}

type GridMesh struct {
	Faces    TriangleMesh
	Outlines []TriangleMesh
}

type SectionMesh struct {
	ID                                                  int64
	Background, Foreground                              GridMesh
	Vines                                               []TriangleMesh
	ForegroundVines                                     []TriangleMesh
	Mushrooms                                           TriangleMesh
	Geometry                                            *terrain.Geometry
	VinesBounds, ForegroundVinesBounds, MushroomsBounds image.Rectangle
}

func PrepareSection(data terrain.SectionData, view View) SectionMesh {
	mesh := SectionMesh{ID: data.ID}
	jobs := make(chan func(), 5)
	jobs <- func() { mesh.Background = prepareGrid(data.Background, view) }
	jobs <- func() { mesh.Foreground = PrepareGridWithTopology(data.Foreground, view, data.ForegroundTopology) }
	if view == ViewShaded {
		jobs <- func() { mesh.Vines = PrepareVines(data.Vines) }
		jobs <- func() { mesh.ForegroundVines = PrepareForegroundVines(data.ForegroundVines, data.Orientation) }
		jobs <- func() { mesh.Mushrooms = PrepareMushrooms(data.Mushrooms) }
	}
	close(jobs)

	// Each layer owns its output buffers. Match parallelFor's CPU budget so
	// polygonization also leaves capacity for the game loop.
	workers := min(max(1, runtime.GOMAXPROCS(0)-1), len(jobs))
	if workers == 1 {
		for job := range jobs {
			job()
		}
		return mesh
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for job := range jobs {
				job()
			}
		})
	}
	wg.Wait()
	return mesh
}

func (m TriangleMesh) Draw(dst, white *ebiten.Image) {
	if len(m.Indices) == 0 {
		return
	}
	op := &ebiten.DrawTrianglesOptions{AntiAlias: true, FillRule: m.fillRule}
	if m.premultiplied {
		op.ColorScaleMode = ebiten.ColorScaleModePremultipliedAlpha
	}
	dst.DrawTriangles32(m.Vertices, m.Indices, white, op)
}

// Render each tessellation separately so transparent guide cells contribute
// neither faces nor outlines, and the foreground covers the background mesh.
func prepareGrid(grid terrain.RockGrid, view View) GridMesh {
	return PrepareGridWithTopology(grid, view, nil)
}

func PrepareGridWithTopology(grid terrain.RockGrid, view View, topology *terrain.RockTopology) GridMesh {
	if topology != nil {
		grid = topology.Grid
	} else if len(grid) > 0 && grid[0].Raised {
		grid = terrain.InsetForegroundGrid(grid)
	}
	vertices := make([]ebiten.Vertex, 0, len(grid)*18)
	indices := make([]uint32, 0, len(grid)*18)
	var boundary []terrain.RockEdge
	raised := len(grid) > 0 && grid[0].Raised
	if raised {
		if topology != nil {
			boundary = topology.Boundary
		} else {
			boundary = terrain.ExposedRockEdges(grid)
		}
	}
	grid = terrain.ShadeRockFaces(grid, boundary)
	if raised {
		if view == ViewShaded || view == ViewClay {
			vertices, indices = appendRockWalls(vertices, indices, grid, boundary, view)
		}
	}
	type cellEdge struct {
		a, b  geom.V
		alpha uint8
		cell  int
	}
	edges := make(map[[4]int64]cellEdge)
	hiddenEdges := make(map[[4]int64]bool)
	for cellIndex, cell := range grid {
		s, clr, poly := cell.Center, rockViewColor(cell, view), cell.Polygon
		if clr.A == 0 {
			continue
		}
		vertices, indices = appendCellMesh(vertices, indices, poly, s, clr, cell.Normal)
		if raised {
			continue // Foreground seams are selected from actual shared relief.
		}
		for j, a := range poly {
			b := poly[(j+1)%len(poly)]
			key := geom.EdgeKey(a, b)
			// Preserve quiet black pockets in the underlying grid.
			if clr.A == 255 && clr.R <= 3 && clr.G <= 3 && clr.B <= 3 {
				hiddenEdges[key] = true
			}
			// Background silhouettes are quiet; shared joins receive a seam
			// only where neighboring planes form a recess or a depth step.
			alpha := uint8(math.Round((18 + 20*terrain.SurfaceLight(cell.Normal, terrain.RockOrientation(cell))) * float64(clr.A) / 255))
			if previous, shared := edges[key]; shared {
				alpha = uint8(math.Round(40 * rockCreviceStrength(grid[previous.cell], cell, a.Add(b).Mul(.5))))
			}
			if view != ViewShaded {
				alpha = 0
			}
			edges[key] = cellEdge{a, b, alpha, cellIndex}
		}
	}

	if raised && view == ViewShaded {
		for _, seam := range rockCrevices(grid) {
			edges[geom.EdgeKey(seam.a, seam.b)] = cellEdge{a: seam.a, b: seam.b, alpha: seam.alpha}
		}
	}
	if view == ViewShaded || view == ViewClay {
		vertices, indices = appendRockBevels(vertices, indices, grid, boundary, view)
	}
	if topology != nil && len(topology.Cuts) > 0 && (view == ViewShaded || view == ViewClay) {
		vertices, indices = AppendCarveRims(vertices, indices, topology, view)
	}
	mesh := GridMesh{Faces: TriangleMesh{Vertices: vertices, Indices: indices}}
	if len(indices) == 0 {
		return mesh
	}

	// Shared edges are stroked once, using the more visible adjacent cell.
	// Outline opacity follows the faces, including at transparent boundaries.
	// Each disconnected edge needs at most five stroke vertices. Bound paths
	// so the vector tessellator's 16-bit indices cannot overflow.
	const maxPathEdges = 4096
	type outlinePath struct {
		path  vector.Path
		edges int
	}
	var paths [256][]*outlinePath
	// Stable path order also keeps antialiasing identical after cache eviction.
	keys := make([][4]int64, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		for k := 0; k < 4; k++ {
			if keys[i][k] != keys[j][k] {
				return keys[i][k] < keys[j][k]
			}
		}
		return false
	})
	for _, key := range keys {
		edge := edges[key]
		if hiddenEdges[key] || edge.alpha == 0 {
			continue
		}
		group := paths[edge.alpha]
		if len(group) == 0 || group[len(group)-1].edges == maxPathEdges {
			group = append(group, &outlinePath{})
			paths[edge.alpha] = group
		}
		path := group[len(group)-1]
		path.path.MoveTo(float32(edge.a.X*RasterPixelsPerUnit), float32((edge.a.Y-terrain.GenerationMinY)*RasterPixelsPerUnit))
		path.path.LineTo(float32(edge.b.X*RasterPixelsPerUnit), float32((edge.b.Y-terrain.GenerationMinY)*RasterPixelsPerUnit))
		path.edges++
	}
	for alpha, group := range paths {
		for _, path := range group {
			var stroke vector.Path
			stroke.AddStroke(&path.path, &vector.AddStrokeOptions{StrokeOptions: vector.StrokeOptions{
				Width: 1.05, LineCap: vector.LineCapButt, LineJoin: vector.LineJoinRound,
			}})
			vs, is := stroke.AppendVerticesAndIndicesForFilling(nil, nil)
			a := float32(alpha) / 255
			r, g, b := float32(18), float32(18), float32(17)
			if !raised {
				r, g, b = 3, 5, 6
			}
			for i := range vs {
				vs[i].SrcX, vs[i].SrcY = .5, .5
				vs[i].ColorR, vs[i].ColorG, vs[i].ColorB, vs[i].ColorA = r/255*a, g/255*a, b/255*a, a
			}
			indices := make([]uint32, len(is))
			for i, index := range is {
				indices[i] = uint32(index)
			}
			mesh.Outlines = append(mesh.Outlines, TriangleMesh{
				Vertices: vs, Indices: indices,
				fillRule: ebiten.FillRuleNonZero, premultiplied: true,
			})
		}
	}
	return mesh
}
