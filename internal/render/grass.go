package render

import (
	"image/color"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

const DefaultGrassDensity = .35

type grassBlade struct {
	root    geom.V
	height  float64
	polygon []geom.V
}

type grassPatch struct {
	blades []grassBlade
	ground [][]geom.V
	color  color.NRGBA
}

const grassRootGuideDistance = .004

// Find the closest guide whose crest still has supporting rock immediately
// below it. A nearby guide outside the face, or removed by a cut, is ineligible.
func grassGuideAnchor(cell terrain.RockCell, guides []terrain.Guide) (int, geom.V) {
	down := terrain.InternalPoint(terrain.RockOrientation(cell), geom.V{Y: 1})
	best, distance, anchor := -1, math.Inf(1), geom.V{}
	lo, hi := geom.PolygonBounds(cell.Polygon)
	for i := range guides {
		g := &guides[i]
		if len(g.Pts) < 2 {
			continue
		}
		guideLo, guideHi := geom.PolygonBounds(g.Pts)
		if guideHi.X < lo.X-grassRootGuideDistance || guideLo.X > hi.X+grassRootGuideDistance ||
			guideHi.Y < lo.Y-grassRootGuideDistance || guideLo.Y > hi.Y+grassRootGuideDistance {
			continue
		}
		q, d := g.NearestPoint(cell.Center)
		if d < distance && geom.InsidePolygon(q.Add(down.Mul(.003)), cell.Polygon) {
			best, distance, anchor = i, d, q
		}
	}
	return best, anchor
}

// Each eligible facet chooses a single tuft color and a fan of unequal,
// curved blades. Density uses its own channel so surviving tufts retain their
// roots, palette and shape across settings, mesh rebuilds and padded windows.
func grassForCell(cell terrain.RockCell, guides []terrain.Guide, density float64) grassPatch {
	if density <= 0 || len(guides) == 0 || !cell.Raised || cell.Color.A == 0 || len(cell.Polygon) < 3 {
		return grassPatch{}
	}
	mode := terrain.RockOrientation(cell)
	upward := -terrain.WorldNormal(mode, cell.Normal).Y
	random := rockGrowthRandom(cell, cell.Center, 0x6772617373)
	if upward < .10 || random(0) > density*geom.Smoothstep(.08, .35, upward) {
		return grassPatch{}
	}
	size := math.Min(.017, math.Sqrt(math.Abs(geom.PolygonArea(cell.Polygon)))*.48)
	if size < .002 {
		return grassPatch{}
	}
	guide, anchor := grassGuideAnchor(cell, guides)
	if guide < 0 {
		return grassPatch{}
	}
	up := terrain.InternalPoint(mode, geom.V{Y: -1})
	right := terrain.InternalPoint(mode, geom.V{X: 1})
	center := anchor.Add(right.Mul((random(1) - .5) * size * .5))
	patch := grassPatch{color: color.NRGBA{R: 126, G: 146, B: 65, A: 255}}
	if random(2) < .48 {
		patch.color = color.NRGBA{R: 206, G: 171, B: 88, A: 255}
	}
	light := .62 + .38*geom.Clamp(float64(cell.Color.R)/165, 0, 1)
	patch.color = growthColor(patch.color, light)
	triangles := geom.Triangulate(cell.Polygon)
	count := 7 + int(random(3)*6)
	for i := 0; i < count; i++ {
		channel := 10 + int64(i)*5
		along := geom.Lerp(-.8, .8, float64(i)/float64(count-1))
		q, _ := guides[guide].NearestPoint(center.Add(right.Mul(size * along)))
		root := q.Sub(up.Mul(geom.Lerp(.002, grassRootGuideDistance, random(channel))))
		if !geom.InsidePolygon(root, cell.Polygon) {
			continue
		}
		height := geom.Lerp(.010, .029, random(channel+1)) * geom.Clamp(size/.009, .55, 1)
		bend := size * (along*.75 + geom.Lerp(-.4, .4, random(channel+2)))
		width := geom.Lerp(.0008, .0018, random(channel+3))
		control := root.Add(up.Mul(height * .65)).Add(right.Mul(bend * .15))
		tip := root.Add(up.Mul(height)).Add(right.Mul(bend))
		var left, rightSide [3]geom.V
		for j := range left {
			t := float64(j) / 3
			p := root.Mul((1 - t) * (1 - t)).Add(control.Mul(2 * (1 - t) * t)).Add(tip.Mul(t * t))
			tangent := control.Sub(root).Mul(1 - t).Add(tip.Sub(control).Mul(t)).Norm()
			n := tangent.Perp().Mul(width * .5 * math.Pow(1-t, 1.2))
			left[j], rightSide[j] = p.Sub(n), p.Add(n)
		}
		poly := []geom.V{left[0], left[1], left[2], tip, rightSide[2], rightSide[1], rightSide[0]}
		patch.blades = append(patch.blades, grassBlade{root: root, height: height, polygon: poly})
		// Overlapping pieces form a small irregular sod strip following the
		// guide. Clip each piece to the actual face, including concave cuts.
		half := math.Max(.0025, size*1.6/float64(count)+.0012)
		depth := geom.Lerp(.006, .009, random(channel+4))
		top := q.Add(up.Mul(.0005))
		bottom := q.Sub(up.Mul(depth))
		ground := []geom.V{top.Sub(right.Mul(half)), top.Add(right.Mul(half)), bottom.Add(right.Mul(half)), bottom.Sub(right.Mul(half))}
		for _, tri := range triangles {
			piece := ground
			for j, index := range tri {
				a, b := cell.Polygon[index], cell.Polygon[tri[(j+1)%3]]
				n := b.Sub(a).Perp().Mul(-1).Norm()
				piece = geom.ClipHalfPlane(piece, n, a.Dot(n))
			}
			if len(piece) >= 3 && geom.PolygonArea(piece) > 1e-15 {
				patch.ground = append(patch.ground, piece)
			}
		}
	}
	if len(patch.blades) < 3 {
		return grassPatch{}
	}
	return patch
}

// Draw complete ground patches and blades together above custom content.
// Cull using the blade footprint so a tuft can cross the owning image's seam.
func prepareRockGrass(grid terrain.RockGrid, guides []terrain.Guide, owned bool, density float64) TriangleMesh {
	mesh := TriangleMesh{MaterialFrame: ReferenceMaterialFrame()}
	if density <= 0 || len(guides) == 0 {
		return mesh
	}
	for _, cell := range grid {
		patch := grassForCell(cell, guides, density)
		for _, poly := range patch.ground {
			if owned && !inTerrainBand(poly...) {
				continue
			}
			mesh.Vertices, mesh.Indices = appendCellMesh(mesh.Vertices, mesh.Indices, poly, geom.PolygonCenter(poly), growthColor(patch.color, .78), cell.Normal)
		}
		for _, blade := range patch.blades {
			if owned && !inTerrainBand(blade.polygon...) {
				continue
			}
			first := len(mesh.Vertices)
			mesh.Vertices, mesh.Indices = appendCellMesh(mesh.Vertices, mesh.Indices, blade.polygon, geom.PolygonCenter(blade.polygon), patch.color, geom.V3{Z: 1})
			for i := first; i < len(mesh.Vertices); i++ {
				v := &mesh.Vertices[i]
				p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
				height := -terrain.WorldPoint(terrain.RockOrientation(cell), p.Sub(blade.root)).Y / blade.height
				clr := growthColor(patch.color, geom.Lerp(.62, 1.12, geom.Clamp(height, 0, 1)))
				v.ColorR, v.ColorG, v.ColorB = float32(clr.R)/255, float32(clr.G)/255, float32(clr.B)/255
			}
		}
	}
	return mesh
}
