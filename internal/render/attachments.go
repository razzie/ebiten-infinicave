package render

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type AttachmentKind uint8

const (
	SurfaceMoss AttachmentKind = iota
	GrassAttachment
	HangingAttachment
	EdgeAttachment
	MushroomAttachment
	WebAttachment
)

// Attachment keeps its baked sample/mesh and support metadata across edits.
// Dst coordinates are in the original padded-window frame until rasterization.
type Attachment struct {
	Cell            int
	Anchor          geom.V
	Rotation, Scale float64
	Orientation     terrain.Orientation
	Dead            bool
	Underlay        bool
	Kind            AttachmentKind
	ID              [2]int64
	Mesh, Extra     TriangleMesh
	Min, Max        geom.V
	Anchors         []geom.V
	Footprint       [][]geom.V
}

func spriteMesh(kind AtlasKind, cell int, anchor geom.V, rotation, scale float64, mode terrain.Orientation, tint float64) (TriangleMesh, []geom.V) {
	// CPU samples are prepared by workers; a pending carve never generates
	// new sample geometry on the game goroutine.
	AtlasBatches(kind)
	s := specForAtlas(kind)
	poly := []geom.V{{X: s.min.X, Y: s.min.Y}, {X: s.max.X, Y: s.min.Y}, {X: s.max.X, Y: s.max.Y}, {X: s.min.X, Y: s.max.Y}}
	for i, p := range poly {
		poly[i] = anchor.Add(terrain.InternalPoint(mode, geom.Rotate(p, rotation).Mul(scale)))
	}
	m := TriangleMesh{Texture: kind, premultiplied: true}
	appendSpritePolygon(&m, poly, kind, cell, anchor, rotation, scale, mode, tint)
	return m, poly
}

func appendSpritePolygon(m *TriangleMesh, poly []geom.V, kind AtlasKind, cell int, anchor geom.V, rotation, scale float64, mode terrain.Orientation, tint float64) {
	if len(poly) < 3 {
		return
	}
	s := specForAtlas(kind)
	offset := uint32(len(m.Vertices))
	for _, p := range poly {
		q := geom.Rotate(terrain.WorldPoint(mode, p.Sub(anchor)), -rotation).Mul(1 / scale)
		m.Vertices = append(m.Vertices, ebiten.Vertex{DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: float32(cell%s.columns*s.pixels+2) + float32((q.X-s.min.X)/(s.max.X-s.min.X)*float64(s.pixels-4)), SrcY: float32(cell/s.columns*s.pixels+2) + float32((q.Y-s.min.Y)/(s.max.Y-s.min.Y)*float64(s.pixels-4)), ColorR: float32(tint), ColorG: float32(tint), ColorB: float32(tint), ColorA: 1})
	}
	for i := 1; i+1 < len(poly); i++ {
		m.Indices = append(m.Indices, offset, offset+uint32(i), offset+uint32(i+1))
	}
}

func newSpriteAttachment(kind AttachmentKind, atlas AtlasKind, cell int, anchor geom.V, rotation, scale float64, mode terrain.Orientation, tint float64) Attachment {
	m, poly := spriteMesh(atlas, cell, anchor, rotation, scale, mode, tint)
	lo, hi := geom.PolygonBounds(poly)
	return Attachment{Kind: kind, ID: terrain.CollisionVertexKey(anchor), Mesh: m, Min: lo, Max: hi, Anchors: []geom.V{anchor}, Footprint: [][]geom.V{poly}, Cell: cell, Anchor: anchor, Rotation: rotation, Scale: scale, Orientation: mode}
}

