package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// One batched draw: per-mushroom vector paths allocated gigabytes per section.
// Caps stay in front of rock; stalks and their borders stop at its silhouette.
func PrepareMushrooms(groups []terrain.MushroomGroup, topology *terrain.RockTopology) TriangleMesh {
	if len(groups) == 0 {
		return TriangleMesh{}
	}
	rock := mushroomRockSilhouette(topology)
	var vertices []ebiten.Vertex
	var indices []uint32
	vertex := func(p geom.V, c color.NRGBA) uint32 {
		a := float32(c.A) / 255
		vertices = append(vertices, ebiten.Vertex{DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: .5, SrcY: .5,
			ColorR: float32(c.R) / 255 * a, ColorG: float32(c.G) / 255 * a, ColorB: float32(c.B) / 255 * a, ColorA: a})
		return uint32(len(vertices) - 1)
	}
	ribbon := func(points []geom.V, width float64, c color.NRGBA, clips [][]geom.V) {
		var prev [2]geom.V
		triangle := func(a, b, d geom.V) {
			for _, poly := range clipMushroomStalk([]geom.V{a, b, d}, clips) {
				first := uint32(len(vertices))
				for _, p := range poly {
					vertex(p, c)
				}
				for i := 1; i+1 < len(poly); i++ {
					indices = append(indices, first, first+uint32(i), first+uint32(i+1))
				}
			}
		}
		for i, p := range points {
			n := points[min(len(points)-1, i+1)].Sub(points[max(0, i-1)]).Norm().Perp().Mul(width / 2)
			cur := [2]geom.V{p.Add(n), p.Sub(n)}
			if i > 0 {
				triangle(prev[0], prev[1], cur[1])
				triangle(prev[0], cur[1], cur[0])
			}
			prev = cur
		}
	}
	fan := func(center geom.V, outline []geom.V, grow float64, shade func(geom.V) color.NRGBA) {
		mid := vertex(center, shade(center))
		first := uint32(len(vertices))
		for _, p := range outline {
			vertex(p.Add(p.Sub(center).Norm().Mul(grow)), shade(p))
		}
		for i := range outline {
			indices = append(indices, mid, first+uint32(i), first+uint32((i+1)%len(outline)))
		}
	}
	for _, group := range groups {
		for _, m := range group.Mushrooms {
			clips := rock.stalkClips(m)
			ribbon(m.Stem, m.StemWidth(), color.NRGBA{R: 108, G: 70, B: 32, A: 255}, clips)
			ribbon(m.Stem, m.CapWidth*.11, color.NRGBA{R: 255, G: 233, B: 167, A: 255}, clips)
			outline := terrain.MushroomCapOutline(m)
			center := m.CapCenter.Add(terrain.InternalPoint(terrain.MushroomOrientation(m), geom.V{X: 0, Y: -m.CapHeight * .4}))
			fan(center, outline, .001, func(geom.V) color.NRGBA {
				return color.NRGBA{R: 115, G: 60, B: 23, A: 255}
			})
			fan(center, outline, 0, func(p geom.V) color.NRGBA {
				// Shade the existing cap fan in world axes, including horizontal
				// caves. A pale underside and upper-left highlight suggest glow.
				d := terrain.WorldPoint(terrain.MushroomOrientation(m), p.Sub(m.CapCenter))
				x := d.X / math.Max(m.CapWidth*.5, 1e-9)
				y := d.Y / math.Max(m.CapHeight, 1e-9)
				underside := geom.Smoothstep(-.18, .05, y)
				highlight := .35 * geom.Clamp(1-math.Hypot((x+.28)*1.2, (y+.65)*1.6), 0, 1)
				light := math.Max(underside*.88, highlight)
				return color.NRGBA{
					R: uint8(math.Round(geom.Lerp(float64(m.Color.R), 255, light))),
					G: uint8(math.Round(geom.Lerp(float64(m.Color.G), 239, light))),
					B: uint8(math.Round(geom.Lerp(float64(m.Color.B), 155, light))), A: m.Color.A,
				}
			})
		}
	}
	return TriangleMesh{Vertices: vertices, Indices: indices}
}

