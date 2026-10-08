package terrain

import (
	"context"
	"math"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestCanceledBuildDoesNotLoadOrDecorate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	builder := NewSectionBuilder(42, func(int64) SectionContent { t.Fatal("canceled build loaded content"); return SectionContent{} })
	if _, ok := builder.BuildWithContext(ctx, 0, nil); ok {
		t.Fatal("canceled build returned a section")
	}
	ctx, cancel = context.WithCancel(context.Background())
	builder = NewSectionBuilder(42, func(int64) SectionContent { return SectionContent{} })
	published := false
	data, ok := builder.BuildWithContext(ctx, 0, func(SectionData) { published = true; cancel() })
	if !published || ok || len(data.Vines)+len(data.ForegroundVines)+len(data.Mushrooms) != 0 {
		t.Fatal("cancellation after terrain did not stop decoration")
	}
	if len(builder.fields.free)+len(builder.frontFields.free) != 0 {
		t.Fatal("canceled decoration allocated vine workspaces")
	}
}

type sectionStats struct {
	bg, fg, vines int
	area          float64
}

func statsOf(s SectionData) sectionStats {
	st := sectionStats{bg: len(s.Background), fg: len(s.Foreground), vines: len(s.Vines)}
	for _, c := range s.Foreground {
		st.area += geom.PolygonArea(c.Polygon)
	}
	return st
}

// Areas compare with a tolerance because contour clipping order can nudge
// polished contours slightly. Vine counts reflect generation in scene units;
// rescaling floats can change steering ties and the resulting offshoots.
// Counts use background material without baked foreground shadow darkening.
// Foreground snapshots include the completed-curve horizontal guide bias.
// Spurs start inside solid ridges and stop before guide shadows or edge fade.
// Procedural guide lips survive edge fade and omit short final spline knots.
// Open tips taper before their endpoints; spurs retain a sampled solid width.
func TestSectionStatsStable(t *testing.T) {
	want := map[int64]sectionStats{
		1:  {9710, 1544, 142, .7774879788},
		42: {9651, 1154, 138, .6324142322},
	}
	for seed, w := range want {
		got := statsOf(BuildSection(seed, 0))
		if got.bg != w.bg || got.fg != w.fg || got.vines != w.vines || math.Abs(got.area-w.area) > w.area*1e-4 {
			t.Errorf("seed %d: got %+v, want %+v", seed, got, w)
		}
	}
}

func BenchmarkBuildSection(b *testing.B) {
	for i := 0; i < b.N; i++ {
		BuildSection(42, int64(i%3))
	}
}

func TestSection10GenerationCompletes(t *testing.T) {
	// This section used to spend over a minute repeatedly growing thin
	// fragments into sprawling polygons. Keep a generous deadline so a
	// regression fails without hanging the test suite.
	result := make(chan SectionData, 1)
	go func() { result <- BuildSection(42, 10) }()
	select {
	case s := <-result:
		if len(s.Background) == 0 || len(s.Foreground) == 0 {
			t.Fatal("section 10 generated empty terrain")
		}
		for _, c := range s.Foreground {
			if len(c.Polygon) < 3 || geom.PolygonArea(c.Polygon) <= 0 || len(geom.Triangulate(c.Polygon)) != len(c.Polygon)-2 {
				t.Fatal("section 10 generated an invalid foreground face")
			}
		}
	case <-time.After(20 * time.Second):
		t.Fatal("section 10 generation stalled")
	}
}

func BenchmarkBuildSection10(b *testing.B) {
	for i := 0; i < b.N; i++ {
		BuildSection(42, 10)
	}
}

