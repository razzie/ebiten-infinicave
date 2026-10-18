package render

import (
	"image"
	"math"
	"slices"
	"unsafe"

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
	BackgroundGrassAttachment
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

func prepareGrowthAttachments(grid terrain.RockGrid, boundary []terrain.RockEdge, growth RockGrowthOptions, owned bool, trim ...Attachment) []Attachment {
	var out []Attachment
	var clearance growthClearance
	if growth.Hanging > 0 || growth.Moss > 0 {
		clearance = newGrowthClearance(grid)
	}
	// Exposed growth lies beneath rock. Moss covers faces, with guide bulbs
	// hanging over the surface before the structural edge trim.
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
		if p, ok := terrain.InsetRockAnchor(c.Polygon, support, outward.Mul(-1)); ok {
			a.Anchors = []geom.V{p}
		} else {
			continue
		}
		if (owned && !inTerrainBand(a.Min, a.Max)) || !clearance.fits(a) {
			continue
		}
		out = append(out, a)
	}
	out = append(out, prepareRockMoss(grid, boundary, clearance, growth.Moss, owned)...)
	out = append(out, prepareGuideBulbs(grid, boundary, growth, owned)...)
	var blades []Attachment
	supports := make([][]terrain.RockEdge, len(grid))
	for _, e := range boundary {
		supports[e.Cell] = append(supports[e.Cell], e)
	}
	for i, c := range grid {
		if len(supports[i]) == 0 {
			continue
		}
		first, firstBlade := len(out), len(blades)
		var anchors []geom.V
		for _, tuft := range prepareGrassColony(c, growth, supports[i]) {
			patch, blade := tuft[0], tuft[1]
			rear := rockGrowthRandom(c, patch.Anchor, 0x6772617373)(4) < .4
			if !rear {
				coverGrassTrim(&patch, trim)
			}
			anchors = append(anchors, patch.Anchors...)
			if owned && !inTerrainBand(patch.Min, patch.Max) {
				continue
			}
			// Each instance chooses depth independently of density and its
			// neighbors. Rear blades grow behind the rock without sod.
			if rear {
				blade.Kind, blade.Underlay = BackgroundGrassAttachment, true
				out = append(out, blade)
				continue
			}
			blade.Min = geom.V{X: min(blade.Min.X, patch.Min.X), Y: min(blade.Min.Y, patch.Min.Y)}
			blade.Max = geom.V{X: max(blade.Max.X, patch.Max.X), Y: max(blade.Max.Y, patch.Max.Y)}
			blade.Footprint = patch.Footprint
			blade.Anchors = patch.Anchors
			out = append(out, patch)
			blades = append(blades, blade)
		}
		// A colony is one supported entity, even when its tufts occupy
		// different drawing layers. Share the complete support set.
		for j := first; j < len(out); j++ {
			out[j].Anchors = anchors
		}
		for j := firstBlade; j < len(blades); j++ {
			blades[j].Anchors = anchors
		}
	}
	out = append(out, blades...)
	return out
}

// PreservesCutPixels identifies growth removed only by support damage.
// Surviving grass and lichen keep their complete artwork across terrain cuts.
func (a Attachment) PreservesCutPixels() bool {
	return a.Kind == GrassAttachment || a.Kind == BackgroundGrassAttachment || a.Kind == HangingAttachment
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
			a := Attachment{Kind: MushroomAttachment, Anchor: group.Mushrooms[0].Anchor, Mesh: m, Min: geom.V{X: float64(b.Min.X) / RasterPixelsPerUnit, Y: float64(b.Min.Y)/RasterPixelsPerUnit + terrain.GenerationMinY}, Max: geom.V{X: float64(b.Max.X) / RasterPixelsPerUnit, Y: float64(b.Max.Y)/RasterPixelsPerUnit + terrain.GenerationMinY}}
			for _, m := range owned {
				a.Anchors = append(a.Anchors, terrain.MushroomSupport(m))
			}
			out = append(out, a)
			continue
		}
		if anchor.Y < 0 || anchor.Y >= terrain.SectionHeight {
			continue
		}
		a := Attachment{Kind: MushroomAttachment, Anchor: group.Mushrooms[0].Anchor, Mesh: TriangleMesh{Texture: MushroomStemAtlas, premultiplied: true}, Extra: TriangleMesh{Texture: MushroomCapAtlas, premultiplied: true}, Min: geom.V{X: math.Inf(1), Y: math.Inf(1)}, Max: geom.V{X: math.Inf(-1), Y: math.Inf(-1)}}
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
			a.Anchors = append(a.Anchors, terrain.MushroomSupport(m))
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

