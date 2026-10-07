package infinicave

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Use generated terrain and an already populated query cache, as in the viewer.
// Setup and section generation are excluded from the measured edit latency.
func BenchmarkCarve(b *testing.B) {
	data := make([]terrain.SectionData, 3)
	for i := range data {
		data[i] = terrain.BuildSection(42, int64(i))
	}
	for _, name := range []string{"circle", "segment"} {
		b.Run(name, func(b *testing.B) {
			original := queryScene(data...)
			original.collisionTolerance = 0
			var center V
			for _, face := range original.world.sections[0].geometry.Faces {
				p := geom.PolygonCenter(face.Poly).Add(V{Y: -1})
				if p.X > .2 && p.X < .8 && p.Y > -.8 && p.Y < -.2 {
					center = p
					break
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				scene := &Scene{world: &world{sections: make(map[int64]*worldSection)}}
				for id, section := range original.world.sections {
					scene.world.sections[id] = &worldSection{geometry: section.geometry}
				}
				scene.world.queryIndex()
				b.StartTimer()
				var result CarveResult
				var err error
				if name == "circle" {
					result, err = scene.CarveCircle(center, .05)
				} else {
					result, err = scene.CarveSegment(center.Sub(V{X: .2}), center.Add(V{X: .2}), .02)
				}
				if err != nil || len(result.SectionIDs) == 0 {
					b.Fatalf("carve missed generated rock: %+v, %v", result, err)
				}
			}
		})
	}
}