func prepareGrowthAttachments(grid terrain.RockGrid, boundary []terrain.RockEdge, growth RockGrowthOptions, owned bool) []Attachment {
	var out []Attachment
	var clearance growthClearance
	if growth.Hanging > 0 {
		clearance = newGrowthClearance(grid)
	}
	// Lichen lies beneath the rock, then moss covers faces, then edge trim.
	for _, e := range boundary {
		c := grid[e.Cell]
		outward := e.B.Sub(e.A).Norm().Perp().Mul(-1)
		if growth.Hanging <= 0 || terrain.WorldPoint(terrain.RockOrientation(c), outward).Y < .45 || e.B.Sub(e.A).Len() < .008 {
			continue
		}
		random := rockGrowthRandom(c, geom.LerpVector(e.A, e.B, .5), 0x6c696368656e)
		if random(0) > growth.Hanging {
			continue
		}
		t := .22 + .56*random(1)
		anchor := geom.LerpVector(e.A, e.B, t)
		support := anchor
		if wall, visible := rockWallPoints(c, e); visible {
			anchor = geom.LerpVector(wall[3], wall[2], t)
		}
		a := newSpriteAttachment(HangingAttachment, HangingAtlas, int(random(1)*64), anchor, 0, .75+.35*random(2), terrain.RockOrientation(c), 1)
		a.Underlay = true
		a.Anchors = []geom.V{support}
		if (owned && !inTerrainBand(a.Min, a.Max)) || !clearance.fits(a) {
			continue
		}
		out = append(out, a)
	}
	for _, c := range grid {
		mode := terrain.RockOrientation(c)
		upward := -terrain.WorldNormal(mode, c.Normal).Y
		random := rockGrowthRandom(c, c.Center, 0x6d6f7373)
		if growth.Moss <= 0 || upward < .10 || random(0) > growth.Moss*geom.Smoothstep(.08, .35, upward) {
			continue
		}
		size := math.Min(.014, math.Sqrt(math.Abs(geom.PolygonArea(c.Polygon)))*.42)
		if size < .001 {
			continue
		}
		a := newSpriteAttachment(SurfaceMoss, MossAtlas, int(random(1)*64), c.Center.Add(terrain.InternalPoint(mode, geom.V{Y: -size * .55})), 0, size/.014, mode, .48+.52*geom.Clamp(float64(c.Color.R)/165, 0, 1))
		if owned && !inTerrainBand(a.Min, a.Max) {
			continue
		}
		poly := a.Footprint[0]
		a.Mesh.Vertices = nil
		a.Mesh.Indices = nil
		for _, tri := range geom.Triangulate(c.Polygon) {
			piece := poly
			for j, index := range tri {
				p, q := c.Polygon[index], c.Polygon[tri[(j+1)%3]]
				n := q.Sub(p).Perp().Mul(-1).Norm()
				piece = geom.ClipHalfPlane(piece, n, p.Dot(n))
			}
			if geom.PolygonArea(piece) > 1e-15 {
				appendSpritePolygon(&a.Mesh, piece, MossAtlas, int(random(1)*64), a.Anchor, 0, size/.014, mode, .48+.52*geom.Clamp(float64(c.Color.R)/165, 0, 1))
			}
		}
		out = append(out, a)
	}
	var blades []Attachment
	supports := make([][]terrain.RockEdge, len(grid))
	for _, e := range boundary {
		supports[e.Cell] = append(supports[e.Cell], e)
	}
	for i, c := range grid {
		if len(supports[i]) == 0 {
			continue
		}
		patch, blade, ok := prepareGrassAttachments(c, growth, supports[i]...)
		if !ok || (owned && !inTerrainBand(patch.Min, patch.Max)) {
			continue
		}
		out = append(out, patch)
		blades = append(blades, blade)
	}
	out = append(out, blades...)
	return out
}

