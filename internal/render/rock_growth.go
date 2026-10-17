package render

import (
	"image/color"
	"math"
	"sort"

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

type mossColony struct {
	anchor          geom.V
	rotation, scale float64
	sample          int
}

// A jittered lattice in material/world coordinates defines one continuous colony
// mask across facets and padded section windows. Only nearby ledges attract it.
func rockMossColonies(grid terrain.RockGrid, boundary []terrain.RockEdge, density float64) []mossColony {
	if density <= 0 || len(grid) == 0 {
		return nil
	}
	const spacing, reach = .055, .037
	type ledge struct {
		a, b       geom.V
		tipA, tipB bool
		rim        bool
		light      float64
	}
	var ledges []ledge
	// Significant changes of silhouette direction identify real bends/tips;
	// collinear cell subdivisions do not create new growth hot spots.
	corners := make(map[[2]int64][]geom.V)
	for _, edge := range boundary {
		c := grid[edge.Cell]
		a, b := terrain.RockMaterialPoint(c, edge.A), terrain.RockMaterialPoint(c, edge.B)
		n := a.Sub(b).Perp().Norm()
		for _, p := range []geom.V{a, b} {
			key := terrain.CollisionVertexKey(p)
			corners[key] = append(corners[key], n)
		}
	}
	isTip := func(p, n geom.V) bool {
		for _, other := range corners[terrain.CollisionVertexKey(p)] {
			if n.Dot(other) < .75 {
				return true
			}
		}
		return false
	}
	for _, edge := range boundary {
		c := grid[edge.Cell]
		if !c.Raised || c.Color.A == 0 {
			continue
		}
		a, b := terrain.RockMaterialPoint(c, edge.A), terrain.RockMaterialPoint(c, edge.B)
		n := a.Sub(b).Perp().Norm()
		if n.Y < -.15 {
			ledges = append(ledges, ledge{a: a, b: b, rim: true, tipA: isTip(a, n), tipB: isTip(b, n)})
		}
	}
	for _, c := range grid {
		if !c.Raised || c.Color.A == 0 || c.Color.R < 85 || -terrain.WorldNormal(terrain.RockOrientation(c), c.Normal).Y < .3 {
			continue
		}
		for i, p := range c.Polygon {
			a, b := terrain.RockMaterialPoint(c, p), terrain.RockMaterialPoint(c, c.Polygon[(i+1)%len(c.Polygon)])
			if a.Sub(b).Perp().Norm().Y > .35 {
				ledges = append(ledges, ledge{a: a, b: b, light: geom.Clamp((float64(c.Color.R)-85)/100, 0, 1)})
			}
		}
	}
	type candidate struct {
		colony mossColony
		score  float64
		edge   [4]int64
	}
	candidates := make(map[[2]int64]candidate)
	ref := grid[0]
	origin := terrain.RockMaterialPoint(ref, geom.V{})
	mode := terrain.RockOrientation(ref)
	for _, edge := range ledges {
		d := edge.b.Sub(edge.a)
		if d.Len2() < 1e-12 {
			continue
		}
		lo, hi := geom.PolygonBounds([]geom.V{edge.a, edge.b})
		for x := int64(math.Floor((lo.X - reach) / spacing)); x <= int64(math.Floor((hi.X+reach)/spacing)); x++ {
			for y := int64(math.Floor((lo.Y - reach) / spacing)); y <= int64(math.Floor((hi.Y+reach)/spacing)); y++ {
				random := func(channel int64) float64 { return terrain.SiteRandom(0x6d6f7373636f6c, x, y, channel) }
				if random(0) > density {
					continue
				}
				p := geom.V{X: (float64(x) + .25 + .5*random(1)) * spacing, Y: (float64(y) + .25 + .5*random(2)) * spacing}
				t := geom.Clamp(p.Sub(edge.a).Dot(d)/d.Len2(), 0, 1)
				q := edge.a.Add(d.Mul(t))
				distance := p.Sub(q).Len2()
				if distance > reach*reach {
					continue
				}
				feature := 0.0
				if edge.tipA {
					feature = math.Exp(-q.Sub(edge.a).Len2() / .0003)
				}
				if edge.tipB {
					feature = max(feature, math.Exp(-q.Sub(edge.b).Len2()/.0003))
				}
				score := distance / (1 + feature*.7 + edge.light*.8)
				// Prefer the foot of an upward-facing facet when a site can
				// reach both it and the exposed rim. Nearby tips can still
				// attract colonies, and rim-only sites remain eligible.
				if random(6) < .18 {
					if edge.rim {
						score *= .65
					}
				} else if edge.rim {
					score += reach * reach * .2
				} else {
					score *= .35
				}
				key, edgeKey := [2]int64{x, y}, geom.EdgeKey(edge.a, edge.b)
				previous, exists := candidates[key]
				if exists && (score > previous.score+1e-15 || (math.Abs(score-previous.score) <= 1e-15 && !mossEdgeLess(edgeKey, previous.edge))) {
					continue
				}
				tangent := d.Norm()
				if tangent.X < 0 {
					tangent = tangent.Mul(-1)
				}
				sample := int(random(3) * 32)
				scale := geom.Lerp(.45, .80, random(5)) + feature*.10
				if !edge.rim {
					// Spread the smaller colonies farther onto their lit
					// supporting face while retaining a fringe over the seam.
					inset := (.009 + .003*edge.light) * scale
					q = q.Sub(edge.a.Sub(edge.b).Perp().Norm().Mul(inset))
				}
				if terrain.VegetationGreen(q) < .5 {
					sample += 32
				}
				colony := mossColony{anchor: terrain.InternalPoint(mode, q.Sub(origin)), rotation: math.Atan2(tangent.Y, tangent.X), scale: scale, sample: sample}
				candidates[key] = candidate{colony: colony, score: score, edge: edgeKey}
			}
		}
	}
	keys := make([][2]int64, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || (keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1])
	})
	colonies := make([]mossColony, 0, len(keys))
	for _, key := range keys {
		colonies = append(colonies, candidates[key].colony)
	}
	return colonies
}