func TestCollisionReadyBeforeDecorationIncludesAuthoredHoles(t *testing.T) {
	load := func(id int64) SectionContent {
		if id != 0 {
			return SectionContent{}
		}
		return SectionContent{
			Guides: []Guide{{Pts: []geom.V{{X: .15, Y: .4}, {X: .5, Y: .35}, {X: .85, Y: .45}}, Seed: 1234}},
			Holes: []Hole{
				{Shape: HoleCircle, Center: geom.V{X: .5, Y: .4}, Radius: .08},
				{Shape: HoleSegment, Start: geom.V{X: .3, Y: .3}, End: geom.V{X: .6, Y: .6}, Width: .04},
			},
		}
	}
	var early *Geometry
	data := NewSectionBuilder(42, load).BuildWithTerrain(0, func(data SectionData) {
		if len(data.Vines)+len(data.ForegroundVines)+len(data.Mushrooms) != 0 {
			t.Fatal("collision waited for decoration")
		}
		early = PrepareTerrainGeometry(data, .002)
		if len(early.Collision.Polygons) == 0 || early.Collision.Contains(geom.V{X: .5, Y: -.6}) {
			t.Fatal("early collision omitted terrain or authored holes")
		}
	})
	final := PrepareTerrainGeometry(data, .002)
	if early == nil || !reflect.DeepEqual(early.Collision, final.Collision) || !reflect.DeepEqual(early.Grid, final.Grid) {
		t.Fatal("decoration changed published terrain")
	}
	if len(data.Vines)+len(data.ForegroundVines)+len(data.Mushrooms) == 0 {
		t.Fatal("prioritizing collision lost decoration")
	}
}

func TestSectionGeometryIndependentOfWorkersCacheAndBufferReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)
	load := func(id int64) SectionContent {
		if id != 0 {
			return SectionContent{}
		}
		return SectionContent{Guides: []Guide{{Pts: []geom.V{{X: .15, Y: .4}, {X: .5, Y: .35}, {X: .85, Y: .45}}, Seed: 1234}}}
	}
	for _, loader := range []SectionLoader{nil, load} {
		runtime.GOMAXPROCS(1)
		builder := NewSectionBuilder(42, loader)
		original := builder.Build(0)
		builder.Build(1)
		// Evict guide data without generating distant terrain.
		builder.guides.window(100)
		runtime.GOMAXPROCS(4)
		if regenerated := builder.Build(0); !reflect.DeepEqual(original, regenerated) {
			t.Fatal("same seed, section, and guides changed geometry after cache/buffer reuse")
		}
		if fresh := NewSectionBuilder(42, loader).Build(0); !reflect.DeepEqual(original, fresh) {
			t.Fatal("streaming and synchronous generation disagree")
		}
	}
}

func BenchmarkBuildStreamSection(b *testing.B) {
	builder := NewSectionBuilder(42, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		builder.Build(int64(i % 3))
	}
}

func TestHorizontalRandomSectionBorderCutoffs(t *testing.T) {
	data := NewSectionBuilder(93, nil, Horizontal).Build(0)
	grid := InsetForegroundGrid(data.Foreground)
	if len(grid) == 0 || len(data.Vines) == 0 || len(data.ForegroundVines) == 0 {
		t.Fatal("horizontal generation omitted rock or vines")
	}
	for _, c := range grid {
		for _, p := range c.Polygon {
			world := Horizontal.local(p)
			if world.Y < foregroundScreenInset-1e-9 || world.Y > 1-foregroundScreenInset+1e-9 {
				t.Fatalf("horizontal rock exceeds top/bottom cutoff: %v", world)
			}
		}
	}
	for _, vines := range [][]Vine{data.Vines, data.ForegroundVines} {
		for i, v := range vines {
			for _, point := range v.Points {
				p := Horizontal.local(point.P)
				if p.Y < 0 || p.Y > 1 {
					t.Fatalf("horizontal vine escapes top/bottom bounds: %v", p)
				}
			}
			if v.Parent >= 0 && (v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P) {
				t.Fatal("horizontal vine lost its branch attachment")
			}
		}
	}
}

func TestWorldSectionSeam(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		t.Run(orientation.String(), func(t *testing.T) { testWorldSectionSeam(t, orientation) })
	}
}

