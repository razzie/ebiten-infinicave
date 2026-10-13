package render

import (
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

var atlasBatches [atlasKinds]struct {
	sync.Once
	meshes []TriangleMesh
}

// AtlasBatches prepares page coordinates on the generation worker. Clipping
// each sample before batching preserves cell isolation and transparent gutters.
func AtlasBatches(kind AtlasKind) []TriangleMesh {
	r := &atlasBatches[kind]
	r.Do(func() {
		s := specForAtlas(kind)
		var meshes []TriangleMesh
		for cell, recipe := range AtlasRecipes(kind) {
			m := TriangleMesh{premultiplied: recipe.premultiplied}
			x, y := float32(cell%s.columns*s.pixels), float32(cell/s.columns*s.pixels)
			vertices := append([]ebiten.Vertex(nil), recipe.Vertices...)
			inside := true
			for j := range vertices {
				v := &vertices[j]
				v.SrcX, v.SrcY = .5, .5
				v.DstX = x + 2 + float32((float64(v.DstX)/RasterPixelsPerUnit-s.min.X)/(s.max.X-s.min.X)*float64(s.pixels-4))
				v.DstY = y + 2 + float32((float64(v.DstY)/RasterPixelsPerUnit+terrain.GenerationMinY-s.min.Y)/(s.max.Y-s.min.Y)*float64(s.pixels-4))
				if v.DstX < x+1 || v.DstX > x+float32(s.pixels-1) || v.DstY < y+1 || v.DstY > y+float32(s.pixels-1) {
					inside = false
				}
			}
			if inside {
				m.Vertices, m.Indices = vertices, recipe.Indices
				meshes = append(meshes, m)
				continue
			}
			for i := 0; i < len(recipe.Indices); i += 3 {
				poly := []ebiten.Vertex{vertices[recipe.Indices[i]], vertices[recipe.Indices[i+1]], vertices[recipe.Indices[i+2]]}
				for axis := 0; axis < 4; axis++ {
					limit := x + 1
					if axis%2 == 1 {
						limit = y + 1
					}
					if axis >= 2 {
						limit += float32(s.pixels - 2)
					}
					poly = clipAtlasPolygon(poly, axis, limit)
				}
				offset := uint32(len(m.Vertices))
				m.Vertices = append(m.Vertices, poly...)
				for j := 1; j+1 < len(poly); j++ {
					m.Indices = append(m.Indices, offset, offset+uint32(j), offset+uint32(j+1))
				}
			}
			meshes = append(meshes, m)
		}
		r.meshes = batchTriangleMeshes(meshes)
	})
	return r.meshes
}

func clipAtlasPolygon(poly []ebiten.Vertex, axis int, limit float32) []ebiten.Vertex {
	if len(poly) == 0 {
		return nil
	}
	coordinate := func(v ebiten.Vertex) float32 {
		if axis%2 == 0 {
			return v.DstX
		}
		return v.DstY
	}
	inside := func(d float32) bool {
		if axis < 2 {
			return d >= 0
		}
		return d <= 0
	}
	var out []ebiten.Vertex
	a := poly[len(poly)-1]
	da := coordinate(a) - limit
	for _, b := range poly {
		db := coordinate(b) - limit
		if inside(da) != inside(db) {
			t := da / (da - db)
			v := a
			v.DstX += (b.DstX - a.DstX) * t
			v.DstY += (b.DstY - a.DstY) * t
			v.ColorR += (b.ColorR - a.ColorR) * t
			v.ColorG += (b.ColorG - a.ColorG) * t
			v.ColorB += (b.ColorB - a.ColorB) * t
			v.ColorA += (b.ColorA - a.ColorA) * t
			out = append(out, v)
		}
		if inside(db) {
			out = append(out, b)
		}
		a, da = b, db
	}
	return out
}
