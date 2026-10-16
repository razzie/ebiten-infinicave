package render

import (
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestGrassAtlasKeepsBroadBladeSilhouettes(t *testing.T) {
	for cell, mesh := range AtlasRecipes(GrassAtlas) {
		minY, maxY, area := math.Inf(1), math.Inf(-1), 0.0
		for _, v := range mesh.Vertices {
			minY, maxY = min(minY, float64(v.DstY)), max(maxY, float64(v.DstY))
		}
		for i := 0; i < len(mesh.Indices); i += 3 {
			a, b, c := mesh.Vertices[mesh.Indices[i]], mesh.Vertices[mesh.Indices[i+1]], mesh.Vertices[mesh.Indices[i+2]]
			area += math.Abs(float64(b.DstX-a.DstX)*float64(c.DstY-a.DstY)-float64(b.DstY-a.DstY)*float64(c.DstX-a.DstX)) * .5
		}
		if area/math.Pow(maxY-minY, 2) < .09 {
			t.Fatalf("sample %d is too thin to read as a broad pointed blade", cell)
		}
	}
}

func TestMossAtlasKeepsMottledDominantColorColonies(t *testing.T) {
	accentSamples := 0
	for cell, mesh := range AtlasRecipes(MossAtlas) {
		minX, maxX, minY, maxY := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		preferred, accent, rooted := 0.0, 0.0, 0.0
		shades := make(map[[3]float32]bool)
		for _, v := range mesh.Vertices {
			minX, maxX = min(minX, float64(v.DstX)), max(maxX, float64(v.DstX))
			minY, maxY = min(minY, float64(v.DstY)), max(maxY, float64(v.DstY))
			shades[[3]float32{v.ColorR, v.ColorG, v.ColorB}] = true
		}
		for i := 0; i < len(mesh.Indices); i += 3 {
			a, b, c := mesh.Vertices[mesh.Indices[i]], mesh.Vertices[mesh.Indices[i+1]], mesh.Vertices[mesh.Indices[i+2]]
			area := math.Abs(float64(b.DstX-a.DstX)*float64(c.DstY-a.DstY)-float64(b.DstY-a.DstY)*float64(c.DstX-a.DstX)) * .5
			depth := float64(a.DstY+b.DstY+c.DstY)/(3*RasterPixelsPerUnit) + terrain.GenerationMinY
			if depth > -.002 && depth < .024 {
				rooted += area
			}
			if (a.ColorG > a.ColorR) == (cell < 32) {
				preferred += area
			} else {
				accent += area
			}
		}
		if maxX-minX < maxY-minY || maxY-minY < .02*RasterPixelsPerUnit {
			t.Fatalf("moss sample %d lacks a broad draping silhouette", cell)
		}
		if rooted/(preferred+accent) < .65 {
			t.Fatalf("moss sample %d does not gather near its supporting ledge", cell)
		}
		if len(shades) < 12 || preferred/(preferred+accent) < .88 {
			t.Fatalf("moss sample %d lacks subtle dominant-color variation", cell)
		}
		if accent > 0 {
			accentSamples++
		}
	}
	if accentSamples == 0 {
		t.Fatalf("contrasting moss accents should be occasional: %d/64 samples", accentSamples)
	}
}

func TestAtlasBatchesKeepSamplesIsolated(t *testing.T) {
	for kind := GrassAtlas; kind < atlasKinds; kind++ {
		s := specForAtlas(kind)
		batches := AtlasBatches(kind)
		if len(batches) >= s.columns*s.rows/4 {
			t.Fatal("atlas still uploads individual samples")
		}
		t.Logf("atlas %d: %d batches for %d samples", kind, len(batches), s.columns*s.rows)
		for _, m := range batches {
			if len(m.Vertices) > maxBatchVertices {
				t.Fatal("atlas exceeds a bounded upload batch")
			}
			for i := 0; i < len(m.Indices); i += 3 {
				first := m.Vertices[m.Indices[i]]
				x, y := int(first.DstX)/s.pixels, int(first.DstY)/s.pixels
				for _, index := range m.Indices[i : i+3] {
					v := m.Vertices[index]
					if v.SrcX != .5 || v.SrcY != .5 || int(v.DstX)/s.pixels != x || int(v.DstY)/s.pixels != y || v.DstX < 1 || v.DstY < 1 || v.DstX > float32(s.columns*s.pixels-1) || v.DstY > float32(s.rows*s.pixels-1) {
						t.Fatal("atlas triangle bleeds into a neighboring sample")
					}
				}
			}
		}
	}
}
