package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Faces visible from their center use a triangle fan. Deep concave guide
// cuts use ear clipping, with material rims only on the actual perimeter.
func appendCellMesh(vertices []ebiten.Vertex, indices []uint32, poly []geom.V, center geom.V, clr color.NRGBA, surface geom.V3) ([]ebiten.Vertex, []uint32) {
	if len(poly) < 3 {
		return vertices, indices
	}
	radius := 0.0
	for _, p := range poly {
		radius = math.Max(radius, p.Sub(center).Len())
	}
	radius = math.Max(radius, .001)
	tilt := geom.V{X: surface.X, Y: surface.Y}
	type facetTriangle struct {
		center, a, b geom.V
		boundary     bool
	}
	var facets []facetTriangle
	fan := true
	for i, a := range poly {
		if geom.Cross(poly[(i+1)%len(poly)].Sub(a), center.Sub(a)) < -1e-15 {
			fan = false
			break
		}
	}
	if fan {
		for i, a := range poly {
			facets = append(facets, facetTriangle{center, a, poly[(i+1)%len(poly)], true})
		}
	} else {
		for _, tri := range geom.Triangulate(poly) {
			mid := poly[tri[0]].Add(poly[tri[1]]).Add(poly[tri[2]]).Mul(1.0 / 3)
			for i, a := range tri {
				b := tri[(i+1)%3]
				facets = append(facets, facetTriangle{mid, poly[a], poly[b], (a+1)%len(poly) == b})
			}
		}
	}
	for _, triangle := range facets {
		a, b := triangle.a, triangle.b
		normal := b.Sub(a).Perp().Norm()
		if triangle.center.Sub(a).Dot(normal) < 0 {
			normal = normal.Mul(-1)
		}
		first := uint32(len(vertices))
		for _, p := range []geom.V{triangle.center, a, b} {
			edgeDistance := .002 // Internal triangulation edges have no rim.
			if triangle.boundary {
				edgeDistance = math.Max(0, p.Sub(a).Dot(normal))
			}
			vertices = append(vertices, ebiten.Vertex{
				DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
				SrcX: float32(p.X * RasterPixelsPerUnit), SrcY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
				ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255,
				ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
				Custom0: float32(edgeDistance * RasterPixelsPerUnit),
				Custom1: float32(p.Sub(center).Dot(tilt) / radius),
				Custom2: float32(surface.X),
				Custom3: float32(surface.Y),
			})
		}
		indices = append(indices, first, first+1, first+2)
	}
	return vertices, indices
}

func appendRockQuad(vertices []ebiten.Vertex, indices []uint32, points [4]geom.V, clr color.NRGBA) ([]ebiten.Vertex, []uint32) {
	first := uint32(len(vertices))
	for _, p := range points {
		vertices = append(vertices, ebiten.Vertex{
			DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: float32(p.X * RasterPixelsPerUnit), SrcY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
			ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255, ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
			Custom0: 2,
		})
	}
	return vertices, append(indices, first, first+1, first+2, first, first+2, first+3)
}

// A shallow oblique view exposes the drop below a silhouette. Draw walls
// before the top faces; their inward portions are naturally hidden by rock.
func rockWallPoints(c terrain.RockCell, edge terrain.RockEdge) ([4]geom.V, bool) {
	outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
	view := terrain.InternalPoint(terrain.RockOrientation(c), geom.V{X: .045, Y: .08})
	if outward.Dot(view) <= 0 {
		return [4]geom.V{}, false
	}
	za, zb := math.Max(0, terrain.RockDepthAt(c, edge.A)+.008), math.Max(0, terrain.RockDepthAt(c, edge.B)+.008)
	return [4]geom.V{edge.A, edge.B, edge.B.Add(view.Mul(zb)), edge.A.Add(view.Mul(za))}, true
}

