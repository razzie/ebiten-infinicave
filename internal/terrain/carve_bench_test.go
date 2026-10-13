package terrain

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Measure decoration handling separately from rock subtraction, query identity
// and GPU submissions, so unrelated costs cannot hide vine and anchor scans.
func BenchmarkCarveDecorations(b *testing.B) {
	for _, name := range []string{"vines", "support_miss"} {
		b.Run(name, func(b *testing.B) {
			data := SectionData{}
			if name == "vines" {
				for i := 0; i < 64; i++ {
					vine := Vine{Parent: -1}
					for j := 0; j <= 128; j++ {
						vine.Points = append(vine.Points, VinePoint{P: geom.V{X: .02 + .96*float64(i)/63, Y: float64(j) / 128}, Radius: .002})
					}
					data.Vines = append(data.Vines, vine)
				}
				data.ForegroundVines = data.Vines
			} else {
				for i := 0; i < 1024; i++ {
					root := geom.V{X: float64(i) / 1024, Y: .1}
					cap := root.Add(geom.V{Y: -.02})
					data.Mushrooms = append(data.Mushrooms, MushroomGroup{Mushrooms: []Mushroom{{Anchor: root, Stem: []geom.V{root, cap}, CapCenter: cap, CapWidth: .01, CapHeight: .006}}})
				}
			}
			geometry := PrepareTerrainGeometry(data, 0)
			cut, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .5, Y: -.5}, Radius: .025})
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				cut.Plants(geometry)
			}
		})
	}
}
