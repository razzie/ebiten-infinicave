package infinicave

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Use generated terrain and an already populated query cache, as in the viewer.
// Setup and section generation are excluded from the measured edit latency.
func BenchmarkCarve(b *testing.B) {
	benchmarkCarve(b, false)
}

func BenchmarkCarveTerrain(b *testing.B) {
	benchmarkCarve(b, true)
}

func benchmarkCarve(b *testing.B, terrainOnly bool) {
	data := make([]terrain.SectionData, 3)
	for i := range data {
		data[i] = terrain.BuildSection(42, int64(i))
	}
	for _, name := range []string{"circle", "segment", "overlap"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
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
			durations := make([]time.Duration, 0, b.N)
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				scene := &Scene{world: &world{sections: make(map[int64]*worldSection)}}
				for id, section := range original.world.sections {
					scene.world.sections[id] = &worldSection{geometry: section.geometry}
				}
				scene.world.queryIndex()
				if name == "overlap" {
					if _, err := scene.CarveCircle(center.Sub(V{X: .025}), .05); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()
				start := time.Now()
				carve := scene.Carve
				if terrainOnly {
					carve = scene.CarveTerrain
				}
				var hole Hole
				if name == "circle" || name == "overlap" {
					hole = Hole{Shape: HoleCircle, Center: center, Radius: .05}
				} else {
					hole = Hole{Shape: HoleSegment, Start: center.Sub(V{X: .2}), End: center.Add(V{X: .2}), Width: .02}
				}
				result, err := carve(hole)
				elapsed := time.Since(start)
				b.StopTimer()
				durations = append(durations, elapsed)
				if err != nil || len(result.SectionIDs) == 0 {
					b.Fatalf("carve missed generated rock: %+v, %v", result, err)
				}
			}
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			b.ReportMetric(float64(durations[len(durations)/2]), "median-ns")
			b.ReportMetric(float64(durations[min(len(durations)-1, (len(durations)*95+99)/100-1)]), "p95-ns")
		})
	}
}

// Compare CPU replay and draw submission for the same generated rock and cuts.
// GPU completion is excluded; rendered output is covered by the pixel tests.
func BenchmarkCarveRockReplay(b *testing.B) {
	data := terrain.BuildSection(42, 0)
	mesh := render.PrepareSection(data, ViewShaded)
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	render.FinishSectionMesh(&mesh)
	g, err := NewScene(Config{DisableBackground: true})
	if err != nil {
		b.Fatal(err)
	}
	defer g.Close()
	g.SetRenderWidth(768)
	g.world.startUpload(mesh)
	for i := 0; i < 2000 && g.world.upload != nil; i++ {
		g.world.receive(g)
	}
	if g.world.upload != nil {
		b.Fatal("upload stalled")
	}
	for _, x := range []float64{.45, .5} {
		if _, err := g.CarveCircle(V{X: x, Y: -.5}, .085); err != nil {
			b.Fatal(err)
		}
		g.flushCarveImages()
	}
	s := *g.world.sections[0]
	s.mesh = s.rockSource
	if s.carveBase == nil {
		b.Fatal("missing baked rock cache")
	}
	dirty := geom.Bounds{Min: V{X: .35, Y: -.6}, Max: V{X: .6, Y: -.4}}
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%v", cached), func(b *testing.B) {
			section := s
			if !cached {
				section.carveBase = nil
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				g.replayGridRegion(&section, 0, dirty, false)
			}
		})
	}
}

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
