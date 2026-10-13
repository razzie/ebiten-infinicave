package render

import (
	"image/color"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
		if terrain.WorldPoint(mode, inward).Y < .15 {
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
	yellow := random(2) < .48
	if yellow {
		clr = color.NRGBA{R: 206, G: 171, B: 88, A: 255}
	}
	triangles := geom.Triangulate(c.Polygon)
	type rootPoint struct {
		p     geom.V
		along float64
	}
	var roots []rootPoint
	half := .017 * scale
	for edge, e := range support {
		if len(roots) >= 12 {
			break
		}
		d := e.B.Sub(e.A)
		length := d.Len()
		if length < .001 {
			continue
		}
		inward := d.Norm().Perp()
		if terrain.WorldPoint(mode, inward).Y < .15 {
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
		if (hi-lo)*length < .002 {
			continue
		}
		count := max(2, min(12-len(roots), int(math.Ceil((hi-lo)*length/.0035))))
		for j := 0; j < min(3, count); j++ {
			x0, x1 := geom.Lerp(lo, hi, float64(j)/float64(min(3, count))), geom.Lerp(lo, hi, float64(j+1)/float64(min(3, count)))
			a, b := e.A.Add(d.Mul(x0)), e.A.Add(d.Mul(x1))
			depth := scale * (.004 + .003*random(100+int64(edge)*31+int64(j)))
			poly := []geom.V{a.Add(inward.Mul(.0002)), b.Add(inward.Mul(.0002)), b.Add(inward.Mul(depth)), a.Add(inward.Mul(depth * .85))}
			for _, tri := range triangles {
				piece := poly
				for k, index := range tri {
					p, q := c.Polygon[index], c.Polygon[tri[(k+1)%3]]
					n := q.Sub(p).Perp().Mul(-1).Norm()
					piece = geom.ClipHalfPlane(piece, n, p.Dot(n))
				}
				if geom.PolygonArea(piece) > 1e-15 {
					patch.Mesh.Vertices, patch.Mesh.Indices = appendCellMesh(patch.Mesh.Vertices, patch.Mesh.Indices, piece, geom.PolygonCenter(piece), growthColor(clr, tint*.78), c.Normal)
					patch.Footprint = append(patch.Footprint, piece)
				}
			}

		}
		for j := 0; j < count; j++ {
			u := geom.Lerp(lo, hi, (float64(j)+.25+.5*random(200+int64(edge)*31+int64(j)))/float64(count))
			root := e.A.Add(d.Mul(u)).Add(inward.Mul(.001 * scale))
			if geom.InsidePolygon(root, c.Polygon) {
				roots = append(roots, rootPoint{root, geom.Clamp(root.Sub(anchor).Dot(tangent)/half, -1, 1)})
				patch.Anchors = append(patch.Anchors, root)
			}
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
		rotation := angle*.10 + point.along*.18 + (random(11+int64(i)*3)-.5)*.12
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
