package render

import (
	"context"
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
			if math.Abs(float64(v.ColorR-v.ColorA*3/255)) > 1e-7 {
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
		want := SectionMesh{ID: data.ID, BakedInstances: view == ViewShaded, Background: prepareGrid(data.Background, view), Foreground: prepareGrid(data.Foreground, view)}
		if view == ViewShaded {
			want.Vines = prepareRockVines(data.Vines, terrain.ShadeRockFaces(data.Background, nil, data.BackgroundNeighbors))
			want.ForegroundVines = prepareForegroundVines(data.ForegroundVines, sectionVineGrid(data), data.Orientation)
			want.Mushrooms = PrepareMushrooms(data.Mushrooms, terrain.NewRockTopology(data.Foreground))
			want.Plants = prepareMushroomAttachments(data.Mushrooms, terrain.NewRockTopology(data.Foreground), data.Orientation)
		}
		applyAuthoredVisualCuts(&want, data)
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

func TestForegroundGrowthReusesStructureAndMatchesCompleteRecipe(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for _, authored := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/authored=%v", mode, authored), func(t *testing.T) {
				point := func(p geom.V) geom.V {
					if mode == terrain.Horizontal {
						return geom.V{X: 1 - p.Y, Y: p.X}
					}
					return p
				}
				var loader terrain.SectionLoader
				if authored {
					loader = func(int64) terrain.SectionContent {
						return terrain.SectionContent{Guides: []terrain.Guide{terrain.SplineGuide([]geom.V{point(geom.V{X: .1, Y: .4}), point(geom.V{X: .9, Y: .4})}, 42)}, Holes: []terrain.Hole{{Shape: terrain.HoleCircle, Center: point(geom.V{X: .5, Y: .4}), Radius: .04}}}
					}
				}
				builder := terrain.NewSectionBuilder(42, loader, mode)
				builder.Shape = terrain.ShapeOptions{EdgeRoughness: .7, SpikeDensity: .8}
				build := builder.Begin(context.Background(), 0)
				if build == nil || !build.MaterializeForeground(context.Background()) {
					t.Fatal("early foreground failed")
				}
				options := RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1}
				data := build.Data()
				base := PrepareRockBase(data, ViewShaded, options)
				before := PrepareRockBase(data, ViewShaded, options)
				earlyGrowth := PrepareForegroundGrowth(data, ViewShaded, base, options)
				if !build.Materialize(context.Background()) {
					t.Fatal("material failed")
				}
				got := PrepareForegroundGrowth(build.Data(), ViewShaded, base, options)
				want := PrepareForeground(build.Data(), ViewShaded, options)
				if !reflect.DeepEqual(earlyGrowth, want) {
					t.Fatal("growth depended on background compatibility material")
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("growth-only preparation changed geometry, IDs, order, or authored cuts")
				}
				if !reflect.DeepEqual(base, before) {
					t.Fatal("growth preparation mutated the published base")
				}
				if len(base.Foreground.Faces.Vertices) == 0 || &base.Foreground.Faces.Vertices[0] != &got.Foreground.Faces.Vertices[0] {
					t.Fatal("growth rebuilt structural vertices")
				}
				if &base.Foreground.Faces.Indices[0] != &got.Foreground.Faces.Indices[0] {
					t.Fatal("growth rebuilt structural indices")
				}
			})
		}
	}
}

func TestForegroundMasksRetainSupportIndices(t *testing.T) {
	grid := GridMesh{Growth: []Attachment{
		{Kind: HangingAttachment, Underlay: true}, {Kind: EdgeAttachment, Underlay: true},
		{Kind: SurfaceMoss}, {Kind: EdgeAttachment}, {Kind: GrassAttachment},
	}}
	grid.GrowthIndex = AttachmentIndex(grid.Growth)
	for _, span := range []ForegroundSpan{StructuralSpan, StructuralTopSpan, UnderGrowthSpan, SurfaceGrowthSpan} {
		masked := MaskForeground(grid, span)
		if len(masked.Growth) != len(grid.Growth) || masked.GrowthIndex != grid.GrowthIndex {
			t.Fatal("mask invalidated attachment indices")
		}
		for i, a := range masked.Growth {
			if a.Dead == span.Contains(grid.Growth[i]) || grid.Growth[i].Dead {
				t.Fatal("mask changed source liveness or drew another span")
			}
		}
	}
}

