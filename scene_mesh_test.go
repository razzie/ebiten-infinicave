package infinicave

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

func checkMesh(t *testing.T, mesh triangleMesh) {
	t.Helper()
	if len(mesh.indices)%3 != 0 {
		t.Fatal("mesh has incomplete triangles")
	}
	for _, index := range mesh.indices {
		if int(index) >= len(mesh.vertices) {
			t.Fatal("mesh index exceeds vertex count")
		}
	}
	for _, v := range mesh.vertices {
		if math.IsNaN(float64(v.DstX)) || math.IsNaN(float64(v.DstY)) || math.IsInf(float64(v.DstX), 0) || math.IsInf(float64(v.DstY), 0) {
			t.Fatal("mesh has nonfinite geometry")
		}
	}
}

func TestPreparedOutlinesPreserveBlackPockets(t *testing.T) {
	grid := RockGrid{
		{Center: V{0.005, 0.005}, Polygon: []V{{0, 0}, {0.01, 0}, {0.01, 0.01}, {0, 0.01}}, Normal: V3{Z: 1}, Color: color.NRGBA{A: 255}},
		{Center: V{0.015, 0.005}, Polygon: []V{{0.01, 0}, {0.02, 0}, {0.02, 0.01}, {0.01, 0.01}}, Normal: V3{Z: 1}, Color: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
	}
	mesh := prepareGrid(grid, ViewShaded)
	if len(mesh.faces.indices) == 0 || len(mesh.outlines) == 0 {
		t.Fatal("visible rock lost its faces or outlines")
	}
	checkMesh(t, mesh.faces)
	for _, outline := range mesh.outlines {
		checkMesh(t, outline)
		if outline.fillRule != ebiten.FillRuleNonZero || !outline.premultiplied {
			t.Fatal("outline intersections or transparency use the wrong blending")
		}
		for _, v := range outline.vertices {
			if v.DstX < 9.99 {
				t.Fatal("outline entered a black pocket")
			}
			if math.Abs(float64(v.ColorR-v.ColorA*18/255)) > 1e-7 {
				t.Fatal("outline color is not premultiplied")
			}
		}
	}
	for _, view := range []View{ViewClay, ViewHeight, ViewNormals, ViewShadows} {
		if diagnostic := prepareGrid(grid, view); len(diagnostic.outlines) != 0 || len(diagnostic.faces.indices) == 0 {
			t.Fatalf("view %s lost faces or gained outlines", view)
		}
	}
}

func TestLargePreparedOutlineGroups(t *testing.T) {
	// More than 65536 stroke vertices sharing one opacity must be split
	// before calling the vector tessellator, then use valid 32-bit indices.
	grid := make(RockGrid, 5000)
	for i := range grid {
		x, y := float64(i%100)*.008, float64(i/100)*.008
		grid[i] = RockCell{Center: V{x + 0.002, y + 0.002}, Polygon: []V{{x, y}, {x + 0.004, y}, {x + 0.004, y + 0.004}, {x, y + 0.004}}, Normal: V3{Z: 1}, Color: color.NRGBA{R: 60, G: 60, B: 60, A: 255}}
	}
	mesh := prepareGrid(grid, ViewShaded)
	total := 0
	for _, outline := range mesh.outlines {
		checkMesh(t, outline)
		total += len(outline.vertices)
		if len(outline.vertices) > 1<<16 {
			t.Fatal("outline batch exceeded the tessellator's index limit")
		}
	}
	if total <= 1<<16 || len(mesh.outlines) < 2 {
		t.Fatal("test did not exercise large outline splitting")
	}
	if !reflect.DeepEqual(mesh, prepareGrid(grid, ViewShaded)) {
		t.Fatal("prepared geometry changed on revisiting a section")
	}
}

func TestPrepareSectionMatchesSerialLayers(t *testing.T) {
	data := sectionData{
		id: 17,
		background: RockGrid{{Center: V{0.05, 0.05}, Polygon: []V{{0, 0}, {0.1, 0}, {0.1, 0.1}, {0, 0.1}},
			Normal: V3{Z: 1}, Color: color.NRGBA{R: 40, G: 40, B: 40, A: 255}}},
		foreground: RockGrid{{Center: V{0.04, 0.04}, Polygon: []V{{0, 0.02}, {0.08, 0.02}, {0.08, 0.08}, {0, 0.08}},
			Normal: V3{Z: 1}, Color: color.NRGBA{R: 90, G: 80, B: 70, A: 255}, Raised: true, Z: .030, Shadow: 1, Ambient: 1}},
		vines: []Vine{
			{Parent: -1, Points: []VinePoint{{P: V{0.1, 0.1}, Radius: 0.003}, {P: V{0.11, 0.11}, Radius: 0.002}, {P: V{0.12, 0.12}}}},
			{Parent: 0, Joint: 1, Depth: 1, Points: []VinePoint{{P: V{0.11, 0.11}, Radius: 0.002}, {P: V{0.12, 0.11}, Radius: 0.001}, {P: V{0.13, 0.11}}}},
		},
		foregroundVines: []Vine{{Parent: -1, Foreground: true, Points: []VinePoint{
			{P: V{0.03, 0.04}}, {P: V{0.04, 0.05}, Radius: 0.003}, {P: V{0.05, 0.06}},
		}}},
		mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{{
			Stem: []V{{0.05, 0.04}, {0.052, 0.03}, {0.05, 0.02}}, CapCenter: V{0.05, 0.018}, CapWidth: 0.01, CapHeight: 0.004,
			Color: mushroomColors[0],
		}}}},
	}
	previous := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	for _, view := range []View{ViewShaded, ViewClay, ViewHeight, ViewNormals, ViewShadows} {
		want := sectionMesh{id: data.id, background: prepareGrid(data.background, view), foreground: prepareGrid(data.foreground, view)}
		if view == ViewShaded {
			want.vines = prepareVines(data.vines)
			want.foregroundVines = prepareForegroundVines(data.foregroundVines)
			want.mushrooms = prepareMushrooms(data.mushrooms)
		}
		for _, procs := range []int{1, 2, 4, 8} {
			t.Run(fmt.Sprintf("%s/procs%d", view, procs), func(t *testing.T) {
				runtime.GOMAXPROCS(procs)
				if got := prepareSection(data, view); !reflect.DeepEqual(got, want) {
					t.Fatal("concurrent preparation changed layer geometry or ordering")
				}
			})
		}
	}
}