func mossEdgeLess(a, b [4]int64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Colonies rooted near a horizontal seam follow that seam laterally. The world
// field still owns their centers, sizes, and palettes; this only fits the growth
// direction to nearby edges of lit, upward-facing cells.
func mossSeamRotation(colony mossColony, rock growthClearance) float64 {
	radius := .028 * colony.scale
	pad := geom.V{X: radius, Y: radius}
	bounds := geom.Bounds{Min: colony.anchor.Sub(pad), Max: colony.anchor.Add(pad)}
	best, rotation := radius*radius, colony.rotation
	var bestKey [4]int64
	found := false
	rock.index.Query(bounds, func(i int) bool {
		c := rock.grid[i]
		mode := terrain.RockOrientation(c)
		if !c.Raised || c.Color.A == 0 || c.Color.R < 85 || -terrain.WorldNormal(mode, c.Normal).Y < .3 {
			return true
		}
		for j, a := range c.Polygon {
			b := c.Polygon[(j+1)%len(c.Polygon)]
			direction := terrain.WorldPoint(mode, b.Sub(a))
			if direction.Len2() < 1e-12 || math.Abs(direction.X)/direction.Len() < .75 {
				continue
			}
			distance := geom.SegmentDistanceSquared(colony.anchor, colony.anchor, a, b)
			key := geom.EdgeKey(terrain.RockMaterialPoint(c, a), terrain.RockMaterialPoint(c, b))
			if distance > best+1e-15 || (found && math.Abs(distance-best) <= 1e-15 && !mossEdgeLess(key, bestKey)) {
				continue
			}
			if direction.X < 0 || (direction.X == 0 && direction.Y < 0) {
				direction = direction.Mul(-1)
			}
			best, bestKey, found = distance, key, true
			rotation = math.Atan2(direction.Y, direction.X)
		}
		return true
	})
	return rotation
}

// One atlas mask wraps over every receiving facet. Geometry clips it to solid
// rock while each face supplies its own light; seams never restart the texture.
func prepareRockMoss(grid terrain.RockGrid, boundary []terrain.RockEdge, rock growthClearance, density float64, owned bool) []Attachment {
	var out []Attachment
	for _, colony := range rockMossColonies(grid, boundary, density) {
		colony.rotation = mossSeamRotation(colony, rock)
		mode := terrain.RockOrientation(grid[0])
		a := newSpriteAttachment(SurfaceMoss, MossAtlas, colony.sample, colony.anchor, colony.rotation, colony.scale, mode, 1)
		if owned && !inTerrainBand(a.Min, a.Max) {
			continue
		}
		a.Anchors = nil
		poly := a.Footprint[0]
		a.Mesh.Vertices, a.Mesh.Indices = nil, nil
		bounds := geom.Bounds{Min: a.Min, Max: a.Max}
		rock.index.Query(bounds, func(i int) bool {
			c := grid[i]
			if !c.Raised || c.Color.A == 0 {
				return true
			}
			lo, hi := geom.PolygonBounds(c.Polygon)
			if !bounds.Overlaps(geom.Bounds{Min: lo, Max: hi}) {
				return true
			}
			tint := .38 + .62*geom.Clamp(float64(c.Color.R)/165, 0, 1)
			for _, tri := range geom.Triangulate(c.Polygon) {
				piece := poly
				for j, index := range tri {
					p, q := c.Polygon[index], c.Polygon[tri[(j+1)%3]]
					n := p.Sub(q).Perp().Norm()
					piece = geom.ClipHalfPlane(piece, n, p.Dot(n))
				}
				if len(piece) >= 3 && geom.PolygonArea(piece) > 1e-15 {
					appendSpritePolygon(&a.Mesh, piece, MossAtlas, colony.sample, a.Anchor, a.Rotation, a.Scale, mode, tint)
				}
			}
			return true
		})
		if len(a.Mesh.Indices) > 0 {
			out = append(out, a)
		}
	}
	return out
}

type hangingGrowth struct {
	stem   []geom.V
	body   []geom.V
	lobes  [][]geom.V
	center geom.V
	radius float64
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
	growth := hangingGrowth{radius: geom.Lerp(.0023, .0048, random(5))}
	root := anchor.Sub(outward.Mul(.0006))
	for i := 0; i < 6; i++ {
		s := float64(i) / 5
		growth.stem = append(growth.stem, root.Add(down.Mul(length*s)))
	}
	growth.center = growth.stem[len(growth.stem)-1].Add(down.Mul(growth.radius * .55))
	for i := 0; i < 10; i++ {
		angle := float64(i)*2*math.Pi/10 - math.Pi/2
		r := growth.radius * geom.Lerp(.87, 1.12, random(10+int64(i)))
		// A narrow neck and a fuller droplet underneath the attachment.
		width := .8 + .2*math.Sin(angle)
		growth.body = append(growth.body, growth.center.Add(right.Mul(math.Cos(angle)*r*width)).Add(down.Mul(math.Sin(angle)*r*1.2)))
	}
	// Small alternating lobes turn every stem into a hanging lichen tuft.
	for i := 2; i < len(growth.stem); i++ {
		p := growth.stem[i]
		side := right.Mul(float64(2*(i%2)-1) * growth.radius * .8)
		leaf := []geom.V{p.Sub(down.Mul(.002)), p.Add(side).Sub(down.Mul(.001)), p.Add(side.Mul(.65)).Add(down.Mul(.002)), p.Add(down.Mul(.003))}
		if geom.PolygonArea(leaf) < 0 {
			leaf[1], leaf[3] = leaf[3], leaf[1]
		}
		growth.lobes = append(growth.lobes, leaf)
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
		palette := color.NRGBA{R: 155, G: 169, B: 93, A: 255}
		for _, leaf := range growth.lobes {
			vertices, indices = appendCellMesh(vertices, indices, leaf, geom.PolygonCenter(leaf), growthColor(palette, .72), geom.V3{Z: 1})
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

// The lower edges of guide-facing tops can lie inside a formation. Bulbs hang
// over its darker flanks, above the rock texture, but never across another top.
func prepareGuideBulbs(grid terrain.RockGrid, boundary []terrain.RockEdge, growth RockGrowthOptions, owned bool) []Attachment {
	if growth.Hanging <= 0 || len(growth.Guides) == 0 {
		return nil
	}
	var tops terrain.RockGrid
	for _, c := range grid {
		if len(c.Polygon) >= 3 && c.Color.A != 0 && -terrain.WorldNormal(terrain.RockOrientation(c), c.Normal).Y >= .10 {
			tops = append(tops, c)
		}
	}
	clearance := newGrowthClearance(tops)
	exposed := make(map[[4]int64]bool, len(boundary))
	for _, e := range boundary {
		exposed[geom.EdgeKey(e.A, e.B)] = true
	}
	var out []Attachment
	for i, c := range tops {
		if !c.Raised {
			continue
		}
		if guide, _ := grassGuideAnchor(c, growth.Guides); guide < 0 {
			continue
		}
		mode := terrain.RockOrientation(c)
		for j, p := range c.Polygon {
			q := c.Polygon[(j+1)%len(c.Polygon)]
			outward := q.Sub(p).Norm().Perp().Mul(-1)
			if exposed[geom.EdgeKey(p, q)] || q.Sub(p).Len() < .008 || terrain.WorldPoint(mode, outward).Y < .45 {
				continue
			}
			random := rockGrowthRandom(c, geom.LerpVector(p, q, .5), 0x62756c6273)
			if random(0) > growth.Hanging {
				continue
			}
			anchor := geom.LerpVector(p, q, .22+.56*random(1))
			support, ok := terrain.InsetRockAnchor(c.Polygon, anchor, outward.Mul(-1))
			if !ok {
				continue
			}
			a := newSpriteAttachment(HangingAttachment, HangingAtlas, int(random(3)*64), anchor, 0, .75+.35*random(2), mode, 1)
			a.Anchors = []geom.V{support}
			if (owned && !inTerrainBand(a.Min, a.Max)) || !clearance.fitsAroundSupport(a, i) {
				continue
			}
			out = append(out, a)
		}
	}
	return out
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
	return c.fitsAroundSupport(a, -1)
}

// Guide bulbs may overlap their supporting top. Other tops must clear every
// painted triangle, including the root and narrow stalk segments.
func (c growthClearance) fitsAroundSupport(a Attachment, support int) bool {
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
		if nearRoot && support < 0 {
			continue
		}
		if geom.PolygonArea(tri[:]) < 0 {
			tri[0], tri[2] = tri[2], tri[0]
		}
		lo, hi := geom.PolygonBounds(tri[:])
		clear := true
		c.index.Query(geom.Bounds{Min: lo, Max: hi}, func(j int) bool {
			if c.grid[j].Color.A == 0 || j == support {
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
