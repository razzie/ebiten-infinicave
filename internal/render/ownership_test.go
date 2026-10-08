package render

import (
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestMushroomOwnershipUsesHalfOpenAnchorBand(t *testing.T) {
	mushrooms := []terrain.Mushroom{
		{Anchor: geom.V{X: .5, Y: -.001}, CapCenter: geom.V{X: .5, Y: 0}, CapWidth: .01, CapHeight: .005},
		{Anchor: geom.V{X: .5, Y: 0}, CapCenter: geom.V{X: .5, Y: -.01}, CapWidth: .01, CapHeight: .005},
		{Anchor: geom.V{X: .5, Y: .99}, CapCenter: geom.V{X: .5, Y: .98}, CapWidth: .01, CapHeight: .005},
		{Anchor: geom.V{X: .5, Y: 1}, CapCenter: geom.V{X: .5, Y: .99}, CapWidth: .01, CapHeight: .005},
	}
	groups := []terrain.MushroomGroup{{Mushrooms: mushrooms}}
	got := PrepareOwnedMushrooms(groups)
	want := PrepareMushrooms([]terrain.MushroomGroup{{Mushrooms: mushrooms[1:3]}})
	if !reflect.DeepEqual(got, want) || len(groups[0].Mushrooms) != 4 {
		t.Fatal("ownership altered padded input or included another anchor band")
	}
}

func TestSupersampledBatchBoundsVertexSubmissionsAndKeepsMaterials(t *testing.T) {
	mesh := TriangleMesh{Vertices: make([]ebiten.Vertex, 100000), Indices: []uint32{99999, 4, 0, 0, 4, 55}, premultiplied: true}
	for _, i := range mesh.Indices {
		mesh.Vertices[i] = ebiten.Vertex{DstX: float32(i) / 100, DstY: 3, SrcX: 7, SrcY: 11, ColorA: .5}
	}
	original := append([]ebiten.Vertex(nil), mesh.Vertices...)
	for start := 0; start < len(mesh.Indices); start += 3 {
		batch := SupersampledBatch(mesh, start, start+3)
		if len(batch.Vertices) > len(batch.Indices) || !batch.premultiplied {
			t.Fatal("batch sent unrelated vertices or lost its blend mode")
		}
		for j, index := range batch.Indices {
			got := batch.Vertices[index]
			want := original[mesh.Indices[start+j]]
			want.DstX *= 2
			want.DstY *= 2
			if got != want {
				t.Fatal("batch changed triangle order or material coordinates")
			}
		}
	}
	if !reflect.DeepEqual(original, mesh.Vertices) {
		t.Fatal("batch modified the immutable source mesh")
	}
}