func prepareMushroomAttachments(groups []terrain.MushroomGroup, topology *terrain.RockTopology, mode terrain.Orientation) []Attachment {
	rock := mushroomRockSilhouette(topology)
	var out []Attachment
	for _, group := range groups {
		_, anchor, _, _, ok := terrain.MushroomPlacement(group)
		if !ok {
			var owned []terrain.Mushroom
			for _, m := range group.Mushrooms {
				if m.Anchor.Y >= 0 && m.Anchor.Y < terrain.SectionHeight {
					owned = append(owned, m)
				}
			}
			if len(owned) == 0 {
				continue
			}
			m := PrepareMushrooms([]terrain.MushroomGroup{{Mushrooms: owned}}, topology)
			b := triangleMeshesBounds([]TriangleMesh{m})
			a := Attachment{Kind: MushroomAttachment, Mesh: m, Min: geom.V{X: float64(b.Min.X) / RasterPixelsPerUnit, Y: float64(b.Min.Y)/RasterPixelsPerUnit + terrain.GenerationMinY}, Max: geom.V{X: float64(b.Max.X) / RasterPixelsPerUnit, Y: float64(b.Max.Y)/RasterPixelsPerUnit + terrain.GenerationMinY}}
			for _, m := range owned {
				a.Anchors = append(a.Anchors, m.Anchor)
				a.Footprint = append(a.Footprint, terrain.MushroomCapFootprint(m))
				for j := 1; j < len(m.Stem); j++ {
					n := m.Stem[j].Sub(m.Stem[j-1]).Norm().Perp().Mul(m.StemWidth() / 2)
					a.Footprint = append(a.Footprint, []geom.V{m.Stem[j-1].Add(n), m.Stem[j-1].Sub(n), m.Stem[j].Sub(n), m.Stem[j].Add(n)})
				}
			}
			out = append(out, a)
			continue
		}
		if anchor.Y < 0 || anchor.Y >= terrain.SectionHeight {
			continue
		}
		a := Attachment{Kind: MushroomAttachment, Mesh: TriangleMesh{Texture: MushroomStemAtlas, premultiplied: true}, Extra: TriangleMesh{Texture: MushroomCapAtlas, premultiplied: true}, Min: geom.V{X: math.Inf(1), Y: math.Inf(1)}, Max: geom.V{X: math.Inf(-1), Y: math.Inf(-1)}}
		for _, plant := range group.Mushrooms {
			sample, root, rotation, scale, ok := terrain.MushroomSpritePlacement(plant)
			if !ok {
				continue
			}
			stem, poly := spriteMesh(MushroomStemAtlas, sample, root, rotation, scale, mode, 1)
			stem.Vertices = nil
			stem.Indices = nil
			clips := rock.stalkClips(terrain.Mushroom{Stem: poly, CapWidth: .001})
			for _, piece := range clipMushroomStalk(poly, clips) {
				appendSpritePolygon(&stem, piece, MushroomStemAtlas, sample, root, rotation, scale, mode, 1)
			}
			caps, _ := spriteMesh(MushroomCapAtlas, sample, root, rotation, scale, mode, 1)
			appendAttachmentMesh(&a.Mesh, stem)
			appendAttachmentMesh(&a.Extra, caps)
			lo, hi := geom.PolygonBounds(poly)
			a.Min = geom.V{X: min(a.Min.X, lo.X), Y: min(a.Min.Y, lo.Y)}
			a.Max = geom.V{X: max(a.Max.X, hi.X), Y: max(a.Max.Y, hi.Y)}
		}
		for _, m := range group.Mushrooms {
			a.Anchors = append(a.Anchors, m.Anchor)
			a.Footprint = append(a.Footprint, terrain.MushroomCapFootprint(m))
			for j := 1; j < len(m.Stem); j++ {
				d := m.Stem[j].Sub(m.Stem[j-1]).Norm().Perp().Mul(m.StemWidth() / 2)
				a.Footprint = append(a.Footprint, []geom.V{m.Stem[j-1].Add(d), m.Stem[j-1].Sub(d), m.Stem[j].Sub(d), m.Stem[j].Add(d)})
			}
		}
		out = append(out, a)
	}
	stampAttachmentIDs(out, 0)
	return out
}

func appendAttachmentMesh(dst *TriangleMesh, source TriangleMesh) {
	offset := uint32(len(dst.Vertices))
	dst.Vertices = append(dst.Vertices, source.Vertices...)
	for _, id := range source.Indices {
		dst.Indices = append(dst.Indices, offset+id)
	}
}

func (a Attachment) Hit(cut terrain.RockCut, top float64) bool {
	if a.Dead || a.Kind == SurfaceMoss || a.Max.X <= cut.Min.X || a.Min.X >= cut.Max.X || a.Max.Y+top <= cut.Min.Y || a.Min.Y+top >= cut.Max.Y {
		return false
	}
	for _, p := range a.Anchors {
		if geom.InsidePolygon(p.Add(geom.V{Y: top}), cut.Poly) {
			return true
		}
	}
	if a.Kind == HangingAttachment {
		// Test sample triangles rather than transparent sprite margins.
		m := AtlasRecipes(a.Mesh.Texture)[a.Cell]
		for i := 0; i < len(m.Indices); i += 3 {
			var tri [3]geom.V
			for j := range tri {
				v := m.Vertices[m.Indices[i+j]]
				p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
				tri[j] = a.Anchor.Add(terrain.InternalPoint(a.Orientation, geom.Rotate(p, a.Rotation).Mul(a.Scale))).Add(geom.V{Y: top})
			}
			if geom.PolygonArea(tri[:]) < 0 {
				tri[0], tri[2] = tri[2], tri[0]
			}
			if geom.ConvexOverlap(tri[:], cut.Poly) {
				return true
			}
		}
		return false
	}
	for _, poly := range a.Footprint {
		if cut.Intersects(poly, top) {
			return true
		}
	}
	return false
}