func appendRockWalls(vertices []ebiten.Vertex, indices []uint32, grid terrain.RockGrid, edges []terrain.RockEdge, viewMode View, owned ...bool) ([]ebiten.Vertex, []uint32) {
	if len(grid) == 0 || !grid[0].Raised {
		return vertices, indices
	}
	for _, edge := range edges {
		c := grid[edge.Cell]
		points, visible := rockWallPoints(c, edge)
		if !visible {
			continue
		}
		if len(owned) > 0 && owned[0] && !inTerrainBand(points[:]...) {
			continue
		}
		outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
		normal := (geom.V3{X: outward.X, Y: outward.Y, Z: .1}).Norm()
		clr := terrain.RockSurfaceColor(normal, c.Shadow*.65, c.Ambient*.55, terrain.RockOrientation(c))
		clr = rockViewColor(terrain.RockCell{Color: clr, Raised: true}, viewMode)
		vertices, indices = appendRockQuad(vertices, indices, points, clr)
	}
	return vertices, indices
}

func rockViewColor(c terrain.RockCell, view View) color.NRGBA {
	switch view {
	case ViewHeight:
		v := uint8(math.Round(255 * geom.Clamp((c.Z+.012)/.112, 0, 1)))
		return color.NRGBA{v, v, v, 255}
	case ViewNormals:
		n := terrain.WorldNormal(terrain.RockOrientation(c), c.Normal)
		return color.NRGBA{uint8(127.5 * (n.X + 1)), uint8(127.5 * (n.Y + 1)), uint8(127.5 * (n.Z + 1)), 255}
	case ViewShadows:
		v := uint8(math.Round(255 * c.Shadow))
		return color.NRGBA{v, v, v, 255}
	case ViewClay:
		v := uint8(math.Round(.30*float64(c.Color.R) + .59*float64(c.Color.G) + .11*float64(c.Color.B)))
		return color.NRGBA{v, v, v, c.Color.A}
	}
	return rockMaterialColor(c.Color, c.Raised)
}

// Grade only the rendered material. Terrain colors also steer vine growth,
// so changing their palette during generation would change seeded geometry.
func rockMaterialColor(c color.NRGBA, raised bool) color.NRGBA {
	if !raised {
		v := (.30*float64(c.R) + .59*float64(c.G) + .11*float64(c.B)) / 31
		return color.NRGBA{
			R: uint8(math.Round(math.Min(255, 19*v))),
			G: uint8(math.Round(math.Min(255, 26*v))),
			B: uint8(math.Round(math.Min(255, 29*v))), A: c.A,
		}
	}
	stops := [...]struct {
		red, r, g, b float64
	}{
		{0, 0, 0, 0}, {8, 8, 8, 7}, {18, 23, 22, 20},
		{38, 49, 46, 40}, {120, 151, 139, 116},
		{165, 229, 210, 172}, {190, 255, 237, 197}, {255, 255, 250, 230},
	}
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if float64(c.R) <= b.red {
			t := (float64(c.R) - a.red) / (b.red - a.red)
			return color.NRGBA{
				R: uint8(math.Round(geom.Lerp(a.r, b.r, t))),
				G: uint8(math.Round(geom.Lerp(a.g, b.g, t))),
				B: uint8(math.Round(geom.Lerp(a.b, b.b, t))), A: c.A,
			}
		}
	}
	return c
}

// A restrained bevel belongs only to an exposed silhouette. Internal cell
// edges have neither a raised rim nor a lighting seam from triangulation.
func appendRockBevels(vertices []ebiten.Vertex, indices []uint32, grid terrain.RockGrid, edges []terrain.RockEdge, view View, owned ...bool) ([]ebiten.Vertex, []uint32) {
	for _, edge := range edges {
		c := grid[edge.Cell]
		inward := edge.B.Sub(edge.A).Perp().Norm()
		world := terrain.RockMaterialPoint(c, edge.A.Add(edge.B).Mul(.5))
		key := terrain.CollisionVertexKey(world)
		chip := terrain.SiteRandom(0x626576656c, key[0], key[1], 0)
		width := math.Min(.00025+.00055*chip, edge.B.Sub(edge.A).Len()*.06)
		lightDirection := terrain.RockLightDirection(terrain.RockOrientation(c))
		edgeLight := math.Max(0, inward.Mul(-1).Dot((geom.V{X: lightDirection.X, Y: lightDirection.Y}).Norm()))
		facing := geom.Smoothstep(.02, .85, edgeLight)
		if view == ViewShaded {
			width = math.Min((.00065+.00115*chip)*(.6+.4*facing), edge.B.Sub(edge.A).Len()*.1)
		}
		a, b := edge.A.Add(inward.Mul(width)), edge.B.Add(inward.Mul(width))
		if len(owned) > 0 && owned[0] && !inTerrainBand(edge.A, edge.B, b, a) {
			continue
		}
		if !geom.InsidePolygon(geom.LerpVector(a, b, .5), c.Polygon) {
			continue
		}
		normal := (geom.V3{X: c.Normal.X - inward.X*.5, Y: c.Normal.Y - inward.Y*.5, Z: c.Normal.Z}).Norm()
		clr := terrain.RockSurfaceColor(normal, c.Shadow, c.Ambient, terrain.RockOrientation(c))
		clr = rockViewColor(terrain.RockCell{Color: clr, Raised: true}, view)
		if view == ViewShaded {
			// A broader pale rim catches the upper-left light on exposed tops
			// and side facets. Occluded and light-opposing edges stay subdued.
			light := terrain.SurfaceLight(normal, terrain.RockOrientation(c))
			warmth := .8 * facing * light * c.Shadow * (.5 + .5*chip)
			clr.R = uint8(math.Round(geom.Lerp(float64(clr.R), 255, warmth)))
			clr.G = uint8(math.Round(geom.Lerp(float64(clr.G), 234, warmth)))
			clr.B = uint8(math.Round(geom.Lerp(float64(clr.B), 193, warmth)))
		}
		vertices, indices = appendRockQuad(vertices, indices, [4]geom.V{edge.A, edge.B, b, a}, clr)
	}
	return vertices, indices
}

