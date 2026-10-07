package render

import (
	"fmt"
	"image/color"
	"math"
	"reflect"
	"runtime"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func BenchmarkStreamSections(b *testing.B) {
	builder := terrain.NewSectionBuilder(42, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data := builder.Build(int64(i % 3))
		mesh := PrepareSection(data, ViewShaded)
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		FinishSectionMesh(&mesh)
	}
}

func TestPreparedOutlinesPreserveBlackPockets(t *testing.T) {
	grid := terrain.RockGrid{
		{Center: geom.V{X: 0.005, Y: 0.005}, Polygon: []geom.V{{X: 0, Y: 0}, {X: 0.01, Y: 0}, {X: 0.01, Y: 0.01}, {X: 0, Y: 0.01}}, Normal: geom.V3{Z: 1}, Color: color.NRGBA{A: 255}},
		{Center: geom.V{X: 0.015, Y: 0.005}, Polygon: []geom.V{{X: 0.01, Y: 0}, {X: 0.02, Y: 0}, {X: 0.02, Y: 0.01}, {X: 0.01, Y: 0.01}}, Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
	}
	mesh := prepareGrid(grid, ViewShaded)
	if len(mesh.Faces.Indices) == 0 || len(mesh.Outlines) == 0 {
		t.Fatal("visible rock lost its faces or outlines")
	}
	checkMesh(t, mesh.Faces)
	for _, outline := range mesh.Outlines {
		checkMesh(t, outline)
		if outline.fillRule != ebiten.FillRuleNonZero || !outline.premultiplied {
			t.Fatal("outline intersections or transparency use the wrong blending")
		}
		for _, v := range outline.Vertices {
			if v.DstX < 9.99 {
				t.Fatal("outline entered a black pocket")
			}
			if math.Abs(float64(v.ColorR-v.ColorA*18/255)) > 1e-7 {
				t.Fatal("outline color is not premultiplied")
			}
		}
	}
	for _, view := range []View{ViewClay, ViewHeight, ViewNormals, ViewShadows} {
		if diagnostic := prepareGrid(grid, view); len(diagnostic.Outlines) != 0 || len(diagnostic.Faces.Indices) == 0 {
			t.Fatalf("view %s lost faces or gained outlines", view)
		}
	}
}

func TestLargePreparedOutlineGroups(t *testing.T) {
	// More than 65536 stroke vertices sharing one opacity must be split
	// before calling the vector tessellator, then use valid 32-bit indices.
	grid := make(terrain.RockGrid, 5000)
	for i := range grid {
		x, y := float64(i%100)*.008, float64(i/100)*.008
		grid[i] = terrain.RockCell{Center: geom.V{X: x + 0.002, Y: y + 0.002}, Polygon: []geom.V{{X: x, Y: y}, {X: x + 0.004, Y: y}, {X: x + 0.004, Y: y + 0.004}, {X: x, Y: y + 0.004}}, Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 60, G: 60, B: 60, A: 255}}
	}
	mesh := prepareGrid(grid, ViewShaded)
	total := 0
	for _, outline := range mesh.Outlines {
		checkMesh(t, outline)
		total += len(outline.Vertices)
		if len(outline.Vertices) > 1<<16 {
			t.Fatal("outline batch exceeded the tessellator's index limit")
		}
	}
	if total <= 1<<16 || len(mesh.Outlines) < 2 {
		t.Fatal("test did not exercise large outline splitting")
	}
	if !reflect.DeepEqual(mesh, prepareGrid(grid, ViewShaded)) {
		t.Fatal("prepared geometry changed on revisiting a section")
	}
}

func TestPrepareSectionMatchesSerialLayers(t *testing.T) {
	data := terrain.SectionData{
		ID: 17,
		Background: terrain.RockGrid{{Center: geom.V{X: 0.05, Y: 0.05}, Polygon: []geom.V{{X: 0, Y: 0}, {X: 0.1, Y: 0}, {X: 0.1, Y: 0.1}, {X: 0, Y: 0.1}},
			Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 40, G: 40, B: 40, A: 255}}},
		Foreground: terrain.RockGrid{{Center: geom.V{X: 0.04, Y: 0.04}, Polygon: []geom.V{{X: 0, Y: 0.02}, {X: 0.08, Y: 0.02}, {X: 0.08, Y: 0.08}, {X: 0, Y: 0.08}},
			Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 90, G: 80, B: 70, A: 255}, Raised: true, Z: .030, Shadow: 1, Ambient: 1}},
		Vines: []terrain.Vine{
			{Parent: -1, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.003}, {P: geom.V{X: 0.11, Y: 0.11}, Radius: 0.002}, {P: geom.V{X: 0.12, Y: 0.12}}}},
			{Parent: 0, Joint: 1, Depth: 1, Points: []terrain.VinePoint{{P: geom.V{X: 0.11, Y: 0.11}, Radius: 0.002}, {P: geom.V{X: 0.12, Y: 0.11}, Radius: 0.001}, {P: geom.V{X: 0.13, Y: 0.11}}}},
		},
		ForegroundVines: []terrain.Vine{{Parent: -1, Foreground: true, Points: []terrain.VinePoint{
			{P: geom.V{X: 0.03, Y: 0.04}}, {P: geom.V{X: 0.04, Y: 0.05}, Radius: 0.003}, {P: geom.V{X: 0.05, Y: 0.06}},
		}}},
		Mushrooms: []terrain.MushroomGroup{{Mushrooms: []terrain.Mushroom{{
			Stem: []geom.V{{X: 0.05, Y: 0.04}, {X: 0.052, Y: 0.03}, {X: 0.05, Y: 0.02}}, CapCenter: geom.V{X: 0.05, Y: 0.018}, CapWidth: 0.01, CapHeight: 0.004,
			Color: terrain.MushroomColors[0],
		}}}},
	}
	previous := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	for _, view := range []View{ViewShaded, ViewClay, ViewHeight, ViewNormals, ViewShadows} {
		want := SectionMesh{ID: data.ID, Background: prepareGrid(data.Background, view), Foreground: prepareGrid(data.Foreground, view)}
		if view == ViewShaded {
			want.Vines = PrepareVines(data.Vines)
			want.ForegroundVines = PrepareForegroundVines(data.ForegroundVines)
			want.Mushrooms = PrepareMushrooms(data.Mushrooms)
		}
		for _, procs := range []int{1, 2, 4, 8} {
			t.Run(fmt.Sprintf("%s/procs%d", view, procs), func(t *testing.T) {
				runtime.GOMAXPROCS(procs)
				if got := PrepareSection(data, view); !reflect.DeepEqual(got, want) {
					t.Fatal("concurrent preparation changed layer geometry or ordering")
				}
			})
		}
	}
}

func BenchmarkPrepareSection(b *testing.B) {
	data := terrain.BuildSection(42, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PrepareSection(data, ViewShaded)
	}
}
