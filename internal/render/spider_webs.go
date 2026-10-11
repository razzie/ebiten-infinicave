package render

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func prepareSpiderWebs(webs []terrain.SpiderWeb, owned bool) TriangleMesh {
	mesh := TriangleMesh{premultiplied: true}
	thread := func(a, b geom.V, opacity, width float64) {
		if owned && !inTerrainBand(a, b) {
			return
		}
		n := b.Sub(a).Perp().Norm().Mul(width / (2 * RasterPixelsPerUnit))
		first := uint32(len(mesh.Vertices))
		for _, p := range [...]geom.V{a.Add(n), b.Add(n), b.Sub(n), a.Sub(n)} {
			alpha := float32(opacity)
			mesh.Vertices = append(mesh.Vertices, ebiten.Vertex{
				DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
				SrcX: .5, SrcY: .5,
				ColorR: .85 * alpha, ColorG: .88 * alpha, ColorB: .83 * alpha, ColorA: alpha,
			})
		}
		mesh.Indices = append(mesh.Indices, first, first+1, first+2, first, first+2, first+3)
	}
	for _, w := range webs {
		if w.Network == nil {
			continue
		}
		for _, silk := range w.Network.Threads {
			opacity, width := .38+.06*w.Variation, .38
			switch silk.Kind {
			case terrain.WebSupport:
				opacity, width = .48+.05*w.Variation, .58
			case terrain.WebFrame:
				opacity, width = .43+.05*w.Variation, .50
			case terrain.WebRadial:
				opacity, width = .42+.05*w.Variation, .46
			case terrain.WebHub:
				opacity, width = .32, .35
			}
			thread(w.Network.Nodes[silk.A], w.Network.Nodes[silk.B], opacity, width)
		}
	}
	return mesh
}

// PrepareOwnedSpiderWebs emits threads for the section's owned rock band.
// FinishSectionMesh clips its image bounds to that band before publication.
func PrepareOwnedSpiderWebs(webs []terrain.SpiderWeb) TriangleMesh {
	return prepareSpiderWebs(webs, true)
}
