package infinicave

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestGuideCacheIsBoundedIndependentAndOwnsItsOutput(t *testing.T) {
	cache := newGuideCache(42)
	for _, id := range []int64{0, 1, 3, 2, 100, 0} {
		got, want := cache.window(id), worldGuides(42, id)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("guide shapes depend on cache history at section %d", id)
		}
		if len(cache.proposals) > 7 || len(cache.resolved) > 5 {
			t.Fatal("guide cache grew beyond its window")
		}
		if len(got) > 0 {
			got[0].Pts[0].X += 100
			got[0].S[1] += 100
			if !reflect.DeepEqual(cache.window(id), want) {
				t.Fatal("caller changed cached guide data")
			}
		}
	}
}

func TestSectionGeometryIndependentOfWorkersCacheAndBufferReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)
	load := func(id int64) []Guide {
		if id != 0 {
			return nil
		}
		return []Guide{{Pts: []V{{.15, .4}, {.5, .35}, {.85, .45}}, Seed: 1234}}
	}
	for _, loader := range []GuideLoader{nil, load} {
		runtime.GOMAXPROCS(1)
		builder := newSectionBuilder(42, StudyNone, loader)
		original := builder.build(0)
		builder.build(1)
		// Evict guide data without generating distant terrain.
		builder.guides.window(100)
		runtime.GOMAXPROCS(4)
		if regenerated := builder.build(0); !reflect.DeepEqual(original, regenerated) {
			t.Fatal("same seed, section, and guides changed geometry after cache/buffer reuse")
		}
		if fresh := buildSectionMode(42, 0, StudyNone, loader); !reflect.DeepEqual(original, fresh) {
			t.Fatal("streaming and synchronous generation disagree")
		}
	}
}

func TestChamferFieldMatchesSingleObstacleDistances(t *testing.T) {
	d := takeVineField()
	for i := range d {
		d[i] = math.Inf(1)
	}
	const cx, cy = 173, 611
	d[cy*vineFieldWidth+cx] = 0
	chamferVineField(d)
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			dx, dy := abs(x-cx), abs(y-cy)
			want := float64(max(dx, dy)-min(dx, dy))*vineFieldStep + float64(min(dx, dy))*vineFieldStep*math.Sqrt2
			if math.Abs(d[y*vineFieldWidth+x]-want) > 1e-10 {
				t.Fatalf("distance at (%d,%d) = %g, want %g", x, y, d[y*vineFieldWidth+x], want)
			}
		}
	}
	putVineField(d)
	d = takeVineField()
	defer putVineField(d)
	for _, value := range d {
		if value != 0 {
			t.Fatal("pooled distance buffer contains old terrain")
		}
	}
}

func TestSpatialRockNeighborsMatchAllPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var sites []V
	for i := 0; i < 160; i++ {
		sites = append(sites, V{rng.Float64(), generationMinY + rng.Float64()*generationHeight})
	}
	grid := newRockGrid(sites, func(V) color.NRGBA { return color.NRGBA{A: 255} })
	want := make([][]int, len(grid))
	for i := range grid {
		for j := i + 1; j < len(grid); j++ {
			if mergeableBorder(grid[i].Polygon, grid[j].Polygon, nil) > mergeTolerance {
				want[i] = append(want[i], j)
				want[j] = append(want[j], i)
			}
		}
	}
	if got := rockNeighbors(grid); !reflect.DeepEqual(got, want) {
		t.Fatal("spatial search changed rock adjacency or neighbor order")
	}
}

func TestMeshBatchesPreservePrimitiveOrderAndFillModes(t *testing.T) {
	triangle := func(x float32, premultiplied bool, fill ebiten.FillRule) triangleMesh {
		return triangleMesh{vertices: []ebiten.Vertex{{DstX: x}, {DstX: x + 1}, {DstX: x + 2}}, indices: []uint32{0, 1, 2}, premultiplied: premultiplied, fillRule: fill}
	}
	meshes := []triangleMesh{triangle(1, false, ebiten.FillRuleFillAll), triangle(2, false, ebiten.FillRuleFillAll),
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
	expand := func(meshes []triangleMesh) []primitive {
		var result []primitive
		for _, m := range meshes {
			for _, i := range m.indices {
				result = append(result, primitive{m.vertices[i], m.premultiplied, m.fillRule})
			}
		}
		return result
	}
	if !reflect.DeepEqual(expand(meshes), expand(batched)) {
		t.Fatal("batching changed triangle order, attributes, or blending")
	}
}

func TestVegetationCropPreservesWorldPositionsAndPadding(t *testing.T) {
	mesh := triangleMesh{vertices: []ebiten.Vertex{{DstX: 123.25, DstY: 1040.5, SrcX: .5},
		{DstX: 140.75, DstY: 1050.25, SrcX: .5}, {DstX: 130, DstY: 1060, SrcX: .5}}, indices: []uint32{0, 1, 2}}
	original := append([]ebiten.Vertex(nil), mesh.vertices...)
	section := sectionMesh{vines: []triangleMesh{mesh}}
	finishSectionMesh(&section)
	want := image.Rect(120, 1037, 144, 1063)
	if section.vinesBounds != want || !section.foregroundVinesBounds.Empty() || !section.mushroomsBounds.Empty() {
		t.Fatal("crop dropped padding or allocated bounds for an empty layer")
	}
	for i, v := range section.vines[0].vertices {
		if v.DstX+float32(want.Min.X) != original[i].DstX || v.DstY+float32(want.Min.Y) != original[i].DstY || v.SrcX != original[i].SrcX {
			t.Fatal("cropping moved vegetation in world space or changed source samples")
		}
	}
}

func BenchmarkStreamSections(b *testing.B) {
	builder := newSectionBuilder(42, StudyNone, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data := builder.build(int64(i % 3))
		mesh := prepareSection(data, ViewShaded)
		mesh.geometry = prepareTerrainGeometry(data, 0)
		finishSectionMesh(&mesh)
	}
}

func BenchmarkBuildStreamSection(b *testing.B) {
	builder := newSectionBuilder(42, StudyNone, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		builder.build(int64(i % 3))
	}
}

func TestVineWorkspaceSurvivesGCWithoutLeakingOldSamples(t *testing.T) {
	var workspace vineWorkspace
	field := workspace.take()
	field[0], field[len(field)-1] = 123, 456
	address := &field[0]
	workspace.put(field)
	runtime.GC()
	reused := workspace.take()
	if &reused[0] != address || reused[0] != 0 || reused[len(reused)-1] != 0 {
		t.Fatal("worker workspace lost its buffer or retained the previous section")
	}
}