type rockCrevice struct {
	a, b  geom.V
	alpha uint8
}

// Concave changes of slope and exposed depth steps get narrow contact cracks.
// Coplanar and convex joins read through their face tones alone. Intersecting
// collinear edge intervals handles the partial joins of clipped/merged cells.
func rockCrevices(grid terrain.RockGrid, adjacency ...[][]int) []rockCrevice {
	var neighbors [][]int
	if len(adjacency) > 0 {
		neighbors = adjacency[0]
	}
	return rockCrevicesInBand(grid, neighbors, false)
}

func rockCrevicesInBand(grid terrain.RockGrid, neighbors [][]int, owned bool) []rockCrevice {
	if neighbors == nil {
		neighbors = terrain.RockNeighbors(grid)
	}
	var seams []rockCrevice
	for i, neighbors := range neighbors {
		for _, j := range neighbors {
			if j <= i {
				continue
			}
			c, other := grid[i], grid[j]
			if c.Color.A == 0 || other.Color.A == 0 {
				continue
			}
			for k, a := range c.Polygon {
				b := c.Polygon[(k+1)%len(c.Polygon)]
				for l, p := range other.Polygon {
					q := other.Polygon[(l+1)%len(other.Polygon)]
					start, end, ok := sharedRockSegment(a, b, p, q)
					if !ok || (owned && !inTerrainBand(start, end)) {
						continue
					}
					strength := rockCreviceStrength(c, other, start.Add(end).Mul(.5))
					if strength < .08 || end.Sub(start).Len() < .002 {
						continue
					}
					world := terrain.RockMaterialPoint(c, start.Add(end).Mul(.5))
					key := terrain.CollisionVertexKey(world)
					variation := terrain.SiteRandom(0x63726576696365, key[0], key[1], 0)
					// End before junctions so tiny cut tips cannot acquire whiskers.
					d := end.Sub(start)
					inset := math.Min(.0012/d.Len(), .15)
					alpha := uint8(math.Round(100 * strength * (.65 + .35*variation)))
					seams = append(seams, rockCrevice{start.Add(d.Mul(inset)), end.Sub(d.Mul(inset)), alpha})
				}
			}
		}
	}
	return seams
}

func sharedRockSegment(a, b, p, q geom.V) (geom.V, geom.V, bool) {
	d := b.Sub(a)
	length := d.Len()
	if length < 1e-8 || math.Abs(geom.Cross(d, p.Sub(a)))/length > 1e-8 || math.Abs(geom.Cross(d, q.Sub(a)))/length > 1e-8 {
		return geom.V{}, geom.V{}, false
	}
	u, v := p.Sub(a).Dot(d)/d.Len2(), q.Sub(a).Dot(d)/d.Len2()
	lo, hi := math.Max(0, math.Min(u, v)), math.Min(1, math.Max(u, v))
	if (hi-lo)*length < 1e-8 {
		return geom.V{}, geom.V{}, false
	}
	return a.Add(d.Mul(lo)), a.Add(d.Mul(hi)), true
}

