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

func TestBackgroundVineCropIncludesFadingSideMargins(t *testing.T) {
	mesh := TriangleMesh{Vertices: []ebiten.Vertex{{DstX: -200, DstY: 1040}, {DstX: 1200, DstY: 1050}, {DstX: -100, DstY: 1060}}, Indices: []uint32{0, 1, 2}}
	original := append([]ebiten.Vertex(nil), mesh.Vertices...)
	section := SectionMesh{Vines: []TriangleMesh{mesh}}
	FinishSectionMesh(&section)
	if section.VinesBounds.Min.X != -203 || section.VinesBounds.Max.X != 1203 {
		t.Fatal("background vine crop discarded the fading border regions")
	}
	for i, v := range section.Vines[0].Vertices {
		if v.DstX+float32(section.VinesBounds.Min.X) != original[i].DstX || v.DstY+float32(section.VinesBounds.Min.Y) != original[i].DstY {
			t.Fatal("border vine crop changed its world coordinates")
		}
	}
}

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