func FilterAttachments(source []Attachment, cuts []terrain.RockCut, top float64) []Attachment {
	var out []Attachment
	for i, a := range source {
		for _, cut := range cuts {
			if a.Hit(cut, top) {
				if out == nil {
					out = append([]Attachment(nil), source...)
				}
				out[i].Dead = true
				break
			}
		}
	}
	if out == nil {
		return source
	}
	return out
}
func AttachmentsChanged(a, b []Attachment) bool {
	return len(a) != len(b) || (len(a) > 0 && &a[0] != &b[0])
}

func AttachmentIndex(source []Attachment) *geom.BoundsIndex {
	b := make([]geom.Bounds, len(source))
	for i, a := range source {
		b[i] = geom.Bounds{Min: a.Min, Max: a.Max}
	}
	return geom.NewBoundsIndex(b)
}

func FilterAttachmentsIndexed(source []Attachment, index *geom.BoundsIndex, cuts []terrain.RockCut, top float64) []Attachment {
	if index == nil {
		return FilterAttachments(source, cuts, top)
	}
	var out []Attachment
	for _, cut := range cuts {
		index.Query(geom.Bounds{Min: cut.Min.Sub(geom.V{Y: top}), Max: cut.Max.Sub(geom.V{Y: top})}, func(i int) bool {
			a := source[i]
			if out != nil {
				a = out[i]
			}
			if a.Hit(cut, top) {
				if out == nil {
					out = append([]Attachment(nil), source...)
				}
				out[i].Dead = true
			}
			return true
		})
	}
	if out == nil {
		return source
	}
	return out
}

func HasLiveAttachments(source []Attachment, grass bool) bool {
	for _, a := range source {
		if !a.Dead && (a.Kind == GrassAttachment) == grass && len(a.Mesh.Indices)+len(a.Extra.Indices) > 0 {
			return true
		}
	}
	return false
}

func attachmentMeshes(source []Attachment) []TriangleMesh {
	var out []TriangleMesh
	for _, a := range source {
		out = append(out, a.Mesh, a.Extra)
	}
	return out
}
func translateAttachments(source []Attachment, origin image.Point) {
	for i := range source {
		translateTriangleMeshes([]TriangleMesh{source[i].Mesh, source[i].Extra}, origin)
	}
}

func ScaleAttachments(source []Attachment, pixels int, origin image.Point) []Attachment {
	out := append([]Attachment(nil), source...)
	scale := float64(pixels) / RasterPixelsPerUnit
	for i := range out {
		out[i].Mesh = scaleTriangleMesh(out[i].Mesh, scale, origin)
		out[i].Extra = scaleTriangleMesh(out[i].Extra, scale, origin)
	}
	return out
}

func scaleCroppedAttachments(source []Attachment, bounds image.Rectangle, pixels int) []Attachment {
	out := append([]Attachment(nil), source...)
	for i := range out {
		meshes, _ := scaleVegetationMeshes([]TriangleMesh{out[i].Mesh, out[i].Extra}, bounds, pixels)
		out[i].Mesh, out[i].Extra = meshes[0], meshes[1]
	}
	return out
}

func prepareUnsampledMushrooms(groups []terrain.MushroomGroup, topology *terrain.RockTopology, owned bool) TriangleMesh {
	var legacy []terrain.MushroomGroup
	for _, g := range groups {
		if _, _, _, _, ok := terrain.MushroomPlacement(g); !ok {
			legacy = append(legacy, g)
		}
	}
	if owned {
		return PrepareOwnedMushrooms(legacy, topology)
	}
	return PrepareMushrooms(legacy, topology)
}

