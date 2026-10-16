package render

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Erase cut coverage directly from a finished texture, including shadows and
// surface decorations. Clip to the target image or dirty subimage before drawing.
func EraseCutsAt(dst *ebiten.Image, bounds image.Rectangle, windowTop float64, cuts []terrain.RockCut, pixels int) {
	EraseCutsWithAntialiasing(dst, bounds, windowTop, cuts, pixels, true)
}

func EraseCutsWithAntialiasing(dst *ebiten.Image, bounds image.Rectangle, windowTop float64, cuts []terrain.RockCut, pixels int, aa bool) {
	if dst == nil || bounds.Empty() {
		return
	}
	target := dst.Bounds().Add(bounds.Min)
	lo := geom.V{X: float64(target.Min.X) / float64(pixels), Y: windowTop + float64(target.Min.Y)/float64(pixels)}
	hi := geom.V{X: float64(target.Max.X) / float64(pixels), Y: windowTop + float64(target.Max.Y)/float64(pixels)}
	// Clip beyond the subimage's edge so antialiasing cannot introduce a seam
	// along a dirty tile boundary inside an otherwise fully erased cut.
	pad := geom.V{X: 2 / float64(pixels), Y: 2 / float64(pixels)}
	for _, cut := range cuts {
		// Clip before converting to float32 pixels, including very large cuts.
		if cut.Max.X <= lo.X || cut.Min.X >= hi.X || cut.Max.Y <= lo.Y || cut.Min.Y >= hi.Y {
			continue
		}
		poly := geom.ClipHalfPlane(cut.Poly, geom.V{X: -1, Y: 0}, -lo.X+pad.X)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 1, Y: 0}, hi.X+pad.X)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 0, Y: -1}, -lo.Y+pad.Y)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 0, Y: 1}, hi.Y+pad.Y)
		if len(poly) < 3 {
			continue
		}
		var path vector.Path
		for i, p := range poly {
			x, y := float32(p.X*float64(pixels)-float64(bounds.Min.X)), float32((p.Y-windowTop)*float64(pixels)-float64(bounds.Min.Y))
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		path.Close()
		vector.FillPath(dst, &path, nil, &vector.DrawPathOptions{AntiAlias: aa, Blend: ebiten.BlendDestinationOut})
	}
}

// Give newly exposed rock a recessed, dark wall and a chipped mineral lip.
// This is decorative depth, like the existing rock walls; collision stays on
// the actual cut perimeter. World-space variation keeps reloads and seams fixed.
func AppendCarveRims(vertices []ebiten.Vertex, indices []uint32, topology *terrain.RockTopology, view View) ([]ebiten.Vertex, []uint32) {
	return appendCarveRims(vertices, indices, topology, view, false)
}

