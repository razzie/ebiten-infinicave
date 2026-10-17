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

// Sod overlaps the silhouette so independent AA caches cannot expose a pale
// rock rim underneath. It also covers the small chips along guide crests.
const grassEdgeOverlap = .002

// Find the closest guide with supporting rock immediately below or beside it.
// A nearby guide outside the face, or removed by a cut, is ineligible.
func grassGuideAnchor(cell terrain.RockCell, guides []terrain.Guide) (int, geom.V) {
	down := terrain.InternalPoint(terrain.RockOrientation(cell), geom.V{Y: 1})
	right := down.Perp().Mul(-1)
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
		// Side-facing crests need support beside the guide, rather than
		// directly below it. This also permits grass on vertical surfaces.
		supported := geom.InsidePolygon(q.Add(down.Mul(.003)), cell.Polygon) ||
			geom.InsidePolygon(q.Add(right.Mul(.003)), cell.Polygon) ||
			geom.InsidePolygon(q.Sub(right.Mul(.003)), cell.Polygon)
		if d < distance && supported {
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
	if terrain.VegetationGreen(terrain.RockMaterialPoint(cell, anchor)) < .5 {
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
		width := geom.Lerp(.0018, .0034, random(channel+3))
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
			mesh.Vertices, mesh.Indices = appendCellMesh(mesh.Vertices, mesh.Indices, poly, geom.PolygonCenter(poly), growthColor(patch.color, .68), cell.Normal)
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

// Grass bases follow visible geometry; shading normals only decide eligibility.
func prepareGrassAttachments(c terrain.RockCell, growth RockGrowthOptions, support ...terrain.RockEdge) (Attachment, Attachment, bool) {
	mode := terrain.RockOrientation(c)
	normal := terrain.WorldNormal(mode, c.Normal)
	upward := -normal.Y
	random := rockGrowthRandom(c, c.Center, 0x6772617373)
	if growth.Grass <= 0 || !c.Raised || c.Color.A == 0 || upward < .10 || random(0) > growth.Grass*geom.Smoothstep(.08, .35, upward) {
		return Attachment{}, Attachment{}, false
	}
	guide, candidate := grassGuideAnchor(c, growth.Guides)
	if guide < 0 {
		return Attachment{}, Attachment{}, false
	}
	if len(support) == 0 {
		for j, a := range c.Polygon {
			support = append(support, terrain.RockEdge{A: a, B: c.Polygon[(j+1)%len(c.Polygon)]})
		}
	}
	best, distance := -1, .018*.018
	var anchor geom.V
	for i, e := range support {
		d := e.B.Sub(e.A)
		if d.Len2() < 1e-12 {
			continue
		}
		inward := d.Norm().Perp()
		if terrain.WorldPoint(mode, inward).Y < -1e-9 {
			continue
		}
		q := e.A.Add(d.Mul(geom.Clamp(candidate.Sub(e.A).Dot(d)/d.Len2(), 0, 1)))
		_, crestDistance := growth.Guides[guide].NearestPoint(q)
		if crestDistance > .012 {
			continue
		}
		if ds := q.Sub(candidate).Len2(); ds < distance && geom.InsidePolygon(q.Add(inward.Mul(.001)), c.Polygon) {
			best, distance, anchor = i, ds, q
		}
	}
	if best < 0 {
		return Attachment{}, Attachment{}, false
	}
	tangent := support[best].B.Sub(support[best].A).Norm()
	worldTangent := terrain.WorldPoint(mode, tangent)
	angle := math.Atan2(worldTangent.Y, worldTangent.X)
	if angle > math.Pi/2 {
		angle -= math.Pi
	}
	if angle < -math.Pi/2 {
		angle += math.Pi
	}
	scale := geom.Clamp(math.Sqrt(math.Abs(geom.PolygonArea(c.Polygon)))/.045, .5, 1)
	tint := .62 + .38*geom.Clamp(float64(c.Color.R)/165, 0, 1)
	patch := Attachment{Kind: GrassAttachment, Anchor: anchor, Orientation: mode, Scale: scale, Rotation: angle}
	clr := color.NRGBA{R: 126, G: 146, B: 65, A: 255}
	yellow := terrain.VegetationGreen(terrain.RockMaterialPoint(c, anchor)) < .5
	if yellow {
		clr = color.NRGBA{R: 206, G: 171, B: 88, A: 255}
	}
	triangles := geom.Triangulate(c.Polygon)
	type rootPoint struct {
		p                   geom.V
		along, surfaceAngle float64
	}
	var roots []rootPoint
	type crestSpan struct{ a, b, inward geom.V }
	var spans []crestSpan
	half := .017 * scale
	for edge, e := range support {
		d := e.B.Sub(e.A)
		length := d.Len()
		if length < 1e-8 {
			continue
		}
		inward := d.Norm().Perp()
		if terrain.WorldPoint(mode, inward).Y < -1e-9 {
			continue
		}
		mid := geom.LerpVector(e.A, e.B, .5)
		_, crestDistance := growth.Guides[guide].NearestPoint(mid)
		if crestDistance > .014 && edge != best {
			continue
		}
		// Intersect the tuft's small disk with this edge, including adjacent
		// corners. Unrelated edges cannot acquire a strip across an air gap.
		u := anchor.Sub(e.A).Dot(d) / d.Len2()
		closest := e.A.Add(d.Mul(u))
		perp2 := closest.Sub(anchor).Len2()
		if perp2 >= half*half {
			continue
		}
		delta := math.Sqrt(half*half-perp2) / length
		lo, hi := max(0, u-delta), min(1, u+delta)
		if (hi-lo)*length < 1e-8 {
			continue
		}
		spans = append(spans, crestSpan{e.A.Add(d.Mul(lo)), e.A.Add(d.Mul(hi)), inward})
		// Cover every eligible segment independently of the blade fan.
		// Rough silhouettes can have many short segments.
		pieces := max(1, min(3, int(math.Ceil((hi-lo)*length/.0035))))
		for j := 0; j < pieces; j++ {
			x0, x1 := geom.Lerp(lo, hi, float64(j)/float64(pieces)), geom.Lerp(lo, hi, float64(j+1)/float64(pieces))
			a, b := e.A.Add(d.Mul(x0)), e.A.Add(d.Mul(x1))
			depth := scale * (.006 + .003*random(100+int64(edge)*31+int64(j)))
			poly := []geom.V{a, b, b.Add(inward.Mul(depth)), a.Add(inward.Mul(depth * .85))}
			for _, tri := range triangles {
				piece := poly
				for k, index := range tri {
					p, q := c.Polygon[index], c.Polygon[tri[(k+1)%3]]
					n := q.Sub(p).Perp().Mul(-1).Norm()
					piece = geom.ClipHalfPlane(piece, n, p.Dot(n))
				}
				if geom.PolygonArea(piece) > 1e-15 {
					patch.Mesh.Vertices, patch.Mesh.Indices = appendCellMesh(patch.Mesh.Vertices, patch.Mesh.Indices, piece, geom.PolygonCenter(piece), growthColor(clr, tint*.68), c.Normal)
					patch.Footprint = append(patch.Footprint, piece)
				}
			}
			// The clipped inner strip alone shares the rock's AA edge and
			// leaves bright pixels visible. A narrow outer lip crosses it.
			along := d.Norm().Mul(.0004)
			lip := []geom.V{a.Sub(along).Sub(inward.Mul(grassEdgeOverlap)), b.Add(along).Sub(inward.Mul(grassEdgeOverlap)), b.Add(along).Add(inward.Mul(.001)), a.Sub(along).Add(inward.Mul(.001))}
			patch.Mesh.Vertices, patch.Mesh.Indices = appendCellMesh(patch.Mesh.Vertices, patch.Mesh.Indices, lip, geom.PolygonCenter(lip), growthColor(clr, tint*.68), c.Normal)
			patch.Footprint = append(patch.Footprint, lip)
		}
	}
	// A compact fan shares one base rather than forming a row of isolated
	// needles. Sample the whole crest, independently of its tessellation.
	count := 5 + int(random(3)*3)
	for j := 0; j < count; j++ {
		along := geom.Lerp(-1, 1, float64(j)/float64(count-1))
		target := anchor.Add(tangent.Mul(half * .42 * along))
		bestDistance := math.Inf(1)
		var root geom.V
		var surfaceAngle float64
		for _, span := range spans {
			d := span.b.Sub(span.a)
			q := span.a.Add(d.Mul(geom.Clamp(target.Sub(span.a).Dot(d)/d.Len2(), 0, 1)))
			p := q.Add(span.inward.Mul(.0015 * scale))
			if ds := q.Sub(target).Len2(); ds < bestDistance && geom.InsidePolygon(p, c.Polygon) {
				root, bestDistance = p, ds
				outward := terrain.WorldPoint(mode, span.inward.Mul(-1))
				surfaceAngle = math.Atan2(outward.X, -outward.Y)
			}
		}
		if !math.IsInf(bestDistance, 1) {
			roots = append(roots, rootPoint{root, along, surfaceAngle})
			patch.Anchors = append(patch.Anchors, root)
		}
	}
	if len(roots) < 3 || len(patch.Mesh.Indices) == 0 {
		return Attachment{}, Attachment{}, false
	}

	for i := range patch.Mesh.Vertices {
		patch.Mesh.Vertices[i].SrcX, patch.Mesh.Vertices[i].SrcY = .5, .5
	}
	blades := patch
	blades.Mesh = TriangleMesh{Texture: GrassAtlas, premultiplied: true}
	lo, hi := geom.PolygonBounds(patch.Footprint[0])
	for _, poly := range patch.Footprint {
		a, b := geom.PolygonBounds(poly)
		lo = geom.V{X: min(lo.X, a.X), Y: min(lo.Y, a.Y)}
		hi = geom.V{X: max(hi.X, b.X), Y: max(hi.Y, b.Y)}
	}
	for i, point := range roots {
		root := point.p
		cell := int(random(10+int64(i)*3) * 64)
		for (terrain.SiteRandom(0x677261737361746c, int64(cell), 0, 0) < .48) != yellow {
			cell = (cell + 1) % 64
		}
		// Split the angle between the local outward normal and world-up:
		// a vertical wall produces a roughly 45-degree upward lean.
		rotation := point.surfaceAngle*.5 + point.along*.58 + (random(11+int64(i)*3)-.5)*.12
		mesh, bounds := spriteMesh(GrassAtlas, cell, root, rotation, scale, mode, tint)
		offset := uint32(len(blades.Mesh.Vertices))
		blades.Mesh.Vertices = append(blades.Mesh.Vertices, mesh.Vertices...)
		for _, index := range mesh.Indices {
			blades.Mesh.Indices = append(blades.Mesh.Indices, offset+index)
		}
		bmin, bmax := geom.PolygonBounds(bounds)
		lo = geom.V{X: min(lo.X, bmin.X), Y: min(lo.Y, bmin.Y)}
		hi = geom.V{X: max(hi.X, bmax.X), Y: max(hi.Y, bmax.Y)}

	}
	patch.Min, patch.Max = lo, hi
	blades.Min, blades.Max, blades.Footprint = lo, hi, patch.Footprint
	return patch, blades, len(blades.Mesh.Indices) > 0
}

// Protruding rock teeth are separate structural attachments, outside the face
// polygon. Extend sod over teeth rooted in its strip so their pale tips cannot
// poke through the grass. Covered teeth also support the tuft during carving.
func coverGrassTrim(patch *Attachment, trim []Attachment) {
	tangent := terrain.InternalPoint(patch.Orientation, geom.Rotate(geom.V{X: 1}, patch.Rotation))
	half := .017*patch.Scale + .0004
	v := patch.Mesh.Vertices[0]
	clr := color.NRGBA{R: uint8(math.Round(float64(v.ColorR) * 255)), G: uint8(math.Round(float64(v.ColorG) * 255)), B: uint8(math.Round(float64(v.ColorB) * 255)), A: 255}
	base := patch.Footprint
	for _, tooth := range trim {
		if tooth.Kind != EdgeAttachment || tooth.Underlay || len(tooth.Footprint) == 0 || tooth.Max.X < patch.Min.X || tooth.Min.X > patch.Max.X || tooth.Max.Y < patch.Min.Y || tooth.Min.Y > patch.Max.Y {
			continue
		}
		covered := false
		for _, anchor := range tooth.Anchors {
			for _, piece := range base {
				covered = covered || geom.InsidePolygon(anchor, piece)
			}
		}
		if !covered {
			continue
		}
		poly := geom.ClipHalfPlane(tooth.Footprint[0], tangent, patch.Anchor.Dot(tangent)+half)
		poly = geom.ClipHalfPlane(poly, tangent.Mul(-1), -patch.Anchor.Dot(tangent)+half)
		if geom.PolygonArea(poly) <= 1e-15 {
			continue
		}
		start := len(patch.Mesh.Vertices)
		patch.Mesh.Vertices, patch.Mesh.Indices = appendCellMesh(patch.Mesh.Vertices, patch.Mesh.Indices, poly, geom.PolygonCenter(poly), clr, geom.V3{Z: 1})
		for i := start; i < len(patch.Mesh.Vertices); i++ {
			patch.Mesh.Vertices[i].SrcX, patch.Mesh.Vertices[i].SrcY = .5, .5
		}
		patch.Footprint = append(patch.Footprint, poly)
		patch.Anchors = append(patch.Anchors, tooth.Anchors...)
		lo, hi := geom.PolygonBounds(poly)
		patch.Min = geom.V{X: min(patch.Min.X, lo.X), Y: min(patch.Min.Y, lo.Y)}
		patch.Max = geom.V{X: max(patch.Max.X, hi.X), Y: max(patch.Max.Y, hi.Y)}
	}
}
