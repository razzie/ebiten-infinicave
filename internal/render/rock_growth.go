package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

const (
	DefaultMossDensity          = .72
	DefaultHangingGrowthDensity = .28
)

// RockGrowthOptions controls material decorations independently of vegetation.
// Explicit zero densities disable growth; omitted mesh options use the defaults.
type RockGrowthOptions struct {
	Moss, Hanging, Grass float64
	Shape                terrain.ShapeOptions
	Guides               []terrain.Guide // Grass roots follow the section's actual guide edges.
}

func rockGrowthOptions(options []RockGrowthOptions) RockGrowthOptions {
	if len(options) > 0 {
		return options[0]
	}
	return RockGrowthOptions{Moss: DefaultMossDensity, Hanging: DefaultHangingGrowthDensity, Grass: DefaultGrassDensity}
}

func sectionRockGrowth(data terrain.SectionData, options []RockGrowthOptions) RockGrowthOptions {
	growth := rockGrowthOptions(options)
	growth.Guides = data.Guides
	growth.Shape = data.Shape
	return growth
}

// Material coordinates keep growth stable in overlapping section windows.
// Separate channels keep moss and hanging growth independent of mesh order.
func rockGrowthRandom(cell terrain.RockCell, point geom.V, seed int64) func(int64) float64 {
	key := terrain.CollisionVertexKey(terrain.RockMaterialPoint(cell, point))
	return func(channel int64) float64 { return terrain.SiteRandom(seed, key[0], key[1], channel) }
}

// Moss follows upward-facing relief, including interior facets below a crest.
// Clip against individual face triangles so deep concavities and runtime holes
// cannot acquire a floating patch across their empty space.
func appendRockMoss(vertices []ebiten.Vertex, indices []uint32, cell terrain.RockCell, density float64) ([]ebiten.Vertex, []uint32) {
	if density <= 0 || !cell.Raised || cell.Color.A == 0 || len(cell.Polygon) < 3 {
		return vertices, indices
	}
	mode := terrain.RockOrientation(cell)
	upward := -terrain.WorldNormal(mode, cell.Normal).Y
	random := rockGrowthRandom(cell, cell.Center, 0x6d6f7373)
	if upward < .10 || random(0) > density*geom.Smoothstep(.08, .35, upward) {
		return vertices, indices
	}
	up := terrain.InternalPoint(mode, geom.V{Y: -1})
	right := terrain.InternalPoint(mode, geom.V{X: 1})
	size := math.Min(.014, math.Sqrt(math.Abs(geom.PolygonArea(cell.Polygon)))*.42)
	if size < .001 {
		return vertices, indices
	}
	base := cell.Center.Add(up.Mul(size * .55))
	triangles := geom.Triangulate(cell.Polygon)
	light := .48 + .52*geom.Clamp(float64(cell.Color.R)/165, 0, 1)
	for patch := int64(0); patch < 2+int64(random(1)*3); patch++ {
		channel := 10 + patch*20
		center := base.Add(right.Mul((random(channel) - .5) * size * 2)).Add(up.Mul((random(channel+1) - .5) * size))
		radius := size * geom.Lerp(.38, .85, random(channel+2))
		palette := color.NRGBA{R: 111, G: 128, B: 55, A: 255}
		if random(channel+3) < .43 {
			palette = color.NRGBA{R: 183, G: 164, B: 70, A: 255}
		}
		palette = growthColor(palette, light*geom.Lerp(.87, 1.08, random(channel+4)))
		outline := make([]geom.V, 9)
		for i := range outline {
			angle := float64(i) * 2 * math.Pi / float64(len(outline))
			r := radius * geom.Lerp(.65, 1.15, random(channel+5+int64(i)))
			outline[i] = center.Add(right.Mul(math.Cos(angle) * r)).Sub(up.Mul(math.Sin(angle) * r * .72))
		}
		for _, tri := range triangles {
			for i, a := range outline {
				piece := []geom.V{center, a, outline[(i+1)%len(outline)]}
				for j, index := range tri {
					p, q := cell.Polygon[index], cell.Polygon[tri[(j+1)%3]]
					n := q.Sub(p).Perp().Mul(-1).Norm()
					piece = geom.ClipHalfPlane(piece, n, p.Dot(n))
				}
				if len(piece) >= 3 && math.Abs(geom.PolygonArea(piece)) > 1e-15 {
					vertices, indices = appendCellMesh(vertices, indices, piece, geom.PolygonCenter(piece), palette, cell.Normal)
				}
			}
		}
	}
	return vertices, indices
}

type hangingGrowth struct {
	stem   []geom.V
	body   []geom.V
	lobes  [][]geom.V
	center geom.V
	radius float64
	lichen bool
}