// Hit only checks support points. Surface coverage is erased from textures.
func (a Attachment) Hit(cut terrain.RockCut, top float64) bool {
	if a.Dead || a.Kind == SurfaceMoss {
		return false
	}
	for _, p := range a.Anchors {
		p.Y += top
		if cut.ContainsAnchor(p) {
			return true
		}
	}
	return false
}

func FilterAttachments(source []Attachment, cuts []terrain.RockCut, top float64) []Attachment {
	out, _ := FilterAttachmentsIndexedWithChanges(source, nil, cuts, top)
	return out
}

func AttachmentsChanged(a, b []Attachment) bool {
	return len(a) != len(b) || (len(a) > 0 && &a[0] != &b[0])
}

// AttachmentSupportIndex indexes individual support points, never sprite
// bounds. Paired records sharing anchors (grass bases/blades and rock teeth)
// share one hit test. The immutable index survives deletions and raster scaling.
type AttachmentSupportIndex struct {
	index  *geom.BoundsIndex
	visual *geom.BoundsIndex
	ids    map[[2]int64][]int
	points []attachmentSupport
}

type attachmentSupport struct {
	point  geom.V
	owners []int
}

func (t *AttachmentSupportIndex) RetainedBytes() int {
	if t == nil {
		return 0
	}
	size := int(unsafe.Sizeof(*t)) + t.index.RetainedBytes() + t.visual.RetainedBytes() + cap(t.points)*int(unsafe.Sizeof(attachmentSupport{}))
	var previous *int
	for _, p := range t.points {
		if len(p.owners) > 0 && &p.owners[0] != previous {
			size += cap(p.owners) * int(unsafe.Sizeof(int(0)))
			previous = &p.owners[0]
		}
	}
	for _, owners := range t.ids {
		size += int(unsafe.Sizeof([2]int64{})) + int(unsafe.Sizeof(owners)) + cap(owners)*int(unsafe.Sizeof(int(0)))
	}
	return size
}

