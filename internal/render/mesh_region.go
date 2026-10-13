package render

import (
	"image"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
	return source
}
