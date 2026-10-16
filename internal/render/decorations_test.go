package render

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestMushroomStalkClippingKeepsCapsAndExposedWidth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		poly    []geom.V
		stem    []geom.V
		exposed float64
	}{
		{"slope", []geom.V{{X: .4, Y: .45}, {X: .6, Y: .55}, {X: .6, Y: .7}, {X: .4, Y: .7}},
			[]geom.V{{X: .5, Y: .54}, {X: .5, Y: .46}, {X: .5, Y: .4}}, .1},
		{"concave pocket", []geom.V{{X: .4, Y: .4}, {X: .48, Y: .4}, {X: .48, Y: .53}, {X: .52, Y: .53}, {X: .52, Y: .4}, {X: .6, Y: .4}, {X: .6, Y: .6}, {X: .4, Y: .6}},
			[]geom.V{{X: .5, Y: .56}, {X: .5, Y: .45}, {X: .5, Y: .35}}, .18},
		{"section padding", []geom.V{{X: .4, Y: -.01}, {X: .6, Y: -.01}, {X: .6, Y: .1}, {X: .4, Y: .1}},
			[]geom.V{{X: .5, Y: .004}, {X: .5, Y: -.02}, {X: .5, Y: -.04}}, .03},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := terrain.Mushroom{Anchor: tc.stem[0], Stem: tc.stem, CapCenter: geom.V{X: .5, Y: .48}, CapWidth: .1, CapHeight: .04, Color: terrain.MushroomColors[0]}
			groups := []terrain.MushroomGroup{{Mushrooms: []terrain.Mushroom{m}}}
			grid := terrain.RockGrid{{Polygon: tc.poly, Color: color.NRGBA{A: 255}}}
			topology := terrain.NewRockTopology(grid)
			mesh := PrepareMushrooms(groups, topology)
			checkMesh(t, mesh)
			unclipped := PrepareMushrooms(groups, nil)
			caps := 2 * (1 + len(terrain.MushroomCapOutline(m)))
			if !reflect.DeepEqual(mesh.Vertices[len(mesh.Vertices)-caps:], unclipped.Vertices[len(unclipped.Vertices)-caps:]) {
				t.Fatal("stalk clipping changed cap geometry or shading")
			}
			border, inner := mushroomStalkAreas(t, mesh, topology.Grid)
			if math.Abs(border-m.StemWidth()*tc.exposed) > 1e-8 || math.Abs(inner-m.CapWidth*.11*tc.exposed) > 1e-8 {
				t.Fatalf("exposed stalk area = %g, %g; want %g, %g", border, inner, m.StemWidth()*tc.exposed, m.CapWidth*.11*tc.exposed)
			}
			if !reflect.DeepEqual(mesh, PrepareMushrooms(groups, topology)) || !reflect.DeepEqual(m.Stem, tc.stem) {
				t.Fatal("clipping changed source geometry or deterministic preparation")
			}
		})
	}
}

