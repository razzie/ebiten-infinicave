package infinicave

import (
	"math"
	"reflect"
	"testing"
)

func TestSectionLoaderHoleCoordinatesAndOwnership(t *testing.T) {
	source := SectionContent{
		Guides: []Guide{{Pts: []V{{.2, .9}, {.7, 1.1}}}},
		Holes: []Hole{
			{Shape: HoleCircle, Center: V{.4, 1}, Radius: .06},
			{Shape: HoleSegment, Start: V{.2, .9}, End: V{.7, 1.1}, Width: .02},
			{Shape: HoleCircle, Center: V{.5, .5}, Radius: math.NaN()},
			{Shape: HoleCircle, Center: V{.5, .5}, Radius: 2},
			{Shape: HoleShape(255)},
		},
	}
	before := append([]Hole(nil), source.Holes...)
	loader := func(id int64) SectionContent {
		if id == -1 {
			return source
		}
		return SectionContent{}
	}
	a, b := loadedWorldContent(42, 0, loader), loadedWorldContent(42, 1, loader)
	if len(a.Holes) != 2 || len(b.Holes) != 2 {
		t.Fatal("invalid holes were not filtered")
	}
	for i := range a.Holes {
		aHole, bHole := a.Holes[i].translatedY(sectionTop(0)), b.Holes[i].translatedY(sectionTop(1))
		if aHole != bHole {
			t.Fatalf("neighboring hole positions differ: %+v / %+v", aHole, bHole)
		}
	}
	a.Holes[0].Center.X = 99
	for i := range before {
		if source.Holes[i].Shape != before[i].Shape || source.Holes[i].Center != before[i].Center || source.Holes[i].Width != before[i].Width {
			t.Fatal("loader-owned hole was modified")
		}
	}
}

func TestSectionLoaderHolesCarveGeneratedGeometry(t *testing.T) {
	guides := []Guide{{Pts: []V{{.15, .4}, {.5, .35}, {.85, .45}}, Seed: 1234}}
	content := SectionContent{Guides: guides}
	load := func(id int64) SectionContent {
		if id == 0 {
			return content
		}
		return SectionContent{}
	}
	base := newSectionBuilder(42, load).build(0)
	var center V
	area := 0.0
	for _, cell := range base.foregroundTopology.grid {
		if cell.Center.Y > .1 && cell.Center.Y < .9 && faceArea(cell.Polygon) > area {
			center, area = cell.Center, faceArea(cell.Polygon)
		}
	}
	if area == 0 || !prepareTerrainGeometry(base, 0).collision.Contains(center.Add(V{Y: -1})) {
		t.Fatal("test needs a solid blast center")
	}
	content.Holes = []Hole{{Shape: HoleCircle, Center: center, Radius: .02}}
	carved := newSectionBuilder(42, load).build(0)
	geometry := prepareTerrainGeometry(carved, 0)
	if geometry.collision.Contains(center.Add(V{Y: -1})) || len(geometry.cuts) != 1 || len(carved.holes) != 1 {
		t.Fatal("authored hole did not reach collision and render geometry")
	}
	for _, cell := range carved.foreground {
		if insideFace(center, cell.Polygon) {
			t.Fatal("exported foreground grid filled authored hole")
		}
	}
	if again := newSectionBuilder(42, load).build(0); !reflect.DeepEqual(carved, again) {
		t.Fatal("authored holes changed across regeneration")
	}
	mesh := prepareSection(carved, ViewClay)
	checkMesh(t, mesh.foreground.faces)
	section, err := GenerateSectionWithConfig(Config{Seed: 42, LoadSection: load}, 0)
	if err != nil || len(section.Holes) != 1 || section.Collision.Contains(center.Add(V{Y: -1})) {
		t.Fatalf("public section lost authored hole: %v", err)
	}
}

func TestCarveRimsFollowCutBoundariesAndDiagnosticViews(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{hoverRect(.1, .1, .8, .8)}})
	if _, err := scene.CarveCircle(V{.5, -.5}, .12); err != nil {
		t.Fatal(err)
	}
	h := scene.world.sections[0].geometry
	topology := carvedTopology(h.grid, h.cuts, h.top)
	vs, is := appendCarveRims(nil, nil, topology, ViewShaded)
	if len(is) == 0 {
		t.Fatal("blast did not expose any crater walls/rim")
	}
	checkMesh(t, triangleMesh{vertices: vs, indices: is})
	for _, v := range vs {
		p := V{float64(v.DstX) / rasterPixelsPerUnit, float64(v.DstY)/rasterPixelsPerUnit + generationMinY}
		if distance := p.Sub(V{.5, .5}).Len(); distance < .10 || distance > .14 {
			t.Fatalf("decorative rim escaped cut boundary: %v", p)
		}
	}
	shaded := prepareGridWithTopology(nil, ViewShaded, topology)
	normals := prepareGridWithTopology(nil, ViewNormals, topology)
	withoutCuts := *topology
	withoutCuts.cuts = nil
	normalsNoRim := prepareGridWithTopology(nil, ViewNormals, &withoutCuts)
	if len(shaded.faces.indices) <= len(normals.faces.indices) || !reflect.DeepEqual(normals, normalsNoRim) {
		t.Fatal("rim should appear in shaded/clay views and leave diagnostic geometry alone")
	}
}
