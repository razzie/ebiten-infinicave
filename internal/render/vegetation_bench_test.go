package render

import (
	"testing"
	"unsafe"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func BenchmarkPrepareVegetation(b *testing.B) {
	data := terrain.BuildSection(42, 0)
	b.ReportAllocs()
	b.ResetTimer()
	var mesh SectionMesh
	for b.Loop() {
		mesh = PrepareVegetation(data, ViewShaded)
	}
	bytes := 0
	count := func(m TriangleMesh) { bytes += len(m.Vertices)*int(unsafe.Sizeof(ebiten.Vertex{})) + len(m.Indices)*4 }
	for _, m := range mesh.Vines {
		count(m)
	}
	for _, m := range mesh.ForegroundVines {
		count(m)
	}
	count(mesh.Mushrooms)
	count(mesh.SpiderWebs)
	for _, a := range mesh.Plants {
		count(a.Mesh)
		count(a.Extra)
	}
	for _, a := range mesh.Webs {
		count(a.Mesh)
	}
	b.ReportMetric(float64(bytes), "mesh-bytes")
}

// Growth is prepared with the foreground, outside PrepareVegetation. Include
// its support metadata and spatial indexes in the retained recipe estimate.
func BenchmarkPrepareForeground(b *testing.B) {
	data := terrain.BuildSection(42, 0)
	PrepareForeground(data, ViewShaded) // warm the shared immutable atlas samples
	b.ReportAllocs()
	b.ResetTimer()
	var mesh SectionMesh
	for b.Loop() {
		mesh = PrepareForeground(data, ViewShaded)
	}
	payload, recipe := 0, 0
	seen := make(map[unsafe.Pointer]bool)
	storage := func(p unsafe.Pointer, bytes int) {
		if p == nil || seen[p] {
			return
		}
		seen[p] = true
		recipe += bytes
	}
	count := func(m TriangleMesh) {
		payload += len(m.Vertices)*int(unsafe.Sizeof(ebiten.Vertex{})) + len(m.Indices)*4
		storage(unsafe.Pointer(unsafe.SliceData(m.Vertices)), cap(m.Vertices)*int(unsafe.Sizeof(ebiten.Vertex{})))
		storage(unsafe.Pointer(unsafe.SliceData(m.Indices)), cap(m.Indices)*4)
		storage(unsafe.Pointer(m.Index), m.Index.RetainedBytes())
	}
	count(mesh.Foreground.Faces)
	for _, m := range mesh.Foreground.Outlines {
		count(m)
	}
	recipe += cap(mesh.Foreground.Growth)*int(unsafe.Sizeof(Attachment{})) + mesh.Foreground.GrowthIndex.RetainedBytes()
	for _, a := range mesh.Foreground.Growth {
		count(a.Mesh)
		count(a.Extra)
		storage(unsafe.Pointer(unsafe.SliceData(a.Anchors)), cap(a.Anchors)*int(unsafe.Sizeof(geom.V{})))
		storage(unsafe.Pointer(unsafe.SliceData(a.Footprint)), cap(a.Footprint)*int(unsafe.Sizeof([]geom.V{})))
		for _, p := range a.Footprint {
			storage(unsafe.Pointer(unsafe.SliceData(p)), cap(p)*int(unsafe.Sizeof(geom.V{})))
		}
	}
	b.ReportMetric(float64(payload), "mesh-bytes")
	b.ReportMetric(float64(recipe), "recipe-bytes")
}
