package infinicave

import (
	"image"
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Prepared meshes contain only CPU data. A worker owns them until it sends
// the section through results; the game loop then owns all GPU resources.
type triangleMesh struct {
	vertices      []ebiten.Vertex
	indices       []uint32
	fillRule      ebiten.FillRule
	premultiplied bool
}

type gridMesh struct {
	faces    triangleMesh
	outlines []triangleMesh
}

type sectionMesh struct {
	id                                                  int64
	background, foreground                              gridMesh
	vines                                               []triangleMesh
	foregroundVines                                     []triangleMesh
	mushrooms                                           triangleMesh
	geometry                                            *terrainGeometry
	vinesBounds, foregroundVinesBounds, mushroomsBounds image.Rectangle
}

func prepareSection(data sectionData, view View) sectionMesh {
	mesh := sectionMesh{id: data.id}
	jobs := make(chan func(), 5)
	jobs <- func() { mesh.background = prepareGrid(data.background, view) }
	jobs <- func() { mesh.foreground = prepareGridWithTopology(data.foreground, view, data.foregroundTopology) }
	if view == ViewShaded {
		jobs <- func() { mesh.vines = prepareVines(data.vines) }
		jobs <- func() { mesh.foregroundVines = prepareForegroundVines(data.foregroundVines) }
		jobs <- func() { mesh.mushrooms = prepareMushrooms(data.mushrooms) }
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

func (m triangleMesh) draw(dst, white *ebiten.Image) {
	if len(m.indices) == 0 {
		return
	}
	op := &ebiten.DrawTrianglesOptions{AntiAlias: true, FillRule: m.fillRule}
	if m.premultiplied {
		op.ColorScaleMode = ebiten.ColorScaleModePremultipliedAlpha
	}
	dst.DrawTriangles32(m.vertices, m.indices, white, op)
}

func (g *Scene) drawGridFaces(dst *ebiten.Image, mesh triangleMesh, top float64) {
	if len(mesh.indices) == 0 {
		return
	}
	dst.DrawTrianglesShader32(mesh.vertices, mesh.indices, g.material, &ebiten.DrawTrianglesShaderOptions{
		AntiAlias: true,
		Uniforms: map[string]any{
			"Texture": float32(g.texture),
			"Offset":  []float32{317, float32(top*rasterPixelsPerUnit) + 791},
		},
	})
}

// Render each tessellation separately so transparent guide cells contribute
// neither faces nor outlines, and the foreground covers the background mesh.
func prepareGrid(grid RockGrid, view View) gridMesh {
	return prepareGridWithTopology(grid, view, nil)
}

func prepareGridWithTopology(grid RockGrid, view View, topology *rockTopology) gridMesh {
	if topology != nil {
		grid = topology.grid
	} else if len(grid) > 0 && grid[0].Raised {
		grid = insetForegroundGrid(grid)
	}
	vertices := make([]ebiten.Vertex, 0, len(grid)*18)
	indices := make([]uint32, 0, len(grid)*18)
	var boundary []rockEdge
	if len(grid) > 0 && grid[0].Raised && (view == ViewShaded || view == ViewClay) {
		if topology != nil {
			boundary = topology.boundary
		} else {
			boundary = exposedRockEdges(grid)
		}
		vertices, indices = appendRockWalls(vertices, indices, grid, boundary, view)
	}
	type cellEdge struct {
		a, b  V
		alpha uint8
	}
	edges := make(map[[4]int64]cellEdge)
	hiddenEdges := make(map[[4]int64]bool)
	for _, cell := range grid {
		s, clr, poly := cell.Center, rockViewColor(cell, view), cell.Polygon
		if clr.A == 0 {
			continue
		}
		vertices, indices = appendCellMesh(vertices, indices, poly, s, clr, cell.Normal)
		for j, a := range poly {
			b := poly[(j+1)%len(poly)]
			key := edgeKey(a, b)
			// Preserve quiet black pockets in the underlying grid.
			if clr.A == 255 && clr.R <= 3 && clr.G <= 3 && clr.B <= 3 {
				hiddenEdges[key] = true
			}
			alpha := uint8(math.Round((96 + 28*surfaceLight(cell.Normal)) * float64(clr.A) / 255))
			if view != ViewShaded {
				alpha = 0
			}
			if previous, ok := edges[key]; !ok || alpha > previous.alpha {
				edges[key] = cellEdge{a, b, alpha}
			}
		}
	}

	vertices, indices = appendRockBevels(vertices, indices, grid, boundary, view)
	mesh := gridMesh{faces: triangleMesh{vertices: vertices, indices: indices}}
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
		path.path.MoveTo(float32(edge.a.X*rasterPixelsPerUnit), float32((edge.a.Y-generationMinY)*rasterPixelsPerUnit))
		path.path.LineTo(float32(edge.b.X*rasterPixelsPerUnit), float32((edge.b.Y-generationMinY)*rasterPixelsPerUnit))
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
			for i := range vs {
				vs[i].SrcX, vs[i].SrcY = .5, .5
				vs[i].ColorR, vs[i].ColorG, vs[i].ColorB, vs[i].ColorA = 18.0/255*a, 18.0/255*a, 17.0/255*a, a
			}
			indices := make([]uint32, len(is))
			for i, index := range is {
				indices[i] = uint32(index)
			}
			mesh.outlines = append(mesh.outlines, triangleMesh{
				vertices: vs, indices: indices,
				fillRule: ebiten.FillRuleNonZero, premultiplied: true,
			})
		}
	}
	return mesh
}
