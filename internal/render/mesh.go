package render

import (
	"image"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Prepared meshes contain only CPU data. A worker owns them until it sends
// the section through results; the game loop then owns all GPU resources.
type TriangleMesh struct {
	MaterialFrame MaterialFrame
	Index         *geom.BoundsIndex
	Texture       AtlasKind
	Vertices      []ebiten.Vertex
	Indices       []uint32
	fillRule      ebiten.FillRule
	premultiplied bool
}

type GridMesh struct {
	GrowthIndex *AttachmentSupportIndex
	Growth      []Attachment
	Faces       TriangleMesh
	Outlines    []TriangleMesh
	// Grass contains ground patches and blades, drawn after AboveRocks.
	Grass TriangleMesh
}

type MeshLayers uint16

const (
	ForegroundLayer MeshLayers = 1 << iota
	BackgroundLayer
	BackgroundVinesLayer
	WebsLayer
	MushroomsLayer
	ForegroundVinesLayer
	VegetationLayer = BackgroundVinesLayer | WebsLayer | MushroomsLayer | ForegroundVinesLayer
	AllLayers       = ForegroundLayer | BackgroundLayer | VegetationLayer
)

type SectionMesh struct {
	// Recipe identities survive cumulative publication and prevent duplicate uploads.
	PlantRecipes [4]*uint8
	// ForegroundBaseOnly omits atlas growth until its later immutable recipe.
	ForegroundBaseOnly                                  bool
	PlantIndex, WebIndex                                *AttachmentSupportIndex
	BakedInstances                                      bool
	Webs                                                []Attachment
	VisualCuts                                          []terrain.RockCut
	AppliedCuts                                         uint64
	Plants                                              []Attachment
	Layers                                              MeshLayers
	ID                                                  int64
	TerrainOnly                                         bool
	Background, Foreground                              GridMesh
	Vines                                               []TriangleMesh
	ForegroundVines                                     []TriangleMesh
	Mushrooms, SpiderWebs                               TriangleMesh
	Geometry                                            *terrain.Geometry
	VinesBounds, ForegroundVinesBounds, MushroomsBounds image.Rectangle
	SpiderWebsBounds                                    image.Rectangle
}

func PrepareSection(data terrain.SectionData, view View, growth ...RockGrowthOptions) SectionMesh {
	mesh := SectionMesh{ID: data.ID, BakedInstances: view == ViewShaded}
	data = sectionBakeVegetation(data)
	parallel.Run(
		func() {
			mesh.Background = prepareGridWithNeighbors(data.Background, view, nil, data.BackgroundNeighbors)
		},
		func() {
			mesh.Foreground = PrepareGridWithTopology(data.Foreground, view, sectionBakeTopology(data), sectionRockGrowth(data, growth))
		},
		func() {
			if view == ViewShaded {
				mesh.Vines = prepareRockVines(data.Vines, terrain.ShadeRockFaces(data.Background, nil, data.BackgroundNeighbors))
			}
		},
		func() {
			if view == ViewShaded {
				mesh.ForegroundVines = prepareForegroundVines(data.ForegroundVines, sectionVineGrid(data), data.Orientation)
			}
		},
		func() {
			if view == ViewShaded {
				mesh.Mushrooms = prepareUnsampledMushrooms(data.Mushrooms, mushroomBakeTopology(data), false)
				mesh.Plants = prepareMushroomAttachments(data.Mushrooms, mushroomBakeTopology(data), data.Orientation)
			}
		},
	)
	if view == ViewShaded && data.ForegroundTopology != nil {
		mesh.SpiderWebs = prepareSpiderWebs(sectionBakeTopology(data).SpiderWebs, false)
		mesh.Webs = prepareWebAttachments(sectionBakeTopology(data).SpiderWebs, false)
	}
	applyAuthoredVisualCuts(&mesh, data)
	return mesh
}

// PrepareTerrain combines both materialized rock layers for synchronous callers.
// Streaming prepares and publishes each layer separately. Buffers remain immutable.
func PrepareTerrain(data terrain.SectionData, view View, growth ...RockGrowthOptions) SectionMesh {
	mesh := SectionMesh{ID: data.ID, TerrainOnly: true, Layers: ForegroundLayer | BackgroundLayer}
	parallel.Run(
		func() {
			mesh.Background = prepareGridBand(data.Background, view, nil, data.BackgroundNeighbors, true)
		},
		func() {
			mesh.Foreground = PrepareOwnedGridWithTopology(data.Foreground, view, sectionBakeTopology(data), sectionRockGrowth(data, growth))
		},
	)
	applyAuthoredVisualCuts(&mesh, data)
	return mesh
}

func PrepareVegetation(data terrain.SectionData, view View) SectionMesh {
	return PrepareVegetationParts(data, view, terrain.AllVegetationParts)
}

// PrepareVegetationParts prepares only ready products; absent products remain unknown.
func PrepareVegetationParts(data terrain.SectionData, view View, parts terrain.VegetationParts) SectionMesh {
	mesh := SectionMesh{ID: data.ID, Layers: MeshLayers(parts) << 2, TerrainOnly: parts != terrain.AllVegetationParts, BakedInstances: view == ViewShaded}
	for i := range mesh.PlantRecipes {
		if parts&(1<<i) != 0 {
			mesh.PlantRecipes[i] = new(uint8)
		}
	}
	data = sectionBakeVegetation(data)
	if view == ViewShaded {
		parallel.Run(
			func() {
				if parts&terrain.BackgroundVinesPart != 0 {
					mesh.Vines = prepareRockVines(data.Vines, terrain.ShadeRockFaces(data.Background, nil, data.BackgroundNeighbors))
				}
			},
			func() {
				if parts&terrain.ForegroundVinesPart != 0 {
					mesh.ForegroundVines = prepareForegroundVines(data.ForegroundVines, sectionVineGrid(data), data.Orientation)
				}
			},
			func() {
				if parts&terrain.MushroomsPart == 0 {
					return
				}
				mesh.Mushrooms = prepareUnsampledMushrooms(data.Mushrooms, mushroomBakeTopology(data), true)
				mesh.Plants = prepareMushroomAttachments(data.Mushrooms, mushroomBakeTopology(data), data.Orientation)
			},
			func() {
				if parts&terrain.WebsPart != 0 && data.ForegroundTopology != nil {
					mesh.SpiderWebs = PrepareOwnedSpiderWebs(sectionBakeTopology(data).SpiderWebs)
					mesh.Webs = prepareWebAttachments(sectionBakeTopology(data).SpiderWebs, true)
				}
			},
		)
	}
	applyAuthoredVisualCuts(&mesh, data)
	FinishSectionMesh(&mesh)
	return mesh
}

func (m TriangleMesh) Draw(dst, white *ebiten.Image) {
	m.DrawWithAntialiasing(dst, white, true)
}

func (m TriangleMesh) DrawWithAntialiasing(dst, white *ebiten.Image, aa bool) {
	if len(m.Indices) == 0 {
		return
	}
	op := &ebiten.DrawTrianglesOptions{AntiAlias: aa, FillRule: m.fillRule}
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

func PrepareGridWithTopology(grid terrain.RockGrid, view View, topology *terrain.RockTopology, growth ...RockGrowthOptions) GridMesh {
	return prepareGridWithNeighbors(grid, view, topology, nil, growth...)
}

func prepareGridWithNeighbors(grid terrain.RockGrid, view View, topology *terrain.RockTopology, neighbors [][]int, growth ...RockGrowthOptions) GridMesh {
	return prepareGridBand(grid, view, topology, neighbors, false, growth...)
}

func prepareGridBand(grid terrain.RockGrid, view View, topology *terrain.RockTopology, neighbors [][]int, owned bool, growth ...RockGrowthOptions) GridMesh {
	density := rockGrowthOptions(growth)
	if topology == nil && len(grid) > 0 && grid[0].Raised {
		topology = terrain.NewRockTopology(grid)
	}
	if topology != nil {
		topology = terrain.PrepareRockAppearance(topology)
		grid = topology.Grid
		neighbors = topology.Neighbors()
	}
	capacity := len(grid) * 18
	if owned {
		capacity /= 3
	}
	vertices := make([]ebiten.Vertex, 0, capacity)
	indices := make([]uint32, 0, capacity)
	var boundary []terrain.RockEdge
	var walls []Attachment
	raised := len(grid) > 0 && grid[0].Raised
	if raised {
		if topology != nil {
			boundary = topology.Boundary
		} else {
			boundary = terrain.ExposedRockEdges(grid)
		}
	}
	if topology != nil {
		grid = topology.Appearance
	} else {
		grid = terrain.ShadeRockFaces(grid, boundary, neighbors)
	}
	var trim []Attachment
	var splits map[int]rockFaceSplit
	if raised && (view == ViewShaded || view == ViewClay) {
		grid, boundary, splits = foldRockEdges(grid, boundary, density.Shape)
		grid, boundary, trim = visualBoundaryGrid(grid, boundary, density, owned, view)
	}
	if raised {
		if view == ViewShaded {
			// Growth is instanced from shared procedural atlas pages below.
		}
		if view == ViewShaded || view == ViewClay {
			walls = prepareWallAttachments(grid, boundary, view, owned)
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
		if clr.A == 0 || (owned && !inTerrainBand(poly...)) {
			continue
		}
		if split, ok := splits[cellIndex]; ok {
			vertices, indices = appendSplitRockCellMesh(vertices, indices, cell, clr, split)
		} else {
			vertices, indices = appendCellMesh(vertices, indices, poly, s, clr, cell.Normal)
		}
		if raised && view == ViewShaded {
			// Surface moss is clipped atlas geometry, generated below.
		}
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
		for _, seam := range rockCrevicesInBand(grid, neighbors, owned) {
			edges[geom.EdgeKey(seam.a, seam.b)] = cellEdge{a: seam.a, b: seam.b, alpha: seam.alpha}
		}
	}
	if view == ViewShaded || view == ViewClay {
		vertices, indices = appendRockBevels(vertices, indices, grid, boundary, view, owned)
	}
	mesh := GridMesh{Faces: TriangleMesh{MaterialFrame: ReferenceMaterialFrame(), Vertices: vertices, Indices: indices}}
	mesh.Growth = append(walls, trim...)
	if raised && view == ViewShaded {
		growth := prepareGrowthAttachments(grid, boundary, density, owned, trim...)
		growth = append(growth, trim...)
		var ordered []Attachment
		for _, a := range growth {
			if a.Underlay {
				ordered = append(ordered, a)
			}
		}
		ordered = append(ordered, walls...)
		for _, a := range growth {
			if !a.Underlay {
				ordered = append(ordered, a)
			}
		}
		mesh.Growth = ordered
	}
	stampGrowthIDs(mesh.Growth, grid)
	mesh.GrowthIndex = AttachmentIndex(mesh.Growth)
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
		if hiddenEdges[key] || edge.alpha == 0 || (owned && !inTerrainBand(edge.a, edge.b)) {
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
	if raised {
		mesh.Faces.Index = MeshIndex(mesh.Faces)
	}
	return mesh
}

// LayerMask accepts legacy complete meshes and terrain-only callers.
func (m SectionMesh) LayerMask() MeshLayers {
	if m.Layers != 0 {
		return m.Layers
	}
	if m.TerrainOnly {
		return ForegroundLayer | BackgroundLayer
	}
	return AllLayers
}

// MergeSectionMeshes combines immutable independently published layers.
func MergeSectionMeshes(previous, next SectionMesh) SectionMesh {
	mask := next.LayerMask()
	if mask&ForegroundLayer != 0 {
		previous.Foreground = next.Foreground
		previous.ForegroundBaseOnly = next.ForegroundBaseOnly
	}
	if mask&BackgroundLayer != 0 {
		previous.Background = next.Background
	}
	if mask&VegetationLayer != 0 {
		previous.BakedInstances = next.BakedInstances
	}
	if mask&BackgroundVinesLayer != 0 {
		previous.Vines, previous.VinesBounds = next.Vines, next.VinesBounds
	}
	if mask&ForegroundVinesLayer != 0 {
		previous.ForegroundVines, previous.ForegroundVinesBounds = next.ForegroundVines, next.ForegroundVinesBounds
	}
	if mask&MushroomsLayer != 0 {
		previous.Plants, previous.PlantIndex = next.Plants, next.PlantIndex
		previous.Mushrooms, previous.MushroomsBounds = next.Mushrooms, next.MushroomsBounds
	}
	if mask&WebsLayer != 0 {
		previous.Webs, previous.WebIndex = next.Webs, next.WebIndex
		previous.SpiderWebs, previous.SpiderWebsBounds = next.SpiderWebs, next.SpiderWebsBounds
	}
	for i := range previous.PlantRecipes {
		if mask&(BackgroundVinesLayer<<i) != 0 {
			previous.PlantRecipes[i] = next.PlantRecipes[i]
		}
	}
	previous.ID = next.ID
	previous.Layers = previous.LayerMask() | mask
	previous.TerrainOnly = previous.Layers&VegetationLayer != VegetationLayer
	previous.Geometry = terrain.MergeTerrainVegetation(previous.Geometry, next.Geometry, terrain.VegetationParts((mask&VegetationLayer)>>2))
	previous.AppliedCuts = min(previous.AppliedCuts, next.AppliedCuts)
	cuts := append([]terrain.RockCut(nil), previous.VisualCuts...)
	for _, cut := range next.VisualCuts {
		found := false
		for _, old := range cuts {
			if old.Min == cut.Min && old.Max == cut.Max {
				found = true
				break
			}
		}
		if !found {
			cuts = append(cuts, cut)
		}
	}
	previous.VisualCuts = cuts
	top := terrain.SectionTop(previous.ID)
	previous.Foreground.Growth = FilterAttachments(previous.Foreground.Growth, cuts, top)
	previous.Plants = FilterAttachments(previous.Plants, cuts, top)
	previous.Webs = FilterAttachments(previous.Webs, cuts, top)
	return previous
}

func PrepareForeground(data terrain.SectionData, view View, growth ...RockGrowthOptions) SectionMesh {
	mesh := SectionMesh{ID: data.ID, TerrainOnly: true, Layers: ForegroundLayer, Foreground: PrepareOwnedGridWithTopology(data.Foreground, view, sectionBakeTopology(data), sectionRockGrowth(data, growth))}
	applyAuthoredVisualCuts(&mesh, data)
	return mesh
}

// PrepareRockBase retains structural walls and edge detail but never fits
// atlas growth, allowing foreground publication before atlas uploads.
func PrepareRockBase(data terrain.SectionData, view View, options ...RockGrowthOptions) SectionMesh {
	growth := rockGrowthOptions(options)
	pending := view == ViewShaded && (growth.Moss > 0 || growth.Hanging > 0 || growth.Grass > 0)
	growth.Moss, growth.Hanging, growth.Grass = 0, 0, 0
	mesh := PrepareForeground(data, view, growth)
	mesh.ForegroundBaseOnly = pending
	return mesh
}

func PrepareBackground(data terrain.SectionData, view View) SectionMesh {
	return SectionMesh{ID: data.ID, TerrainOnly: true, Layers: BackgroundLayer,
		Background: prepareGridBand(data.Background, view, nil, data.BackgroundNeighbors, true)}
}

// A padded guide can generate the same mushroom in neighboring sections.
// Only the anchor's half-open band owns its render mesh; generation stays padded.
func PrepareOwnedMushrooms(groups []terrain.MushroomGroup, topology *terrain.RockTopology) TriangleMesh {
	owned := make([]terrain.MushroomGroup, 0, len(groups))
	for _, group := range groups {
		mushrooms := make([]terrain.Mushroom, 0, len(group.Mushrooms))
		for _, mushroom := range group.Mushrooms {
			if mushroom.Anchor.Y >= 0 && mushroom.Anchor.Y < terrain.SectionHeight {
				mushrooms = append(mushrooms, mushroom)
			}
		}
		if len(mushrooms) > 0 {
			owned = append(owned, terrain.MushroomGroup{Mushrooms: mushrooms})
		}
	}
	return PrepareMushrooms(owned, topology)
}

// DrawUnaliased is used when batching into a persistent supersampled surface.
func (m TriangleMesh) DrawUnaliased(dst, white *ebiten.Image) {
	if len(m.Indices) == 0 {
		return
	}
	op := &ebiten.DrawTrianglesOptions{FillRule: m.fillRule}
	if m.premultiplied {
		op.ColorScaleMode = ebiten.ColorScaleModePremultipliedAlpha
	}
	dst.DrawTriangles32(m.Vertices, m.Indices, white, op)
}

// SupersampledBatch copies only referenced vertices and preserves triangle
// order and material coordinates. Both index and vertex submissions stay bounded.
func SupersampledBatch(mesh TriangleMesh, start, end int) TriangleMesh {
	source := mesh.Vertices
	indices := mesh.Indices[start:end]
	mesh.Vertices = make([]ebiten.Vertex, 0, min(len(source), len(indices)))
	mesh.Indices = make([]uint32, len(indices))
	remap := make(map[uint32]uint32, min(len(source), len(indices)))
	for i, index := range indices {
		mapped, ok := remap[index]
		if !ok {
			mapped = uint32(len(mesh.Vertices))
			remap[index] = mapped
			mesh.Vertices = append(mesh.Vertices, source[index])
		}
		mesh.Indices[i] = mapped
	}
	transformVertices(mesh.Vertices, 2, image.Point{})
	if mesh.MaterialFrame.Pixels != 0 {
		mesh.MaterialFrame.Samples *= 2
	}
	return mesh
}

func sectionBakeTopology(data terrain.SectionData) *terrain.RockTopology {
	if data.ForegroundBake != nil {
		return data.ForegroundBake
	}
	return data.ForegroundTopology
}

func sectionVineGrid(data terrain.SectionData) terrain.RockGrid {
	if topology := sectionBakeTopology(data); topology != nil {
		return terrain.PrepareRockAppearance(topology).Appearance
	}
	return terrain.PrepareRockAppearance(terrain.NewRockTopology(data.Foreground)).Appearance
}

func mushroomBakeTopology(data terrain.SectionData) *terrain.RockTopology {
	if t := sectionBakeTopology(data); t != nil {
		return t
	}
	return mushroomSectionTopology(data)
}
func sectionBakeVegetation(data terrain.SectionData) terrain.SectionData {
	if data.VegetationBake != nil {
		p := data.VegetationBake
		data.Vines, data.ForegroundVines, data.Mushrooms = p.Vines, p.ForegroundVines, p.Mushrooms
	}
	return data
}
func applyAuthoredVisualCuts(mesh *SectionMesh, data terrain.SectionData) {
	for _, list := range [][]Attachment{mesh.Plants, mesh.Webs} {
		stampAttachmentIDs(list, terrain.SectionTop(data.ID))
	}
	mesh.PlantIndex, mesh.WebIndex = AttachmentIndex(mesh.Plants), AttachmentIndex(mesh.Webs)
	if data.ForegroundTopology == nil {
		return
	}
	mesh.VisualCuts = data.ForegroundTopology.Cuts
	top := terrain.SectionTop(data.ID)
	mesh.Foreground.Growth = FilterAttachments(mesh.Foreground.Growth, mesh.VisualCuts, top)
	mesh.Plants = FilterAttachments(mesh.Plants, mesh.VisualCuts, top)
	mesh.Webs = FilterAttachments(mesh.Webs, mesh.VisualCuts, top)
}

// Terrain images contain [0, SectionHeight] along the scrolling axis. Cull
// whole primitives only, retaining a small margin for float32 conversion and
// the reference-scale outline width. Ebitengine antialiasing supersamples the
// destination's own bounds; off-image primitives cannot contribute at any
// render width, including one pixel. Lighting and topology keep their padding.
const terrainBandMargin = .002

func inTerrainBand(points ...geom.V) bool {
	below, above := true, true
	for _, p := range points {
		below = below && p.Y < -terrainBandMargin
		above = above && p.Y > terrain.SectionHeight+terrainBandMargin
	}
	return len(points) > 0 && !below && !above
}

// PrepareOwnedGridWithTopology emits terrain for a section's image while
// retaining complete padded topology for shading, collision, and later edits.
func PrepareOwnedGridWithTopology(grid terrain.RockGrid, view View, topology *terrain.RockTopology, growth ...RockGrowthOptions) GridMesh {
	return prepareGridBand(grid, view, topology, nil, true, growth...)
}

// ForegroundSpan keeps the original attachment indices and support index while
// masking unrelated draws. Independent uploads and edit replay use the same IDs.
type ForegroundSpan uint8

const (
	StructuralSpan ForegroundSpan = iota
	StructuralTopSpan
	UnderGrowthSpan
	SurfaceGrowthSpan
	GrassSpan
)

func (span ForegroundSpan) Contains(a Attachment) bool {
	switch span {
	case StructuralSpan:
		return a.Kind == EdgeAttachment
	case StructuralTopSpan:
		return a.Kind == EdgeAttachment && !a.Underlay
	case GrassSpan:
		return a.Kind == GrassAttachment
	case UnderGrowthSpan:
		return a.Kind != EdgeAttachment && a.Kind != GrassAttachment && a.Underlay
	default:
		return a.Kind != EdgeAttachment && a.Kind != GrassAttachment && !a.Underlay
	}
}

func MaskForeground(grid GridMesh, span ForegroundSpan) GridMesh {
	grid.Growth = append([]Attachment(nil), grid.Growth...)
	for i := range grid.Growth {
		if !span.Contains(grid.Growth[i]) {
			grid.Growth[i].Dead = true
		}
	}
	return grid
}

func HasForegroundSpan(grid GridMesh, span ForegroundSpan) bool {
	for _, a := range grid.Growth {
		if !a.Dead && span.Contains(a) && (len(a.Mesh.Indices) > 0 || len(a.Extra.Indices) > 0) {
			return true
		}
	}
	return false
}

func StructuralSection(mesh SectionMesh) SectionMesh {
	mesh.Foreground = MaskForeground(mesh.Foreground, StructuralSpan)
	return mesh
}

// PrepareForegroundGrowth reuses every structural triangle and attachment from
// the early base. Only growth fitting needs the refined appearance boundary.
func PrepareForegroundGrowth(data terrain.SectionData, view View, base SectionMesh, options ...RockGrowthOptions) SectionMesh {
	mesh := base
	mesh.Layers, mesh.ForegroundBaseOnly = ForegroundLayer, false
	if view == ViewShaded {
		density := sectionRockGrowth(data, options)
		topology := sectionBakeTopology(data)
		if topology == nil {
			topology = terrain.NewRockTopology(data.Foreground)
		}
		topology = terrain.PrepareRockAppearance(topology)
		grid, boundary := topology.Appearance, topology.Boundary
		grid, boundary, _ = foldRockEdges(grid, boundary, density.Shape)
		grid, boundary, trim := visualBoundaryGrid(grid, boundary, density, true, view)
		growth := prepareGrowthAttachments(grid, boundary, density, true, trim...)
		stampGrowthIDs(growth, grid)
		var ordered []Attachment
		for _, a := range growth {
			if a.Underlay {
				ordered = append(ordered, a)
			}
		}
		for _, a := range base.Foreground.Growth {
			if a.Underlay {
				ordered = append(ordered, a)
			}
		}
		for _, a := range growth {
			if !a.Underlay {
				ordered = append(ordered, a)
			}
		}
		for _, a := range base.Foreground.Growth {
			if !a.Underlay {
				ordered = append(ordered, a)
			}
		}
		mesh.Foreground.Growth = ordered
		mesh.Foreground.GrowthIndex = AttachmentIndex(ordered)
	}
	applyAuthoredVisualCuts(&mesh, data)
	return mesh
}

func stampGrowthIDs(attachments []Attachment, grid terrain.RockGrid) {
	if len(grid) == 0 {
		return
	}
	for i := range attachments {
		a := &attachments[i]
		p := geom.LerpVector(a.Min, a.Max, .5)
		if len(a.Anchors) > 0 {
			p = a.Anchors[0]
		}
		a.ID = terrain.CollisionVertexKey(terrain.RockMaterialPoint(grid[0], p))
	}
}
