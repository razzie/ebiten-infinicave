package terrain

import (
	"context"
	"testing"
)

func BenchmarkGenerationStages(b *testing.B) {
	ctx := context.Background()
	b.Run("geometry", func(b *testing.B) {
		builder := NewSectionBuilder(42, nil)
		b.ReportAllocs()
		for b.Loop() {
			if builder.Begin(ctx, 0) == nil {
				b.Fatal("generation canceled")
			}
		}
	})
	b.Run("material", func(b *testing.B) {
		builder := NewSectionBuilder(42, nil)
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			s := builder.Begin(ctx, 0)
			b.StartTimer()
			if !s.Materialize(ctx) {
				b.Fatal("material canceled")
			}
		}
	})
	b.Run("decoration", func(b *testing.B) {
		builder := NewSectionBuilder(42, nil)
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			s := builder.Begin(ctx, 0)
			s.Materialize(ctx)
			b.StartTimer()
			if !builder.Decorate(ctx, s) {
				b.Fatal("decoration canceled")
			}
		}
	})
}