func TestMushroomOwnershipUsesHalfOpenAnchorBand(t *testing.T) {
	mushrooms := []terrain.Mushroom{
		{Anchor: geom.V{X: .5, Y: -.001}, CapCenter: geom.V{X: .5, Y: 0}, CapWidth: .01, CapHeight: .005},
		{Anchor: geom.V{X: .5, Y: 0}, CapCenter: geom.V{X: .5, Y: -.01}, CapWidth: .01, CapHeight: .005},
		{Anchor: geom.V{X: .5, Y: .99}, CapCenter: geom.V{X: .5, Y: .98}, CapWidth: .01, CapHeight: .005},
		{Anchor: geom.V{X: .5, Y: 1}, CapCenter: geom.V{X: .5, Y: .99}, CapWidth: .01, CapHeight: .005},
	}
	groups := []terrain.MushroomGroup{{Mushrooms: mushrooms}}
	got := PrepareOwnedMushrooms(groups, nil)
	want := PrepareMushrooms([]terrain.MushroomGroup{{Mushrooms: mushrooms[1:3]}}, nil)
	if !reflect.DeepEqual(got, want) || len(groups[0].Mushrooms) != 4 {
		t.Fatal("ownership altered padded input or included another anchor band")
	}
}

func TestSupersampledBatchBoundsVertexSubmissionsAndKeepsMaterials(t *testing.T) {
	mesh := TriangleMesh{Vertices: make([]ebiten.Vertex, 100000), Indices: []uint32{99999, 4, 0, 0, 4, 55}, premultiplied: true}
	for _, i := range mesh.Indices {
		mesh.Vertices[i] = ebiten.Vertex{DstX: float32(i) / 100, DstY: 3, SrcX: 7, SrcY: 11, ColorA: .5}
	}
	original := append([]ebiten.Vertex(nil), mesh.Vertices...)
	for start := 0; start < len(mesh.Indices); start += 3 {
		batch := SupersampledBatch(mesh, start, start+3)
		if len(batch.Vertices) > len(batch.Indices) || !batch.premultiplied {
			t.Fatal("batch sent unrelated vertices or lost its blend mode")
		}
		for j, index := range batch.Indices {
			got := batch.Vertices[index]
			want := original[mesh.Indices[start+j]]
			want.DstX *= 2
			want.DstY *= 2
			if got != want {
				t.Fatal("batch changed triangle order or material coordinates")
			}
		}
	}
	if !reflect.DeepEqual(original, mesh.Vertices) {
		t.Fatal("batch modified the immutable source mesh")
	}
}

// Compare the actual ordered primitives touching the destination, including
// material coordinates. Removed primitives must be entirely outside it.
func bandTriangles(mesh TriangleMesh, pixels int) [][3]ebiten.Vertex {
	var triangles [][3]ebiten.Vertex
	for i := 0; i < len(mesh.Indices); i += 3 {
		tri := [3]ebiten.Vertex{mesh.Vertices[mesh.Indices[i]], mesh.Vertices[mesh.Indices[i+1]], mesh.Vertices[mesh.Indices[i+2]]}
		lo, hi := tri[0].DstY, tri[0].DstY
		for _, v := range tri[1:] {
			lo = min(lo, v.DstY)
			hi = max(hi, v.DstY)
		}
		if hi >= 0 && lo <= float32(pixels) {
			triangles = append(triangles, tri)
		}
	}
	return triangles
}

func TestOwnedTerrainKeepsVisiblePrimitives(t *testing.T) {
	for _, raised := range []bool{false, true} {
		var grid terrain.RockGrid
		for _, y := range []float64{-.8, -.001, .2, .999, 1.7} {
			poly := []geom.V{{X: .2, Y: y}, {X: .3, Y: y}, {X: .3, Y: y + .1}, {X: .2, Y: y + .1}}
			grid = append(grid, terrain.RockCell{Center: geom.PolygonCenter(poly), Polygon: poly, Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 80, G: 60, B: 40, A: 255}, Raised: raised, Z: .05, Shadow: 1, Ambient: 1})
		}
		for _, view := range []View{ViewShaded, ViewClay, ViewNormals} {
			full := prepareGrid(grid, view)
			owned := PrepareOwnedGridWithTopology(grid, view, nil)
			if len(owned.Faces.Indices) >= len(full.Faces.Indices) {
				t.Fatal("off-band terrain was not culled")
			}
			for _, pixels := range []int{1, 2, 7, 501, 1000, 1920} {
				a, b := ScaleGridMesh(full, pixels, 0), ScaleGridMesh(owned, pixels, 0)
				if !reflect.DeepEqual(bandTriangles(a.Faces, pixels), bandTriangles(b.Faces, pixels)) {
					t.Fatalf("%v %s %d: visible faces changed", raised, view, pixels)
				}
				var ao, bo [][3]ebiten.Vertex
				for _, mesh := range a.Outlines {
					ao = append(ao, bandTriangles(mesh, pixels)...)
				}
				for _, mesh := range b.Outlines {
					bo = append(bo, bandTriangles(mesh, pixels)...)
				}
				if !reflect.DeepEqual(ao, bo) {
					t.Fatalf("%v %s %d: visible outlines changed", raised, view, pixels)
				}
			}
		}
	}
}