func AttachmentIndex(source []Attachment) *AttachmentSupportIndex {
	t := &AttachmentSupportIndex{}
	visual := make([]geom.Bounds, len(source))
	groups := make(map[*geom.V][]int)
	var order []*geom.V
	for i, a := range source {
		if a.Kind == MushroomAttachment || a.Kind == WebAttachment {
			if t.ids == nil {
				t.ids = make(map[[2]int64][]int)
			}
			t.ids[a.ID] = append(t.ids[a.ID], i)
		}
		visual[i] = geom.Bounds{Min: a.Min, Max: a.Max}
		if a.Kind == SurfaceMoss || len(a.Anchors) == 0 {
			continue
		}
		key := &a.Anchors[0]
		if groups[key] == nil {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}
	var bounds []geom.Bounds
	for _, key := range order {
		owners := groups[key]
		for _, p := range source[owners[0]].Anchors {
			t.points = append(t.points, attachmentSupport{p, owners})
			bounds = append(bounds, geom.Bounds{Min: p, Max: p})
		}
	}
	t.index = geom.NewBoundsIndex(bounds)
	t.visual = geom.NewBoundsIndex(visual)
	return t
}

// AttachmentCandidates selects live recipes for a dirty region in section
// coordinates, preserving original blend order. It also accepts a prefix of
// the recipes while an upload is in progress.
func AttachmentCandidates(source []Attachment, index *AttachmentSupportIndex, bounds geom.Bounds) []int {
	var candidates []int
	visit := func(i int) bool {
		if i < len(source) {
			a := source[i]
			if !a.Dead && (geom.Bounds{Min: a.Min, Max: a.Max}).Overlaps(bounds) {
				candidates = append(candidates, i)
			}
		}
		return true
	}
	if index == nil {
		for i := range source {
			visit(i)
		}
	} else {
		index.visual.Query(bounds, visit)
		slices.Sort(candidates)
	}
	return candidates
}

func FilterAttachmentsIndexed(source []Attachment, index *AttachmentSupportIndex, cuts []terrain.RockCut, top float64) []Attachment {
	out, _ := FilterAttachmentsIndexedWithChanges(source, index, cuts, top)
	return out
}

// FilterAttachmentsByID consumes the terrain support query's result. Runtime
// mushrooms and webs share that query with CPU state instead of redetecting hits.
func FilterAttachmentsByID(source []Attachment, index *AttachmentSupportIndex, ids [][2]int64) ([]Attachment, []int) {
	var out []Attachment
	var removed []int
	kill := func(i int) {
		if source[i].Dead || (out != nil && out[i].Dead) {
			return
		}
		if out == nil {
			out = append([]Attachment(nil), source...)
		}
		out[i].Dead = true
		removed = append(removed, i)
	}
	for _, id := range ids {
		if index != nil {
			for _, i := range index.ids[id] {
				kill(i)
			}
		} else {
			for i, a := range source {
				if a.ID == id {
					kill(i)
				}
			}
		}
	}
	if out == nil {
		return source, nil
	}
	return out, removed
}

// Returns newly dead record indices along with the immutable replacement.
// Callers use the same result for dirty tiles and upload liveness updates.
func FilterAttachmentsIndexedWithChanges(source []Attachment, index *AttachmentSupportIndex, cuts []terrain.RockCut, top float64) ([]Attachment, []int) {
	var out []Attachment
	var removed []int
	current := func(i int) Attachment {
		if out != nil {
			return out[i]
		}
		return source[i]
	}
	kill := func(i int) {
		if current(i).Dead {
			return
		}
		if out == nil {
			out = append([]Attachment(nil), source...)
		}
		out[i].Dead = true
		removed = append(removed, i)
	}
	for _, cut := range cuts {
		if index == nil {
			for i := range source {
				if current(i).Hit(cut, top) {
					kill(i)
				}
			}
			continue
		}
		index.index.Query(geom.Bounds{Min: cut.Min.Sub(geom.V{Y: top}), Max: cut.Max.Sub(geom.V{Y: top})}, func(i int) bool {
			p := index.points[i]
			// Layer snapshots can mask some members of a shared colony.
			// Its remaining members still respond to every colony anchor.
			live := false
			for _, owner := range p.owners {
				live = live || !current(owner).Dead
			}
			if !live {
				return true
			}
			point := p.point
			point.Y += top
			if cut.ContainsAnchor(point) {
				for _, owner := range p.owners {
					kill(owner)
				}
			}
			return true
		})
	}
	if out == nil {
		return source, nil
	}
	return out, removed
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
		// Masked spans retain indices for edits; dead recipes never draw and
		// need no destination-coordinate copy of their vertex buffers.
		if out[i].Dead {
			continue
		}
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
		points := []geom.V{w.Corner, w.A, w.B}
		if w.Network != nil {
			points = w.Network.Nodes
		}
		lo, hi := geom.PolygonBounds(points)
		lo = lo.Sub(geom.V{X: .001, Y: .001})
		hi = hi.Add(geom.V{X: .001, Y: .001})
		out = append(out, Attachment{Kind: WebAttachment, Anchor: w.Corner, ID: terrain.CollisionVertexKey(w.Corner), Mesh: m, Min: lo, Max: hi, Anchors: terrain.SpiderWebSupports(w)})
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
		out = append(out, Attachment{Kind: EdgeAttachment, Underlay: true, Mesh: m, Min: lo, Max: hi, Footprint: [][]geom.V{poly}})
	}
	return out
}

func stampAttachmentIDs(source []Attachment, top float64) {
	for i := range source {
		p := geom.LerpVector(source[i].Min, source[i].Max, .5)
		if source[i].Kind == MushroomAttachment || source[i].Kind == WebAttachment {
			p = source[i].Anchor
		} else if len(source[i].Anchors) > 0 {
			p = source[i].Anchors[0]
		}
		source[i].ID = terrain.CollisionVertexKey(p.Add(geom.V{Y: top}))
	}
}