// Independently sample every stalk triangle and measure its visible area.
func mushroomStalkAreas(t *testing.T, mesh TriangleMesh, rock terrain.RockGrid) (border, inner float64) {
	t.Helper()
	for i := 0; i < len(mesh.Indices); i += 3 {
		v := mesh.Vertices[mesh.Indices[i]]
		isBorder := v.ColorR == float32(108)/255 && v.ColorG == float32(70)/255
		isInner := v.ColorR == 1 && v.ColorG == float32(233)/255
		if !isBorder && !isInner {
			continue
		}
		var tri [3]geom.V
		for j := range tri {
			p := mesh.Vertices[mesh.Indices[i+j]]
			tri[j] = geom.V{X: float64(p.DstX) / RasterPixelsPerUnit, Y: float64(p.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
		}
		area := math.Abs(geom.PolygonArea(tri[:]))
		if isBorder {
			border += area
		} else {
			inner += area
		}
		for a := 1; a < 8; a++ {
			for b := 1; a+b < 8; b++ {
				p := tri[0].Mul(float64(a) / 8).Add(tri[1].Mul(float64(b) / 8)).Add(tri[2].Mul(float64(8-a-b) / 8))
				for _, cell := range rock {
					if geom.InsidePolygon(p, cell.Polygon) {
						t.Fatalf("visible stalk overlaps rock at %+v", p)
					}
				}
			}
		}
	}
	return
}

func TestMushroomStalkClippingIncludesProjectedRockWalls(t *testing.T) {
	cell := terrainRect(.4, .4, .1, .2)
	cell.Z, cell.Normal = .1, geom.V3{Z: 1}
	topology := terrain.NewRockTopology(terrain.RockGrid{cell})
	m := terrain.Mushroom{Stem: []geom.V{{X: .498, Y: .5}, {X: .504, Y: .5}}, CapCenter: geom.V{X: .51, Y: .5}, CapWidth: .01, CapHeight: .005, Color: terrain.MushroomColors[0]}
	mesh := PrepareMushrooms([]terrain.MushroomGroup{{Mushrooms: []terrain.Mushroom{m}}}, topology)
	// The right wall reaches x=.5 + .045*(.1+.008), beyond this stalk.
	if len(mesh.Vertices) != 2*(1+len(terrain.MushroomCapOutline(m))) {
		t.Fatal("a stalk buried entirely in the projected rock wall remained visible")
	}
}

func TestCarveKeepsOriginalStalkOcclusionRecipeAndRemovesDamagedGroup(t *testing.T) {
	cell := terrainRect(.4, .5, .2, .2)
	m := terrain.Mushroom{Anchor: geom.V{X: .5, Y: .5}, Stem: []geom.V{{X: .5, Y: .52}, {X: .5, Y: .4}}, CapCenter: geom.V{X: .5, Y: .4}, CapWidth: .01, CapHeight: .005, Color: terrain.MushroomColors[0]}
	data := terrain.SectionData{Foreground: terrain.RockGrid{cell}, Mushrooms: []terrain.MushroomGroup{{Mushrooms: []terrain.Mushroom{m}}}}
	geometry := terrain.PrepareTerrainGeometry(data, 0)
	cut, _ := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .5, Y: -.49}, Radius: .014})
	carved, _, changed := cut.Geometry(0, geometry, 0)
	carved, plantsChanged := cut.Plants(carved)
	if !changed || !plantsChanged || len(carved.Vegetation.Mushrooms) != 0 {
		t.Fatal("stalk damage retained the mushroom")
	}
	if !reflect.DeepEqual(PrepareMushrooms(data.Mushrooms, geometry.Topology), PrepareMushrooms(data.Mushrooms, carved.Topology)) {
		t.Fatal("cut regenerated or exposed the old stalk recipe")
	}
}

func TestSpiderWebMeshUsesPreparedTerrainSites(t *testing.T) {
	grid := terrain.RockGrid{terrainRect(.1, .1, .6, .6)}
	web := terrain.NewSpiderWeb([]geom.V{{X: .3, Y: .3}, {X: .36, Y: .3}, {X: .33, Y: .35}}, .5)
	// Supplying a prepared appearance prevents the renderer from selecting
	// different sites or redoing the lighting when a cached layer is resized.
	topology := terrain.NewRockTopology(grid)
	topology.Appearance = append(terrain.RockGrid(nil), topology.Grid...)
	topology.SpiderWebs = []terrain.SpiderWeb{web}
	want := prepareSpiderWebs(topology.SpiderWebs, false)
	checkMesh(t, want)
	if len(want.Indices) == 0 || !want.premultiplied {
		t.Fatal("missing translucent silk mesh")
	}
	for _, view := range []View{ViewShaded, ViewClay, ViewHeight, ViewNormals, ViewShadows} {
		rock := PrepareForeground(terrain.SectionData{ForegroundTopology: topology}, view)
		bare := *topology
		bare.SpiderWebs = nil
		if !reflect.DeepEqual(rock.Foreground, PrepareForeground(terrain.SectionData{ForegroundTopology: &bare}, view).Foreground) {
			t.Fatal("rock mesh includes web decoration")
		}
		decoration := PrepareVegetation(terrain.SectionData{ForegroundTopology: topology}, view)
		if view == ViewShaded {
			expected := SectionMesh{}
			expected.SpiderWebs = PrepareOwnedSpiderWebs(topology.SpiderWebs)
			FinishSectionMesh(&expected)
			if !reflect.DeepEqual(decoration.SpiderWebs, expected.SpiderWebs) || decoration.SpiderWebsBounds != expected.SpiderWebsBounds {
				t.Fatal("decoration renderer did not use the precomputed web sites")
			}
		} else if len(decoration.SpiderWebs.Indices) != 0 {
			t.Fatalf("web appeared in %s diagnostic view", view)
		}
	}
	if !reflect.DeepEqual(want, prepareSpiderWebs(topology.SpiderWebs, false)) {
		t.Fatal("prepared web changed on redraw")
	}
}