func hangingGrowthForEdge(cell terrain.RockCell, edge terrain.RockEdge, density float64) (hangingGrowth, bool) {
	mode := terrain.RockOrientation(cell)
	outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
	if density <= 0 || !cell.Raised || cell.Color.A == 0 || terrain.WorldPoint(mode, outward).Y < .45 || edge.B.Sub(edge.A).Len() < .008 {
		return hangingGrowth{}, false
	}
	random := rockGrowthRandom(cell, edge.A.Add(edge.B).Mul(.5), 0x6c696368656e)
	if random(0) > density {
		return hangingGrowth{}, false
	}
	t := geom.Lerp(.22, .78, random(1))
	anchor := geom.LerpVector(edge.A, edge.B, t)
	// Attach at the visible bottom of the shallow rock wall, rather than
	// hiding a short stalk behind its projected thickness.
	if wall, visible := rockWallPoints(cell, edge); visible {
		anchor = geom.LerpVector(wall[3], wall[2], t)
	}
	down := terrain.InternalPoint(mode, geom.V{Y: 1})
	right := terrain.InternalPoint(mode, geom.V{X: 1})
	length := geom.Lerp(.012, .033, random(2))
	sway := geom.Lerp(-.004, .004, random(3))
	growth := hangingGrowth{lichen: random(4) < .42, radius: geom.Lerp(.0023, .0048, random(5))}
	root := anchor.Sub(outward.Mul(.0006))
	for i := 0; i < 6; i++ {
		s := float64(i) / 5
		growth.stem = append(growth.stem, root.Add(down.Mul(length*s)).Add(right.Mul(sway*s*s)))
	}
	growth.center = growth.stem[len(growth.stem)-1].Add(down.Mul(growth.radius * .55))
	for i := 0; i < 10; i++ {
		angle := float64(i)*2*math.Pi/10 - math.Pi/2
		r := growth.radius * geom.Lerp(.87, 1.12, random(10+int64(i)))
		// A narrow neck and a fuller droplet underneath the attachment.
		width := .8 + .2*math.Sin(angle)
		growth.body = append(growth.body, growth.center.Add(right.Mul(math.Cos(angle)*r*width)).Add(down.Mul(math.Sin(angle)*r*1.2)))
	}
	if growth.lichen {
		// Small alternating lobes turn the stem into a hanging lichen tuft.
		for i := 2; i < len(growth.stem); i++ {
			p := growth.stem[i]
			side := right.Mul(float64(2*(i%2)-1) * growth.radius * .8)
			leaf := []geom.V{p.Sub(down.Mul(.002)), p.Add(side).Sub(down.Mul(.001)), p.Add(side.Mul(.65)).Add(down.Mul(.002)), p.Add(down.Mul(.003))}
			if geom.PolygonArea(leaf) < 0 {
				leaf[1], leaf[3] = leaf[3], leaf[1]
			}
			growth.lobes = append(growth.lobes, leaf)
		}
	}
	return growth, true
}

func (growth hangingGrowth) bounds() (lo, hi geom.V) {
	lo, hi = geom.PolygonBounds(growth.body)
	for _, poly := range append([][]geom.V{growth.body, growth.stem}, growth.lobes...) {
		for _, p := range poly {
			lo.X, lo.Y = math.Min(lo.X, p.X-.00055), math.Min(lo.Y, p.Y-.00055)
			hi.X, hi.Y = math.Max(hi.X, p.X+.00055), math.Max(hi.Y, p.Y+.00055)
		}
	}
	return
}

// Reserve actual air for the stalk and bulb. Bounding boxes reject distant
// faces; segment checks also catch narrow rocks between the curve samples.
func hangingGrowthClear(growth hangingGrowth, rock mushroomRock) bool {
	polygons := append([][]geom.V{growth.body}, growth.lobes...)
	lo, hi := growth.bounds()
	for _, face := range rock {
		if hi.X < face.lo.X || lo.X > face.hi.X || hi.Y < face.lo.Y || lo.Y > face.hi.Y {
			continue
		}
		for i := 2; i < len(growth.stem); i++ {
			a, b := growth.stem[i-1], growth.stem[i]
			if geom.InsidePolygon(a, face.poly) || geom.InsidePolygon(b, face.poly) {
				return false
			}
			for j, p := range face.poly {
				if geom.SegmentDistanceSquared(a, b, p, face.poly[(j+1)%len(face.poly)]) < .00000025 {
					return false
				}
			}
		}
		for _, poly := range polygons {
			if geom.InsidePolygon(face.poly[0], poly) {
				return false
			}
			for i, a := range poly {
				if geom.InsidePolygon(a, face.poly) {
					return false
				}
				for j, p := range face.poly {
					if geom.SegmentDistanceSquared(a, poly[(i+1)%len(poly)], p, face.poly[(j+1)%len(face.poly)]) < 1e-18 {
						return false
					}
				}
			}
		}
	}
	return true
}

