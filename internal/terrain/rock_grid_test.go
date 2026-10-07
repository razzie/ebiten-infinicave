package terrain

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestForegroundGridStaysInsideHorizontalScreenInset(t *testing.T) {
	grid := RockGrid{
		{Center: geom.V{X: 0.01, Y: 0.05}, Polygon: []geom.V{{X: 0, Y: 0}, {X: 0.04, Y: 0}, {X: 0.04, Y: 0.1}, {X: 0, Y: 0.1}}, Raised: true},
		{Center: geom.V{X: generationWidth - 0.01, Y: 0.05}, Polygon: []geom.V{{X: generationWidth - 0.04, Y: 0}, {X: generationWidth, Y: 0}, {X: generationWidth, Y: 0.1}, {X: generationWidth - 0.04, Y: 0.1}}, Raised: true},
	}
	original := make([][]geom.V, len(grid))
	for i := range grid {
		original[i] = append([]geom.V(nil), grid[i].Polygon...)
	}

	clipped := InsetForegroundGrid(grid)
	if len(clipped) != len(grid) {
		t.Fatalf("inset retained %d cells, want %d", len(clipped), len(grid))
	}
	for _, cell := range clipped {
		for _, p := range cell.Polygon {
			if p.X < foregroundScreenInset-1e-9 || p.X > generationWidth-foregroundScreenInset+1e-9 {
				t.Fatalf("foreground vertex reaches horizontal screen edge: %v", p)
			}
		}
	}
	for i := range grid {
		if !reflect.DeepEqual(grid[i].Polygon, original[i]) {
			t.Fatal("render inset mutated the source geometry")
		}
	}
}
