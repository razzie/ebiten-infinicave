package render

import (
	"image"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestMeshBatchesPreservePrimitiveOrderAndFillModes(t *testing.T) {
	triangle := func(x float32, premultiplied bool, fill ebiten.FillRule) TriangleMesh {
		return TriangleMesh{Vertices: []ebiten.Vertex{{DstX: x}, {DstX: x + 1}, {DstX: x + 2}}, Indices: []uint32{0, 1, 2}, premultiplied: premultiplied, fillRule: fill}
	}
	meshes := []TriangleMesh{triangle(1, false, ebiten.FillRuleFillAll), triangle(2, false, ebiten.FillRuleFillAll),
		triangle(3, true, ebiten.FillRuleFillAll), triangle(4, true, ebiten.FillRuleNonZero), triangle(5, true, ebiten.FillRuleNonZero),
		triangle(6, false, ebiten.FillRuleFillAll)}
	batched := batchTriangleMeshes(meshes)
	if len(batched) != 5 {
		t.Fatalf("incorrect batching across alpha/fill modes: %d batches", len(batched))
	}
	type primitive struct {
		vertex ebiten.Vertex
		alpha  bool
		fill   ebiten.FillRule
	}
	expand := func(meshes []TriangleMesh) []primitive {
		var result []primitive
		for _, m := range meshes {
			for _, i := range m.Indices {
				result = append(result, primitive{m.Vertices[i], m.premultiplied, m.fillRule})
			}
		}
		return result
	}
	if !reflect.DeepEqual(expand(meshes), expand(batched)) {
		t.Fatal("batching changed triangle order, attributes, or blending")
	}
}

func TestVegetationCropPreservesWorldPositionsAndPadding(t *testing.T) {
	mesh := TriangleMesh{Vertices: []ebiten.Vertex{{DstX: 123.25, DstY: 1040.5, SrcX: .5},
		{DstX: 140.75, DstY: 1050.25, SrcX: .5}, {DstX: 130, DstY: 1060, SrcX: .5}}, Indices: []uint32{0, 1, 2}}
	original := append([]ebiten.Vertex(nil), mesh.Vertices...)
	section := SectionMesh{Vines: []TriangleMesh{mesh}}
	FinishSectionMesh(&section)
	want := image.Rect(120, 1037, 144, 1063)
	if section.VinesBounds != want || !section.ForegroundVinesBounds.Empty() || !section.MushroomsBounds.Empty() {
		t.Fatal("crop dropped padding or allocated bounds for an empty layer")
	}
	for i, v := range section.Vines[0].Vertices {
		if v.DstX+float32(want.Min.X) != original[i].DstX || v.DstY+float32(want.Min.Y) != original[i].DstY || v.SrcX != original[i].SrcX {
			t.Fatal("cropping moved vegetation in world space or changed source samples")
		}
	}
}