// Hanging details share the rock batch and its owned band. Draw them first:
// foreground faces and walls cover the buried attachment naturally, and
// carving/reuploads always use the current silhouette without a second cache.
func appendHangingRockGrowth(vertices []ebiten.Vertex, indices []uint32, grid terrain.RockGrid, boundary []terrain.RockEdge, owned bool, density float64) ([]ebiten.Vertex, []uint32) {
	if density <= 0 {
		return vertices, indices
	}
	rock := mushroomRockSilhouette(&terrain.RockTopology{Grid: grid, Boundary: boundary})
	for _, edge := range boundary {
		cell := grid[edge.Cell]
		growth, ok := hangingGrowthForEdge(cell, edge, density)
		if !ok {
			continue
		}
		lo, hi := growth.bounds()
		if (owned && !inTerrainBand(lo, hi)) || !hangingGrowthClear(growth, rock) {
			continue
		}
		stemColor := color.NRGBA{R: 97, G: 103, B: 57, A: 255}
		for i := 1; i < len(growth.stem); i++ {
			a, b := growth.stem[i-1], growth.stem[i]
			n := b.Sub(a).Norm().Perp().Mul(.00055)
			vertices, indices = appendRockQuad(vertices, indices, [4]geom.V{a.Add(n), a.Sub(n), b.Sub(n), b.Add(n)}, stemColor)
		}
		palette := color.NRGBA{R: 220, G: 215, B: 141, A: 255}
		if growth.lichen {
			palette = color.NRGBA{R: 155, G: 169, B: 93, A: 255}
			for _, leaf := range growth.lobes {
				vertices, indices = appendCellMesh(vertices, indices, leaf, geom.PolygonCenter(leaf), growthColor(palette, .72), geom.V3{Z: 1})
			}
		}
		first := len(vertices)
		vertices, indices = appendCellMesh(vertices, indices, growth.body, growth.center, palette, geom.V3{Z: 1})
		// A pale upper-left core and muted edge keep the bulbs dimensional.
		for i := first; i < len(vertices); i++ {
			v := &vertices[i]
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			d := terrain.WorldPoint(terrain.RockOrientation(cell), p.Sub(growth.center)).Mul(1 / growth.radius)
			light := .78 + .22*geom.Clamp(1-math.Hypot(d.X+.25, d.Y+.3)*.6, 0, 1)
			clr := growthColor(palette, light)
			v.ColorR, v.ColorG, v.ColorB = float32(clr.R)/255, float32(clr.G)/255, float32(clr.B)/255
		}
	}
	return vertices, indices
}

func growthColor(c color.NRGBA, light float64) color.NRGBA {
	return color.NRGBA{R: uint8(geom.Clamp(float64(c.R)*light, 0, 255)), G: uint8(geom.Clamp(float64(c.G)*light, 0, 255)), B: uint8(geom.Clamp(float64(c.B)*light, 0, 255)), A: c.A}
}

type growthClearance struct {
	grid  terrain.RockGrid
	index *geom.BoundsIndex
}

// Reuse one small spatial index for all hanging samples in a section.
func newGrowthClearance(grid terrain.RockGrid) growthClearance {
	c := growthClearance{grid: grid}
	bounds := make([]geom.Bounds, len(grid))
	for i, cell := range grid {
		lo, hi := geom.PolygonBounds(cell.Polygon)
		bounds[i] = geom.Bounds{Min: lo, Max: hi}
	}
	c.index = geom.NewBoundsIndex(bounds)
	return c
}

func (c growthClearance) fits(a Attachment) bool {
	recipe := AtlasRecipes(a.Mesh.Texture)[a.Cell]
	for i := 0; i < len(recipe.Indices); i += 3 {
		var tri [3]geom.V
		nearRoot := true
		for j, id := range recipe.Indices[i : i+3] {
			v := recipe.Vertices[id]
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			nearRoot = nearRoot && p.Len() < .002
			tri[j] = a.Anchor.Add(terrain.InternalPoint(a.Orientation, geom.Rotate(p, a.Rotation).Mul(a.Scale)))
		}
		if nearRoot {
			continue
		}
		if geom.PolygonArea(tri[:]) < 0 {
			tri[0], tri[2] = tri[2], tri[0]
		}
		lo, hi := geom.PolygonBounds(tri[:])
		clear := true
		c.index.Query(geom.Bounds{Min: lo, Max: hi}, func(j int) bool {
			if c.grid[j].Color.A == 0 {
				return true
			}
			clear = !growthTriangleOverlap(tri[:], c.grid[j].Polygon)
			return clear
		})
		if !clear {
			return false
		}
	}
	return true
}

// An indexed narrow phase handles concave faces directly without retaining a
// triangulation of every unrelated rock cell.
func growthTriangleOverlap(tri, face []geom.V) bool {
	for _, p := range tri {
		if geom.InsidePolygon(p, face) {
			return true
		}
	}
	for _, p := range face {
		if geom.InsidePolygon(p, tri) {
			return true
		}
	}
	for i, a := range tri {
		for j, b := range face {
			if geom.SegmentDistanceSquared(a, tri[(i+1)%3], b, face[(j+1)%len(face)]) < 1e-20 {
				return true
			}
		}
	}
	return false
}
