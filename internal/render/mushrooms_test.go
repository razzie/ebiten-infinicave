package render

import (
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

func TestMushroomStalkClippingUsesCurrentCutTopology(t *testing.T) {
	cell := terrainRect(.4, .5, .2, .2)
	m := terrain.Mushroom{Anchor: geom.V{X: .5, Y: .5}, Stem: []geom.V{{X: .5, Y: .52}, {X: .5, Y: .4}}, CapCenter: geom.V{X: .5, Y: .4}, CapWidth: .01, CapHeight: .005, Color: terrain.MushroomColors[0]}
	groups := []terrain.MushroomGroup{{Mushrooms: []terrain.Mushroom{m}}}
	data := terrain.SectionData{Foreground: terrain.RockGrid{cell}, Mushrooms: groups}
	geometry := terrain.PrepareTerrainGeometry(data, 0)
	cut := terrain.RockCut{Poly: []geom.V{{X: .49, Y: -.49}, {X: .51, Y: -.49}, {X: .51, Y: -.47}, {X: .49, Y: -.47}}, Min: geom.V{X: .49, Y: -.49}, Max: geom.V{X: .51, Y: -.47}}
	carved, _, changed := cut.Geometry(0, geometry, 0)
	if !changed {
		t.Fatal("test cut did not remove rock around the buried stalk")
	}
	before := PrepareMushrooms(groups, geometry.Topology)
	after := PrepareMushrooms(groups, carved.Topology)
	oldArea, _ := mushroomStalkAreas(t, before, geometry.Grid)
	newArea, _ := mushroomStalkAreas(t, after, carved.Grid)
	if newArea <= oldArea+1e-6 {
		t.Fatal("terrain cut did not expose the newly uncovered stalk")
	}
}
