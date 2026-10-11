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