func rockCreviceStrength(a, b terrain.RockCell, p geom.V) float64 {
	direction := b.Center.Sub(a.Center).Norm()
	slopeA := (geom.V{X: -a.Normal.X, Y: -a.Normal.Y}).Mul(1 / math.Max(a.Normal.Z, .05))
	slopeB := (geom.V{X: -b.Normal.X, Y: -b.Normal.Y}).Mul(1 / math.Max(b.Normal.Z, .05))
	if !a.Raised && !b.Raised {
		concave := geom.Smoothstep(.025, .25, slopeB.Sub(slopeA).Dot(direction))
		step := geom.Smoothstep(.0005, .003, math.Abs(terrain.RockDepthAt(a, p)-terrain.RockDepthAt(b, p)))
		visible := geom.Smoothstep(2, 16, math.Min(float64(rockViewColor(a, ViewShaded).R), float64(rockViewColor(b, ViewShaded).R)))
		return math.Max(.65*concave, .55*step) * visible
	}
	concave := geom.Smoothstep(.18, .9, slopeB.Sub(slopeA).Dot(direction))
	step := geom.Smoothstep(.003, .016, math.Abs(terrain.RockDepthAt(a, p)-terrain.RockDepthAt(b, p)))
	visible := geom.Smoothstep(12, 65, math.Max(float64(rockViewColor(a, ViewShaded).R), float64(rockViewColor(b, ViewShaded).R)))
	return math.Max(.8*concave, .65*step) * visible
}

// Decorate only the bake recipe. Structural polygons, collision and material
// normals remain immutable; the resulting coverage includes genuine notches.
func visualBoundaryGrid(grid terrain.RockGrid, boundary []terrain.RockEdge, growth RockGrowthOptions, owned bool, view View) (terrain.RockGrid, []terrain.RockEdge, []Attachment) {
	shape := growth.Shape
	if shape.EdgeRoughness == 0 && shape.SpikeDensity == 0 {
		return grid, boundary, nil
	}
	detail := make(map[[4]int64][]geom.V, len(boundary))
	var teeth []Attachment
	for _, e := range boundary {
		length := e.B.Sub(e.A).Len()
		if length < .006 {
			continue
		}
		c := grid[e.Cell]
		mid := geom.LerpVector(e.A, e.B, .5)
		random := rockGrowthRandom(c, mid, 0x6368697073)
		amplitude := math.Min(1, length/.012)
		for _, g := range growth.Guides {
			_, distance := g.NearestPoint(mid)
			if distance < .004 {
				amplitude *= .35
				break
			}
		}
		n := e.B.Sub(e.A).Norm().Perp().Mul(-1)
		count := max(1, int(math.Round(length/.012)))
		var points []geom.V
		type tooth struct {
			p    geom.V
			slot int
		}
		var spikes []tooth
		for j := 0; j < count; j++ {
			channel := int64(j) * 4
			u := (float64(j) + .35 + .30*random(channel)) / float64(count)
			h := shape.EdgeRoughness * (-.003 + .004*random(channel+1))
			if random(channel+2) < shape.SpikeDensity {
				h += .002 + .003*random(channel+3)
			}
			h *= amplitude
			p := geom.LerpVector(e.A, e.B, u).Add(n.Mul(h))
			if p.X > 0 && p.X < 1 && p.Y > terrain.GenerationMinY && p.Y < terrain.GenerationMinY+terrain.GenerationHeight {
				if h > 0 {
					spikes = append(spikes, tooth{p, len(points)})
					points = append(points, geom.LerpVector(e.A, e.B, u))
				} else {
					points = append(points, p)
				}
			}
		}
		for _, spike := range spikes {
			a, b := e.A, e.B
			if spike.slot > 0 {
				a = points[spike.slot-1]
			}
			if spike.slot+1 < len(points) {
				b = points[spike.slot+1]
			}
			teeth = append(teeth, visualTooth(c, a, b, spike.p, n, owned, view)...)
		}
		if len(points) > 0 {
			detail[geom.EdgeKey(e.A, e.B)] = points
		}
	}
	out := append(terrain.RockGrid(nil), grid...)
	accepted := make(map[[4]int64]bool, len(detail))
	for i, c := range grid {
		count := len(c.Polygon)
		for j, a := range c.Polygon {
			count += len(detail[geom.EdgeKey(a, c.Polygon[(j+1)%len(c.Polygon)])])
		}
		if count == len(c.Polygon) {
			continue
		}
		poly := make([]geom.V, 0, count)
		for j, a := range c.Polygon {
			poly = append(poly, a)
			poly = append(poly, detail[geom.EdgeKey(a, c.Polygon[(j+1)%len(c.Polygon)])]...)
		}
		if !simpleVisualOutline(poly) || geom.PolygonArea(poly) <= 1e-6 || len(geom.Triangulate(poly)) != len(poly)-2 {
			continue
		}
		out[i].Polygon = poly
		for j, a := range c.Polygon {
			accepted[geom.EdgeKey(a, c.Polygon[(j+1)%len(c.Polygon)])] = true
		}
	}
	var edges []terrain.RockEdge
	for _, e := range boundary {
		key := geom.EdgeKey(e.A, e.B)
		a := e.A
		if accepted[key] {
			for _, p := range detail[key] {
				edges = append(edges, terrain.RockEdge{A: a, B: p, Cell: e.Cell})
				a = p
			}
		}
		edges = append(edges, terrain.RockEdge{A: a, B: e.B, Cell: e.Cell})
	}
	return out, edges, teeth
}

