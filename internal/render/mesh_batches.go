package render

import (
	"image"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

const maxBatchVertices = 16384

// Merge consecutive ordinary triangle lists, preserving their primitive order
// and alpha mode. Nonzero/even-odd fills must retain separate coverage masks.
func batchTriangleMeshes(meshes []TriangleMesh) []TriangleMesh {
	var result []TriangleMesh
	for _, mesh := range meshes {
		if len(mesh.Indices) == 0 {
			continue
		}
		if mesh.fillRule != ebiten.FillRuleFillAll || len(mesh.Vertices) > maxBatchVertices {
			result = append(result, mesh)
			continue
		}
		if len(result) == 0 || result[len(result)-1].fillRule != mesh.fillRule ||
			result[len(result)-1].Texture != mesh.Texture ||
			result[len(result)-1].premultiplied != mesh.premultiplied ||
			len(result[len(result)-1].Vertices)+len(mesh.Vertices) > maxBatchVertices {
			result = append(result, TriangleMesh{Texture: mesh.Texture, fillRule: mesh.fillRule, premultiplied: mesh.premultiplied})
		}
		batch := &result[len(result)-1]
		offset := uint32(len(batch.Vertices))
		batch.Vertices = append(batch.Vertices, mesh.Vertices...)
		for _, index := range mesh.Indices {
			batch.Indices = append(batch.Indices, index+offset)
		}
	}
	return result
}

// Bounds live in the original padded-window pixels. Integer origins preserve
// raster alignment, and padding leaves room for antialiasing at the edges.
func triangleMeshesBounds(meshes []TriangleMesh) image.Rectangle {
	loX, loY, hiX, hiY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, mesh := range meshes {
		for _, index := range mesh.Indices {
			v := mesh.Vertices[index]
			loX, loY = min(loX, float64(v.DstX)), min(loY, float64(v.DstY))
			hiX, hiY = max(hiX, float64(v.DstX)), max(hiY, float64(v.DstY))
		}
	}
	if math.IsInf(loX, 1) {
		return image.Rectangle{}
	}
	const padding = 3
	return image.Rect(int(math.Floor(loX))-padding, int(math.Floor(loY))-padding,
		int(math.Ceil(hiX))+padding, int(math.Ceil(hiY))+padding).
		Intersect(image.Rect(0, 0, RasterPixelsPerUnit, terrain.GenerationHeight*RasterPixelsPerUnit))
}

func translateTriangleMeshes(meshes []TriangleMesh, origin image.Point) {
	for _, mesh := range meshes {
		for i := range mesh.Vertices {
			mesh.Vertices[i].DstX -= float32(origin.X)
			mesh.Vertices[i].DstY -= float32(origin.Y)
		}
	}
}

// Called only on freshly prepared worker-owned meshes. Once cropped, the
// meshes and their bounds move together to the game thread.
func FinishSectionMesh(mesh *SectionMesh) {
	mesh.VinesBounds = triangleMeshesBounds(mesh.Vines)
	mesh.ForegroundVinesBounds = triangleMeshesBounds(mesh.ForegroundVines)
	// Webs use the same owned band as the rock image, so neighboring padded
	// snapshots cannot draw the same translucent thread twice.
	mesh.SpiderWebsBounds = triangleMeshesBounds([]TriangleMesh{mesh.SpiderWebs}).Intersect(
		image.Rect(0, RasterPixelsPerUnit, RasterPixelsPerUnit, 2*RasterPixelsPerUnit))
	mesh.MushroomsBounds = triangleMeshesBounds(append([]TriangleMesh{mesh.Mushrooms}, attachmentMeshes(mesh.Plants)...))
	mesh.Vines = batchTriangleMeshes(mesh.Vines)
	mesh.ForegroundVines = batchTriangleMeshes(mesh.ForegroundVines)
	translateTriangleMeshes(mesh.Vines, mesh.VinesBounds.Min)
	translateTriangleMeshes(mesh.ForegroundVines, mesh.ForegroundVinesBounds.Min)
	translateTriangleMeshes([]TriangleMesh{mesh.Mushrooms}, mesh.MushroomsBounds.Min)
	translateTriangleMeshes([]TriangleMesh{mesh.SpiderWebs}, mesh.SpiderWebsBounds.Min)
	translateAttachments(mesh.Plants, mesh.MushroomsBounds.Min)
	translateAttachments(mesh.Webs, mesh.SpiderWebsBounds.Min)
	for i := range mesh.Vines {
		mesh.Vines[i].Index = MeshIndex(mesh.Vines[i])
	}
	for i := range mesh.ForegroundVines {
		mesh.ForegroundVines[i].Index = MeshIndex(mesh.ForegroundVines[i])
	}
}

// RegionMesh copies only triangles contributing to a dirty rectangle. Its
// material coordinates and primitive order stay unchanged; caller owns it.
func RegionMesh(source TriangleMesh, rect image.Rectangle) TriangleMesh {
	m := source
	m.Vertices = nil
	m.Indices = nil
	m.Index = nil
	remap := make(map[uint32]uint32)
	var candidates []int
	if source.Index != nil {
		source.Index.Query(geom.Bounds{Min: geom.V{X: float64(rect.Min.X), Y: float64(rect.Min.Y)}, Max: geom.V{X: float64(rect.Max.X), Y: float64(rect.Max.Y)}}, func(i int) bool { candidates = append(candidates, i*3); return true })
		sort.Ints(candidates)
	} else {
		for i := 0; i < len(source.Indices); i += 3 {
			candidates = append(candidates, i)
		}
	}
	for _, i := range candidates {
		ids := source.Indices[i : i+3]
		a, b, c := source.Vertices[ids[0]], source.Vertices[ids[1]], source.Vertices[ids[2]]
		if max(a.DstX, b.DstX, c.DstX) < float32(rect.Min.X) || min(a.DstX, b.DstX, c.DstX) > float32(rect.Max.X) || max(a.DstY, b.DstY, c.DstY) < float32(rect.Min.Y) || min(a.DstY, b.DstY, c.DstY) > float32(rect.Max.Y) {
			continue
		}
		for _, id := range ids {
			v, ok := remap[id]
			if !ok {
				v = uint32(len(m.Vertices))
				remap[id] = v
				m.Vertices = append(m.Vertices, source.Vertices[id])
			}
			m.Indices = append(m.Indices, v)
		}
	}
	return m
}

// AttachmentRegionBatches keeps the selected recipes' blend order and batches
// compatible triangle lists, as the initial upload does. Replaying a dense
// region should not require a separate antialiased draw for every tiny instance.
func AttachmentRegionBatches(source []Attachment, candidates []int, rect image.Rectangle, grass, underlay bool) []TriangleMesh {
	var meshes []TriangleMesh
	for _, i := range candidates {
		a := source[i]
		if a.Dead || a.Underlay != underlay || (a.Kind == GrassAttachment) != grass {
			continue
		}
		m := RegionMesh(a.Mesh, rect)
		if len(m.Indices) > 0 {
			meshes = append(meshes, m)
		}
	}
	return batchTriangleMeshes(meshes)
}

func MeshIndex(mesh TriangleMesh) *geom.BoundsIndex {
	b := make([]geom.Bounds, len(mesh.Indices)/3)
	for i := range b {
		a, c, d := mesh.Vertices[mesh.Indices[i*3]], mesh.Vertices[mesh.Indices[i*3+1]], mesh.Vertices[mesh.Indices[i*3+2]]
		b[i] = geom.Bounds{Min: geom.V{X: float64(min(a.DstX, c.DstX, d.DstX)), Y: float64(min(a.DstY, c.DstY, d.DstY))}, Max: geom.V{X: float64(max(a.DstX, c.DstX, d.DstX)), Y: float64(max(a.DstY, c.DstY, d.DstY))}}
	}
	return geom.NewBoundsIndex(b)
}

func ScaleRegionMesh(source TriangleMesh, pixels int, offset image.Point) TriangleMesh {
	return scaleTriangleMesh(source, float64(pixels)/RasterPixelsPerUnit, offset)
}

func TranslateMesh(source TriangleMesh, dx, dy float32) TriangleMesh {
	source.Vertices = append([]ebiten.Vertex(nil), source.Vertices...)
	for i := range source.Vertices {
		source.Vertices[i].DstX += dx
		source.Vertices[i].DstY += dy
	}
	if source.MaterialFrame.Pixels != 0 {
		source.MaterialFrame.OriginX -= float64(dx) / source.MaterialFrame.Samples
		source.MaterialFrame.OriginY -= float64(dy) / source.MaterialFrame.Samples
	}
	return source
}