func TestWorldWorkerPreparesAllLayers(t *testing.T) {
	w := newWorld(42, StudyNone, ViewShaded, 0, nil)
	defer w.close()
	w.request(0)
	select {
	case mesh := <-w.results:
		if mesh.geometry == nil || len(mesh.geometry.collision.Polygons) == 0 {
			t.Fatal("worker omitted collision geometry")
		}
		if mesh.id != 0 || len(mesh.background.faces.indices) == 0 || len(mesh.foreground.faces.indices) == 0 || len(mesh.vines) == 0 || len(mesh.foregroundVines) == 0 || len(mesh.mushrooms.indices) == 0 {
			t.Fatal("worker returned incomplete section geometry")
		}
		checkMesh(t, mesh.background.faces)
		checkMesh(t, mesh.foreground.faces)
		checkMesh(t, mesh.mushrooms)
		for _, layer := range [][]triangleMesh{mesh.background.outlines, mesh.foreground.outlines, mesh.vines, mesh.foregroundVines} {
			for _, m := range layer {
				checkMesh(t, m)
			}
		}
		if len(w.sections) != 0 || w.upload != nil || w.white != nil {
			t.Fatal("worker touched game-thread scene or GPU state")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("background section preparation stalled")
	}
}

func TestSectionUploadsPublishTerrainBeforeVegetation(t *testing.T) {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1), results: make(chan sectionMesh, 1), done: make(chan struct{}), working: true}
	defer w.close()
	// Empty batches isolate the scheduler; nonempty crop bounds exercise image ownership.
	bounds := image.Rect(10, 20, 30, 40)
	w.results <- sectionMesh{id: 0, background: gridMesh{outlines: make([]triangleMesh, uploadDrawsPerTick*2+1)}, vines: make([]triangleMesh, uploadDrawsPerTick*2+1), foregroundVines: make([]triangleMesh, uploadDrawsPerTick*2+1), vinesBounds: bounds, foregroundVinesBounds: bounds, mushroomsBounds: bounds}
	g := &Scene{world: w, view: ViewShaded}
	w.receive(g)
	if w.upload == nil || w.working {
		t.Fatal("result was not handed off to the upload queue")
	}
	// Neighbor is complete, so readiness below depends only on the pending upload.
	w.sections[1] = &worldSection{}
	published := false
	for tick := 0; tick < 30; tick++ {
		stage, next := w.upload.stage, w.upload.next
		w.receive(g)
		section := w.sections[0]
		if stage < 4 && section != nil {
			t.Fatal("incomplete terrain became visible")
		}
		if stage == 4 {
			if section == nil || section.terrain == nil || section.foreground == nil || !section.vegetationPending {
				t.Fatal("complete terrain was not published early")
			}
			if w.upload.img != nil || w.upload.foreground != nil {
				t.Fatal("upload retained ownership of published images")
			}
			if w.ensure(-.8, .8, 0) {
				t.Fatal("pending vegetation reported ready")
			}
			published = true
			// Camera jumps must not deallocate the section still being completed.
			w.prune(-100.8, .8, 0)
			if w.sections[0] != section {
				t.Fatal("pruning evicted an active upload")
			}
			w.sections[1] = &worldSection{}
		}
		if w.upload == nil {
			if !published || stage != 8 || section == nil || section.vegetationPending {
				t.Fatal("upload did not finish both publication stages")
			}
			if section.vines == nil || section.foregroundVines == nil || section.mushrooms == nil {
				t.Fatal("completed vegetation is missing a layer")
			}
			if section.vines.Bounds() != image.Rect(0, 0, 20, 20) || section.vinesBounds != bounds {
				t.Fatal("vegetation lost its crop or origin")
			}
			if !w.ensure(-.8, .8, 0) {
				t.Fatal("completed viewport is not ready")
			}
			return
		}
		if stage == w.upload.stage && w.upload.next-next > uploadDrawsPerTick {
			t.Fatal("tick submitted too many mesh uploads")
		}
		if (stage == 1 || stage == 5 || stage == 7) && next == 0 && w.upload.stage != stage {
			t.Fatal("large mesh list was uploaded in one tick")
		}
	}
	t.Fatal("section upload did not complete")
}

func BenchmarkPrepareSection(b *testing.B) {
	data := buildSection(42, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prepareSection(data, ViewShaded)
	}
}
