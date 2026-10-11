package terrain

import (
	"context"
	"testing"
)

func BenchmarkFindWebCorners(b *testing.B) {
	build := NewSectionBuilder(42, nil).Begin(context.Background(), 0)
	topology := build.Data().ForegroundTopology
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		findWebCorners(topology.Grid, topology.Boundary)
	}
}

func BenchmarkResolveSpiderWebNetworks(b *testing.B) {
	builder := NewSectionBuilder(42, nil)
	build := builder.Begin(context.Background(), 0)
	build.Materialize(context.Background())
	topology := build.Data().ForegroundTopology
	appearance := PrepareRockAppearance(topology).Appearance
	corners := findWebCorners(topology.Grid, topology.Boundary)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolveSpiderWebs(appearance, corners)
	}
}