func TestSpiderWebImagesClipToOwnedBandAcrossSeams(t *testing.T) {
	for _, y := range []float64{-.02, .98} {
		web := terrain.NewSpiderWeb([]geom.V{{X: .3, Y: y}, {X: .36, Y: y}, {X: .33, Y: y + .05}}, .5)
		mesh := PrepareVegetation(terrain.SectionData{ForegroundTopology: &terrain.RockTopology{SpiderWebs: []terrain.SpiderWeb{web}}}, ViewShaded)
		if mesh.SpiderWebsBounds.Empty() {
			t.Fatal("seam-crossing web was culled")
		}
		for _, bounds := range []image.Rectangle{mesh.SpiderWebsBounds} {
			if !bounds.Empty() && (bounds.Min.Y < RasterPixelsPerUnit || bounds.Max.Y > 2*RasterPixelsPerUnit) {
				t.Fatal("web image overlaps a neighboring section's owned band")
			}
		}
		for _, pixels := range []int{1, 501, 1920} {
			scaled := ScaleVegetationMesh(mesh, pixels)
			for _, bounds := range []image.Rectangle{scaled.SpiderWebsBounds} {
				if !bounds.Empty() && (bounds.Min.Y < pixels || bounds.Max.Y > 2*pixels) {
					t.Fatal("scaled web image overlaps a neighboring band")
				}
			}
		}
	}
}

func TestSpiderWebMeshDrawsCachedNetworkAndThreadRoles(t *testing.T) {
	w := terrain.NewSpiderWeb([]geom.V{{X: .2, Y: .3}, {X: .34, Y: .3}, {X: .23, Y: .4}}, .5)
	if w.Network == nil {
		t.Fatal("fixture has no silk network")
	}
	// A prepared node can be moved independently of the pocket metadata.
	// Rendering must use that exact snapshot without refitting the web.
	w.Network.Nodes[w.Network.Frame[0]].X += .001
	mesh := prepareSpiderWebs([]terrain.SpiderWeb{w}, false)
	checkMesh(t, mesh)
	if len(mesh.Vertices) != 4*len(w.Network.Threads) || len(mesh.Indices) != 6*len(w.Network.Threads) {
		t.Fatal("renderer added or omitted prepared silk")
	}
	var supportWidth, captureWidth, supportAlpha, captureAlpha float32
	for i, silk := range w.Network.Threads {
		vertices := mesh.Vertices[i*4 : (i+1)*4]
		for _, end := range []struct {
			node, first, second int
		}{{silk.A, 0, 3}, {silk.B, 1, 2}} {
			p := w.Network.Nodes[end.node]
			x := (vertices[end.first].DstX + vertices[end.second].DstX) / 2
			y := (vertices[end.first].DstY + vertices[end.second].DstY) / 2
			if absFloat32(x-float32(p.X*RasterPixelsPerUnit)) > .0001 || absFloat32(y-float32((p.Y-terrain.GenerationMinY)*RasterPixelsPerUnit)) > .0002 {
				t.Fatal("rendering changed a settled silk endpoint")
			}
		}
		dx, dy := vertices[0].DstX-vertices[3].DstX, vertices[0].DstY-vertices[3].DstY
		width := dx*dx + dy*dy
		if silk.Kind == terrain.WebSupport {
			supportWidth, supportAlpha = width, vertices[0].ColorA
		} else if silk.Kind == terrain.WebCapture {
			captureWidth, captureAlpha = width, vertices[0].ColorA
		}
	}
	if supportWidth <= captureWidth || supportAlpha <= captureAlpha {
		t.Fatal("attachment silk is not stronger than capture silk")
	}
}

func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