func appendCarveRims(vertices []ebiten.Vertex, indices []uint32, topology *terrain.RockTopology, view View, owned bool) ([]ebiten.Vertex, []uint32) {
	for _, edge := range topology.Boundary {
		// Rims extend at most .009 scene units from the cut edge.
		if owned && !inTerrainBand(edge.A.Add(geom.V{Y: -.009}), edge.B.Add(geom.V{Y: -.009}), edge.A.Add(geom.V{Y: .009}), edge.B.Add(geom.V{Y: .009})) {
			continue
		}
		mid := geom.LerpVector(edge.A, edge.B, .5).Add(geom.V{Y: topology.Top})
		onCut := false
		for _, cut := range topology.Cuts {
			if mid.X < cut.Min.X-1e-8 || mid.X > cut.Max.X+1e-8 || mid.Y < cut.Min.Y-1e-8 || mid.Y > cut.Max.Y+1e-8 {
				continue
			}
			for i, p := range cut.Poly {
				if geom.SegmentDistanceSquared(mid, mid, p, cut.Poly[(i+1)%len(cut.Poly)]) < 1e-16 {
					onCut = true
					break
				}
			}
			if onCut {
				break
			}
		}
		if !onCut {
			continue
		}
		cell := topology.Grid[edge.Cell]
		outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
		light := geom.Clamp(outward.Dot(terrain.InternalPoint(terrain.RockOrientation(cell), geom.V{X: -.6, Y: -.8}))*.5+.5, 0, 1)
		steps := max(1, int(math.Ceil(edge.B.Sub(edge.A).Len()/.009)))
		for step := 0; step < steps; step++ {
			a := geom.LerpVector(edge.A, edge.B, float64(step)/float64(steps))
			b := geom.LerpVector(edge.A, edge.B, float64(step+1)/float64(steps))
			worldMid := geom.LerpVector(a, b, .5).Add(geom.V{Y: topology.Top})
			key := terrain.CollisionVertexKey(worldMid)
			variation := terrain.SiteRandom(0x637261746572, key[0], key[1], 0)
			// Depth is strongest on the wall facing the light; the lower
			// wall gets a thin reflected edge and otherwise falls into shade.
			depth := .003 + .004*light + .002*variation
			shadow := color.NRGBA{R: uint8(15 + 17*light), G: uint8(11 + 12*light), B: uint8(8 + 8*light), A: 245}
			shadow = rockViewColor(terrain.RockCell{Color: shadow, Raised: true}, view)
			vertices, indices = appendRockQuad(vertices, indices,
				[4]geom.V{a, b, b.Add(outward.Mul(depth)), a.Add(outward.Mul(depth * (.7 + .3*variation)))}, shadow)
			width := .0025 + .0045*variation
			for width > .0002 && !geom.InsidePolygon(worldMid.Sub(geom.V{Y: topology.Top}).Sub(outward.Mul(width)), cell.Polygon) {
				width *= .5
			}
			lipA, lipB := a.Sub(outward.Mul(width*(.45+.55*variation))), b.Sub(outward.Mul(width))
			// The rim combines exposed warm mineral with scorched, darker
			// facets. Unequal lip widths break the otherwise smooth outline.
			strength := .85 + .38*light + .22*variation
			lip := color.NRGBA{
				R: uint8(geom.Clamp(math.Max(38+30*light, float64(cell.Color.R)*strength+20), 0, 255)),
				G: uint8(geom.Clamp(math.Max(29+23*light, float64(cell.Color.G)*strength+12), 0, 255)),
				B: uint8(geom.Clamp(math.Max(18+15*light, float64(cell.Color.B)*strength+3), 0, 255)), A: 255,
			}
			lip = rockViewColor(terrain.RockCell{Color: lip, Raised: true}, view)
			vertices, indices = appendRockQuad(vertices, indices, [4]geom.V{a, b, lipB, lipA}, lip)
		}
	}
	return vertices, indices
}

// PrepareShapeRims consumes tagged current cut boundaries; it does not scan
// historical cuts or change source rock lighting and surface decorations.
func PrepareShapeRims(geometry *terrain.Geometry, view View) TriangleMesh {
	return PrepareShapeRimsInBounds(geometry, view, geom.Bounds{Min: geom.V{X: -1, Y: terrain.GenerationMinY - 1}, Max: geom.V{X: 2, Y: 3}})
}

func PrepareShapeRimsInBounds(geometry *terrain.Geometry, view View, bounds geom.Bounds) TriangleMesh {
	m := TriangleMesh{MaterialFrame: ReferenceMaterialFrame()}
	if geometry == nil || (view != ViewShaded && view != ViewClay) {
		return m
	}
	for _, e := range geometry.CutEdgesInBounds(bounds) {
		outward := e.B.Sub(e.A).Norm().Perp().Mul(-1)
		mid := geom.LerpVector(e.A, e.B, .5)
		key := terrain.CollisionVertexKey(mid.Add(geom.V{Y: geometry.Top}))
		r := terrain.SiteRandom(0x637261746572, key[0], key[1], 0)
		depth := .003 + .003*r
		width := .0015 + .002*r
		shadow := color.NRGBA{R: 30, G: 23, B: 17, A: 240}
		lip := color.NRGBA{R: uint8(102 + 40*r), G: uint8(85 + 35*r), B: uint8(62 + 25*r), A: 255}
		if view == ViewClay {
			shadow = color.NRGBA{R: 25, G: 25, B: 25, A: 240}
			lip = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
		}
		m.Vertices, m.Indices = appendRockQuad(m.Vertices, m.Indices, [4]geom.V{e.A, e.B, e.B.Add(outward.Mul(depth)), e.A.Add(outward.Mul(depth))}, shadow)
		m.Vertices, m.Indices = appendRockQuad(m.Vertices, m.Indices, [4]geom.V{e.A, e.B, e.B.Sub(outward.Mul(width)), e.A.Sub(outward.Mul(width))}, lip)
	}
	return m
}
