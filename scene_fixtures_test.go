package infinicave

// Helpers for scene, query, carving, and streaming integration tests.

import (
	"image/color"
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func rockAt(t *testing.T, scene *Scene, p V) QueryResult {
	t.Helper()
	return mustQuery(t, scene, Ray{Origin: p}, QueryOptions{Targets: TargetRock})
}

func testGroundedMushroom(x, y float64) Mushroom {
	return Mushroom{
		Anchor: V{X: x, Y: y}, RootDirection: V{X: 0, Y: -1},
		Stem:      []V{{X: x, Y: y + terrain.MushroomSink}, {X: x, Y: y - .01}, {X: x, Y: y - .02}},
		CapCenter: V{X: x, Y: y - .025}, CapWidth: .012, CapHeight: .005,
		Color: terrain.MushroomColors[0],
	}
}

func queryScene(data ...terrain.SectionData) *Scene {
	w := &world{sections: make(map[int64]*worldSection)}
	for _, d := range data {
		w.sections[d.ID] = &worldSection{geometry: terrain.PrepareTerrainGeometry(d, .01)}
	}
	return &Scene{world: w}
}

func mustQuery(t *testing.T, scene *Scene, ray Ray, options QueryOptions) QueryResult {
	t.Helper()
	result, err := scene.Query(ray, options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func resolutionTestMesh(id int64) render.SectionMesh {
	data := terrain.SectionData{ID: id,
		Background: RockGrid{{Center: V{X: .5, Y: .5}, Polygon: []V{{X: 0, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 2}, {X: 0, Y: 2}}, Color: color.NRGBA{R: 40, A: 255}, Normal: V3{Z: 1}}},
		Foreground: RockGrid{{Center: V{X: .5, Y: .5}, Polygon: []V{{X: .2, Y: .2}, {X: .8, Y: .2}, {X: .8, Y: .8}, {X: .2, Y: .8}}, Color: color.NRGBA{R: 100, A: 255}, Normal: V3{Z: 1}, Raised: true}},
	}
	mesh := render.PrepareSection(data, ViewClay)
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	render.FinishSectionMesh(&mesh)
	return mesh
}

func resolutionTestScene(t *testing.T) *Scene {
	t.Helper()
	g, err := NewScene(Config{View: ViewClay})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	// No worker: any accidental regeneration is observable in the jobs queue.
	g.world = &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan render.SectionMesh, 1), done: make(chan struct{})}
	for id := int64(0); id < 5; id++ {
		mesh := resolutionTestMesh(id)
		g.world.sections[id] = &worldSection{mesh: &mesh, geometry: mesh.Geometry,
			terrain: render.NewBackgroundImageAt(1000), foreground: render.NewSectionImageAt(1, 1000), pixels: 1000}
	}
	t.Cleanup(g.Close)
	return g
}

func finishResolutionRefresh(t *testing.T, g *Scene, viewport Viewport) {
	t.Helper()
	for tick := 0; tick < 40; tick++ {
		if g.Update(viewport) {
			return
		}
	}
	t.Fatal("visible sections never finished refreshing")
}

// Fixed rock shapes keep collision and diagnostic tests reproducible while
// exercising the same section loader used for authored game terrain.
func testLedgeSection(id int64) SectionContent {
	return testRockSection(id, []V{{X: 0.11, Y: 0.42}, {X: 0.5, Y: 0.365}, {X: 0.89, Y: 0.31}})
}

func testCurlSection(id int64) SectionContent {
	return testRockSection(id, []V{{X: 0.08, Y: 0.65}, {X: 0.38, Y: 0.52}, {X: 0.65, Y: 0.41}, {X: 0.71, Y: 0.26}, {X: 0.57, Y: 0.19}, {X: 0.45, Y: 0.29}, {X: 0.52, Y: 0.37}})
}

func testRockSection(id int64, knots []V) SectionContent {
	guide := terrain.RidgedGuide(terrain.SplineGuide(knots, 1), 42)
	guide.Seed = terrain.SectionSeed(42, id)
	return SectionContent{Guides: []Guide{guide}}
}

func checkMesh(t *testing.T, mesh render.TriangleMesh) {
	t.Helper()
	if len(mesh.Indices)%3 != 0 {
		t.Fatal("mesh has incomplete triangles")
	}
	for _, index := range mesh.Indices {
		if int(index) >= len(mesh.Vertices) {
			t.Fatal("mesh index exceeds vertex count")
		}
	}
	for _, v := range mesh.Vertices {
		if math.IsNaN(float64(v.DstX)) || math.IsNaN(float64(v.DstY)) || math.IsInf(float64(v.DstX), 0) || math.IsInf(float64(v.DstY), 0) {
			t.Fatal("mesh has nonfinite geometry")
		}
	}
}

func terrainRock(poly []V) RockCell {
	return RockCell{Polygon: poly, Center: geom.PolygonCenter(poly), Raised: true, Color: color.NRGBA{A: 255}}
}

func terrainRect(x, y, width, height float64) RockCell {
	return terrainRock([]V{{X: x, Y: y}, {X: x + width, Y: y}, {X: x + width, Y: y + height}, {X: x, Y: y + height}})
}
