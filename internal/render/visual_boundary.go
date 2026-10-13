package render

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