func testWorldSectionSeam(t *testing.T, orientation Orientation) {
	builder := NewSectionBuilder(42, nil, orientation)
	a, b := builder.Build(0), builder.Build(1)
	// Compare whole rock faces in a strip around the shared seam, not just
	// sample colors. Geometry, relief, shadowing, and material must agree.
	type face struct {
		color           [4]uint8
		polygon         []geom.V
		seed            int64
		z               float64
		normal          geom.V3
		shadow, ambient float64
	}
	collect := func(grid RockGrid, top float64) map[[2]int64]face {
		result := make(map[[2]int64]face)
		for _, c := range grid {
			y := c.Center.Y + top
			if y < -1.120 || y > -.880 {
				continue
			}
			poly := make([]geom.V, len(c.Polygon))
			for i, p := range c.Polygon {
				poly[i] = geom.V{X: p.X, Y: p.Y + top}
			}
			key := [2]int64{int64(math.Round(c.Center.X * 1e8)), int64(math.Round(y * 1e8))}
			result[key] = face{[4]uint8{c.Color.R, c.Color.G, c.Color.B, c.Color.A}, poly, cellSeed(42, c.Center, top), c.Z, c.Normal, c.Shadow, c.Ambient}
		}
		return result
	}
	for i, pair := range [][2]RockGrid{
		{a.Background, b.Background}, {a.Foreground, b.Foreground},
		{ShadeRockFaces(a.Foreground, ExposedRockEdges(a.Foreground)), ShadeRockFaces(b.Foreground, ExposedRockEdges(b.Foreground))},
		{ShadeRockFaces(a.Background, nil), ShadeRockFaces(b.Background, nil)},
	} {
		left, right := collect(pair[0], SectionTop(0)), collect(pair[1], SectionTop(1))
		if len(left) == 0 || len(left) != len(right) {
			t.Fatalf("layer %d seam has different faces: %d/%d", i, len(left), len(right))
		}
		for key, x := range left {
			y, ok := right[key]
			if !ok || x.color != y.color || x.seed != y.seed || len(x.polygon) != len(y.polygon) {
				t.Fatalf("layer %d: mismatched face at %v: found=%v color %v/%v normal %v/%v shadow %v/%v ambient %v/%v", i, key, ok, x.color, y.color, x.normal, y.normal, x.shadow, y.shadow, x.ambient, y.ambient)
			}
			if math.Abs(x.shadow-y.shadow) > 1e-8 || math.Abs(x.ambient-y.ambient) > 1e-8 || math.Abs(x.z-y.z) > 1e-11 || math.Abs(x.normal.X-y.normal.X)+math.Abs(x.normal.Y-y.normal.Y)+math.Abs(x.normal.Z-y.normal.Z) > 1e-8 {
				t.Fatalf("layer %d: height, normal, or lighting seam at %v: normal %v/%v shadow %v/%v ambient %v/%v", i, key, x.normal, y.normal, x.shadow, y.shadow, x.ambient, y.ambient)
			}
			for j, p := range x.polygon {
				if p.Sub(y.polygon[j]).Len() > 1e-9 {
					t.Fatalf("layer %d: polygon seam at %v: %v vs %v", i, key, p, y.polygon[j])
				}
			}
		}
	}
	// At least one full vine in the adjacent sections must span a boundary; its
	// offshoots must remain attached even outside the owner's core section.
	crossing := false
	for _, section := range []SectionData{a, b} {
		for i, v := range section.Vines {
			for j, p := range v.Points {
				if j == 0 {
					continue
				}
				prev := v.Points[j-1]
				for _, edge := range []float64{0, SectionHeight} {
					if (prev.P.Y-edge)*(p.P.Y-edge) < 0 && prev.Radius > 0 && p.Radius > 0 {
						crossing = true
					}
				}
			}
			if v.Depth > 0 {
				if v.Parent < 0 || v.Parent >= i {
					t.Fatal("cross-section vine lost its parent")
				}
				parent := section.Vines[v.Parent]
				if parent.Points[v.Joint].P != v.Points[0].P {
					t.Fatal("cross-section vine branch is detached")
				}
			}
		}
	}
	if !crossing {
		t.Fatal("vines stopped at all section boundaries")
	}
	// Eviction must not change either geometry or parent attachment indices.
	again := NewSectionBuilder(42, nil, orientation).Build(0)
	if !reflect.DeepEqual(a, again) {
		t.Fatal("revisiting an evicted section changes the scene")
	}
}