type mushroomRockFace struct {
	poly   []geom.V
	lo, hi geom.V
}

type mushroomRock []mushroomRockFace

func mushroomSectionTopology(data terrain.SectionData) *terrain.RockTopology {
	if data.ForegroundTopology != nil || len(data.Mushrooms) == 0 {
		return data.ForegroundTopology
	}
	return terrain.NewRockTopology(data.Foreground)
}

// Use the final inset/cut faces and the same projected walls as rock rendering.
// Keep the padded silhouette so stalks crossing a section seam are clipped too.
func mushroomRockSilhouette(topology *terrain.RockTopology) mushroomRock {
	var rock mushroomRock
	if topology == nil {
		return rock
	}
	add := func(poly []geom.V) {
		if len(poly) >= 3 {
			if geom.PolygonArea(poly) < 0 {
				ordered := make([]geom.V, len(poly))
				for i, p := range poly {
					ordered[len(poly)-1-i] = p
				}
				poly = ordered
			}
			lo, hi := geom.PolygonBounds(poly)
			rock = append(rock, mushroomRockFace{poly, lo, hi})
		}
	}
	for _, cell := range topology.Grid {
		if cell.Color.A != 0 {
			add(cell.Polygon)
		}
	}
	for _, edge := range topology.Boundary {
		cell := topology.Grid[edge.Cell]
		if cell.Raised {
			if points, visible := rockWallPoints(cell, edge); visible {
				add(points[:])
			}
		}
	}
	return rock
}

func (rock mushroomRock) stalkClips(m terrain.Mushroom) [][]geom.V {
	if len(m.Stem) < 2 {
		return nil
	}
	lo, hi := geom.PolygonBounds(m.Stem)
	radius := m.StemWidth() / 2
	var clips [][]geom.V
	for _, face := range rock {
		if hi.X+radius <= face.lo.X || lo.X-radius >= face.hi.X || hi.Y+radius <= face.lo.Y || lo.Y-radius >= face.hi.Y {
			continue
		}
		// Concave guide faces must be decomposed, rather than treated as
		// intersecting half-planes, which would erase open pockets.
		for _, tri := range geom.Triangulate(face.poly) {
			clips = append(clips, []geom.V{face.poly[tri[0]], face.poly[tri[1]], face.poly[tri[2]]})
		}
	}
	return clips
}

// Subtract each rock triangle from the full stalk ribbon, including its border.
// Peeling outside half-planes leaves disjoint convex pieces for fan tessellation.
func clipMushroomStalk(poly []geom.V, clips [][]geom.V) [][]geom.V {
	pieces := [][]geom.V{poly}
	for _, clip := range clips {
		var next [][]geom.V
		for _, piece := range pieces {
			overlap := piece
			for i, a := range clip {
				n := clip[(i+1)%len(clip)].Sub(a).Perp().Mul(-1).Norm()
				overlap = geom.ClipHalfPlane(overlap, n, a.Dot(n))
			}
			if len(overlap) < 3 || math.Abs(geom.PolygonArea(overlap)) <= 1e-15 {
				next = append(next, piece)
				continue
			}
			inside := piece
			for i, a := range clip {
				n := clip[(i+1)%len(clip)].Sub(a).Perp().Mul(-1).Norm()
				outside := geom.ClipHalfPlane(inside, n.Mul(-1), -a.Dot(n))
				if len(outside) >= 3 && math.Abs(geom.PolygonArea(outside)) > 1e-15 {
					next = append(next, outside)
				}
				inside = geom.ClipHalfPlane(inside, n, a.Dot(n))
				if len(inside) < 3 || math.Abs(geom.PolygonArea(inside)) <= 1e-15 {
					break
				}
			}
		}
		pieces = next
		if len(pieces) == 0 {
			break
		}
	}
	return pieces
}