func simpleVisualOutline(poly []geom.V) bool {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		for j := i + 2; j < len(poly); j++ {
			if (j+1)%len(poly) == i {
				continue
			}
			if geom.SegmentDistanceSquared(a, b, poly[j], poly[(j+1)%len(poly)]) < 1e-20 {
				return false
			}
		}
	}
	return true
}

// A tooth and its small projected wall share support, footprint and bounds.
// Cutting either removes the pair, including pixels outside the smooth shape.
func visualTooth(c terrain.RockCell, a, b, tip, outward geom.V, owned bool, view View) []Attachment {
	poly := []geom.V{a, b, tip}
	if geom.PolygonArea(poly) < 0 {
		reversePoints(poly)
	}
	if owned && !inTerrainBand(poly...) {
		return nil
	}
	top := TriangleMesh{}
	top.Vertices, top.Indices = appendCellMesh(nil, nil, poly, geom.PolygonCenter(poly), rockViewColor(c, view), c.Normal)
	pa := geom.LerpVector(a, b, .05).Sub(outward.Mul(.001))
	pb := geom.LerpVector(a, b, .95).Sub(outward.Mul(.001))
	if !geom.InsidePolygon(pa, c.Polygon) || !geom.InsidePolygon(pb, c.Polygon) {
		return nil
	}
	support := []geom.V{a, pa, pb, b, tip}
	if geom.PolygonArea(support) < 0 {
		reversePoints(support)
	}
	footprint := [][]geom.V{support}
	wall := TriangleMesh{}
	for _, edge := range []terrain.RockEdge{{A: a, B: tip}, {A: tip, B: b}} {
		if p, ok := rockWallPoints(c, edge); ok {
			wall.Vertices, wall.Indices = appendRockQuad(wall.Vertices, wall.Indices, p, growthColor(rockViewColor(c, view), .55))
			face := append([]geom.V(nil), p[:]...)
			if geom.PolygonArea(face) < 0 {
				reversePoints(face)
			}
			footprint = append(footprint, face)
		}
	}
	lo, hi := geom.PolygonBounds(support)
	for _, face := range footprint {
		a, b := geom.PolygonBounds(face)
		lo = geom.V{X: min(lo.X, a.X), Y: min(lo.Y, a.Y)}
		hi = geom.V{X: max(hi.X, b.X), Y: max(hi.Y, b.Y)}
	}
	for _, mesh := range []TriangleMesh{top, wall} {
		for j := range mesh.Vertices {
			mesh.Vertices[j].SrcX, mesh.Vertices[j].SrcY = .5, .5
		}
	}
	attachment := Attachment{Kind: EdgeAttachment, Mesh: top, Min: lo, Max: hi, Anchors: []geom.V{pa, pb}, Footprint: footprint}
	out := []Attachment{attachment}
	if len(wall.Indices) > 0 {
		attachment.Mesh = wall
		attachment.Underlay = true
		out = append(out, attachment)
	}
	return out
}
