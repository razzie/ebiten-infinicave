package infinicave

import (
	"fmt"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Fixed world coordinates and generated bake recipes expose costs hidden by the
// small CPU-only benchmark and the single-rectangle cached-image fixture.
func BenchmarkCarveGeneratedImages(b *testing.B) {
	prepared := make([]render.SectionMesh, 3)
	data := make([]terrain.SectionData, len(prepared))
	for i := range prepared {
		data[i] = terrain.BuildSection(42, int64(i))
		prepared[i] = render.PrepareSection(data[i], ViewShaded, render.RockGrowthOptions{Moss: 1, Grass: 1, Hanging: 1})
		prepared[i].Geometry = terrain.PrepareTerrainGeometry(data[i], 0)
		render.FinishSectionMesh(&prepared[i])
	}
	shader, err := ebiten.NewShader(render.MaterialShaderSource)
	if err != nil {
		b.Fatal(err)
	}
	defer shader.Deallocate()
	for _, radius := range []float64{.05, .2, .4} {
		for _, overlap := range []bool{false, true} {
			b.Run(fmt.Sprintf("radius=%.2f/overlap=%v", radius, overlap), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 768}
					g := &Scene{world: w, material: shader, view: ViewShaded}
					for id, recipe := range prepared {
						mesh := recipe
						w.sections[int64(id)] = &worldSection{foreground: ebiten.NewImage(768, 768), grass: ebiten.NewImage(768, 768), mesh: &mesh, geometry: recipe.Geometry, pixels: 768}
					}
					w.queryIndex()
					if overlap {
						if _, err := g.CarveCircle(V{X: .45, Y: -.5}, radius); err != nil {
							b.Fatal(err)
						}
					}
					b.StartTimer()
					if _, err := g.CarveCircle(V{X: .5, Y: -.5}, radius); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					w.close()
				}
			})
		}
	}
}
