package render

import (
	"image"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestAttachmentRegionBatchesPreserveBlendOrderAndTextures(t *testing.T) {
	attachment := func(id float32, texture AtlasKind) Attachment {
		return Attachment{Mesh: TriangleMesh{Texture: texture, Indices: []uint32{0, 1, 2}, Vertices: []ebiten.Vertex{
			{DstX: 1, DstY: 1, ColorR: id}, {DstX: 3, DstY: 1, ColorR: id}, {DstX: 1, DstY: 3, ColorR: id},
		}}}
	}
	source := []Attachment{attachment(1, NoAtlas), attachment(2, NoAtlas), attachment(3, GrassAtlas), attachment(4, NoAtlas), attachment(5, NoAtlas), attachment(6, NoAtlas)}
	source[4].Dead = true
	source[5].Underlay = true
	source = append(source, attachment(7, NoAtlas))
	for i := range source[6].Mesh.Vertices {
		source[6].Mesh.Vertices[i].DstX += 100
	}
	batches := AttachmentRegionBatches(source, []int{0, 1, 2, 3, 4, 5, 6}, image.Rect(0, 0, 10, 10), false, false)
	if len(batches) != 3 || batches[0].Texture != NoAtlas || batches[1].Texture != GrassAtlas || batches[2].Texture != NoAtlas {
		t.Fatal("replay failed to combine compatible meshes or reordered textures")
	}
	var order []float32
	for _, m := range batches {
		checkMesh(t, m)
		for i := 0; i < len(m.Indices); i += 3 {
			order = append(order, m.Vertices[m.Indices[i]].ColorR)
		}
	}
	if !reflect.DeepEqual(order, []float32{1, 2, 3, 4}) {
		t.Fatalf("surviving triangle blend order = %v", order)
	}
}