func prepareWebAttachments(webs []terrain.SpiderWeb, owned bool) []Attachment {
	var out []Attachment
	for _, w := range webs {
		m := prepareSpiderWebs([]terrain.SpiderWeb{w}, owned)
		if len(m.Indices) == 0 {
			continue
		}
		var points []geom.V
		var footprint [][]geom.V
		if w.Network != nil {
			points = w.Network.Nodes
			for _, t := range w.Network.Threads {
				a, b := points[t.A], points[t.B]
				n := b.Sub(a).Norm().Perp().Mul(.0006)
				footprint = append(footprint, []geom.V{a.Add(n), a.Sub(n), b.Sub(n), b.Add(n)})
			}
		}
		if len(points) == 0 {
			points = []geom.V{w.Corner, w.A, w.B}
			footprint = [][]geom.V{points}
		}
		lo, hi := geom.PolygonBounds(points)
		lo = lo.Sub(geom.V{X: .001, Y: .001})
		hi = hi.Add(geom.V{X: .001, Y: .001})
		anchors := points
		if w.Network != nil {
			anchors = w.Network.Nodes[:w.Network.Anchors]
		}
		out = append(out, Attachment{Kind: WebAttachment, ID: terrain.CollisionVertexKey(w.Corner), Mesh: m, Min: lo, Max: hi, Anchors: anchors, Footprint: footprint})
	}
	return out
}

// AttachmentBatch combines tiny instances while preserving texture and order.
// The returned next cursor includes skipped records and is safe to park.
func AttachmentBatch(source []Attachment, start int, grass, underlay bool, limit int) (TriangleMesh, int) {
	var batch TriangleMesh
	for start < len(source) {
		a := source[start]
		if a.Dead || (a.Kind == GrassAttachment) != grass || a.Underlay != underlay {
			start++
			continue
		}
		m := a.Mesh
		if len(batch.Indices) > 0 && (m.Texture != batch.Texture || len(batch.Indices)+len(m.Indices) > limit) {
			break
		}
		if len(batch.Indices) == 0 {
			batch.Texture, batch.premultiplied = m.Texture, m.premultiplied
		}
		offset := uint32(len(batch.Vertices))
		batch.Vertices = append(batch.Vertices, m.Vertices...)
		for _, i := range m.Indices {
			batch.Indices = append(batch.Indices, offset+i)
		}
		start++
		if len(a.Extra.Indices) > 0 {
			break
		} // paired mushroom layers draw individually
	}
	return batch, start
}

func prepareWallAttachments(grid terrain.RockGrid, edges []terrain.RockEdge, view View, owned bool) []Attachment {
	var out []Attachment
	for _, e := range edges {
		c := grid[e.Cell]
		p, visible := rockWallPoints(c, e)
		if !visible || (owned && !inTerrainBand(p[:]...)) {
			continue
		}
		outward := e.B.Sub(e.A).Norm().Perp().Mul(-1)
		normal := (geom.V3{X: outward.X, Y: outward.Y, Z: .1}).Norm()
		clr := rockViewColor(terrain.RockCell{Raised: true, Color: terrain.RockSurfaceColor(normal, c.Shadow*.65, c.Ambient*.55, terrain.RockOrientation(c))}, view)
		m := TriangleMesh{}
		m.Vertices, m.Indices = appendRockQuad(nil, nil, p, clr)
		for i := range m.Vertices {
			m.Vertices[i].SrcX, m.Vertices[i].SrcY = .5, .5
		}
		poly := append([]geom.V(nil), p[:]...)
		if geom.PolygonArea(poly) < 0 {
			reversePoints(poly)
		}
		lo, hi := geom.PolygonBounds(poly)
		out = append(out, Attachment{Kind: EdgeAttachment, Underlay: true, Mesh: m, Min: lo, Max: hi, Anchors: []geom.V{e.A, e.B}, Footprint: [][]geom.V{poly}})
	}
	return out
}

func stampAttachmentIDs(source []Attachment, top float64) {
	for i := range source {
		p := geom.LerpVector(source[i].Min, source[i].Max, .5)
		if len(source[i].Anchors) > 0 {
			p = source[i].Anchors[0]
		}
		source[i].ID = terrain.CollisionVertexKey(p.Add(geom.V{Y: top}))
	}
}
