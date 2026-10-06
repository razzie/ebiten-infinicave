package infinicave

import (
	"image"
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func testGroundedMushroom(x, y float64) Mushroom {
	return Mushroom{
		Anchor: V{x, y}, RootDirection: V{0, -1},
		Stem:      []V{{x, y + mushroomSink}, {x, y - .01}, {x, y - .02}},
		CapCenter: V{x, y - .025}, CapWidth: .012, CapHeight: .005,
		Color: mushroomColors[0],
	}
}

func TestCarveRemovesWholeUnsupportedMushroomsIndividually(t *testing.T) {
	group := MushroomGroup{Mushrooms: []Mushroom{
		testGroundedMushroom(.3, .4), testGroundedMushroom(.5, .4), testGroundedMushroom(.7, .4),
	}}
	scene := queryScene(sectionData{foreground: RockGrid{hoverRect(.1, .4, .8, .3)}, mushrooms: []MushroomGroup{group}})
	if _, err := scene.CarveCircle(V{.3, -.6}, .014); err != nil {
		t.Fatal(err)
	}
	plants := scene.world.sections[0].geometry.vegetation
	if len(plants.mushrooms) != 1 || !reflect.DeepEqual(plants.mushrooms[0].Mushrooms, group.Mushrooms[1:]) {
		t.Fatalf("unsupported mushroom was not removed independently: %+v", plants.mushrooms)
	}
	if len(group.Mushrooms) != 3 || group.Mushrooms[0].Stem[0] != (V{.3, .404}) {
		t.Fatal("carving modified source plant slices")
	}
	if _, err := scene.CarveSegment(V{.1, -.6}, V{.9, -.6}, .03); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.vegetation.mushrooms) != 0 {
		t.Fatal("empty mushroom groups were retained")
	}
}

func TestCarveRetainsMushroomWhileBuriedRootStillTouchesRock(t *testing.T) {
	m := testGroundedMushroom(.5, .4)
	scene := queryScene(sectionData{foreground: RockGrid{hoverRect(.1, .4, .8, .3)}, mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{m}}}})
	// Anchor is removed, but the buried root still has support.
	if _, err := scene.CarveCircle(V{.5, -.6}, .0025); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scene.world.sections[0].geometry.vegetation.mushrooms, []MushroomGroup{{Mushrooms: []Mushroom{m}}}) {
		t.Fatal("mushroom with remaining root contact was removed or partially cut")
	}
	// A cut through the cap alone does not remove a supported mushroom.
	if _, err := scene.CarveCircle(m.CapCenter.Add(V{Y: -1}), .009); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.vegetation.mushrooms) != 1 {
		t.Fatal("cap overlap removed a grounded mushroom")
	}
}

func TestCarveSplitsBothVineLayersBetweenSparseSamples(t *testing.T) {
	vine := Vine{Parent: -1, Points: []VinePoint{{V{.1, .5}, .003}, {V{.9, .5}, .007}}}
	front := vine
	front.Foreground = true
	scene := queryScene(sectionData{vines: []Vine{vine}, foregroundVines: []Vine{front}})
	result, err := scene.CarveCircle(V{.5, -.5}, .1)
	if err != nil || len(result.Changes) != 0 || len(result.SectionIDs) != 0 {
		t.Fatalf("vine-only cut should not report rock changes: %+v / %v", result, err)
	}
	plants := scene.world.sections[0].geometry.vegetation
	for _, vines := range [][]Vine{plants.vines, plants.foregroundVines} {
		if len(vines) != 2 {
			t.Fatalf("sparse vine did not split: %+v", vines)
		}
		left, right := vines[0].Points[len(vines[0].Points)-1], vines[1].Points[0]
		if left.P.Sub(V{.4, .5}).Len() > 1e-10 || right.P.Sub(V{.6, .5}).Len() > 1e-10 ||
			math.Abs(left.Radius-.0045) > 1e-10 || math.Abs(right.Radius-.0055) > 1e-10 {
			t.Fatalf("cut endpoints/radii were not interpolated: %+v / %+v", left, right)
		}
	}
	if len(vine.Points) != 2 || vine.Points[1].P != (V{.9, .5}) {
		t.Fatal("carving modified the original vine")
	}
	first := plants
	if _, err := scene.CarveCircle(V{.5, -.5}, .1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, scene.world.sections[0].geometry.vegetation) {
		t.Fatal("repeated hole changed surviving vines or added duplicate masks")
	}
}

func TestCarveRemapsBranchesAndPreservesFamilyMaterials(t *testing.T) {
	trunk := Vine{Parent: -1, Foreground: true}
	for _, x := range []float64{.1, .3, .5, .7, .9} {
		trunk.Points = append(trunk.Points, VinePoint{V{x, .5}, .004})
	}
	source := []Vine{trunk}
	for joint := 1; joint <= 3; joint++ {
		source = append(source, Vine{
			Parent: 0, Joint: joint, Depth: 1, Foreground: true,
			Points: []VinePoint{{trunk.Points[joint].P, .002}, {trunk.Points[joint].P.Add(V{Y: .3}), .001}},
		})
	}
	cut, _ := (Hole{Shape: HoleCircle, Center: V{.5, -.5}, Radius: .1}).rockCut()
	vines, changed := cut.vines(source, -1)
	if !changed || len(vines) != 5 {
		t.Fatalf("branching cut: %d survivors, changed=%v", len(vines), changed)
	}
	for i, vine := range vines {
		if vineStyleFamily(vines, i) != 0 || vinePalette(vines, i) != vinePalette(source, 0) || !vine.Foreground {
			t.Fatal("split changed a family material")
		}
		if vine.Parent >= 0 {
			if vine.Parent >= i || vine.Joint >= len(vines[vine.Parent].Points) || vine.Points[0].P != vines[vine.Parent].Points[vine.Joint].P {
				t.Fatal("surviving branch has an invalid attachment")
			}
		}
	}
	if vines[2].Parent != 0 || vines[3].Parent != -1 || vines[4].Parent != 1 {
		t.Fatalf("branches did not attach to the correct surviving trunk: %+v", vines)
	}
	for _, mesh := range prepareForegroundVines(vines) {
		checkMesh(t, mesh)
	}
	// Removing the first family must not recolor the next family by index.
	source = []Vine{
		{Parent: -1, Points: []VinePoint{{V{.48, .5}, .002}, {V{.52, .5}, .002}}},
		{Parent: -1, Points: []VinePoint{{V{.1, .7}, .002}, {V{.9, .7}, .002}}},
	}
	palette := vinePalette(source, 1)
	vines, _ = cut.vines(source, -1)
	if len(vines) != 1 || vinePalette(vines, 0) != palette || vineStyleFamily(vines, 0) != 1 {
		t.Fatal("removing an earlier family recolored the next family")
	}
}

func TestCarveVineTangencyAndRibbonOnlyOverlap(t *testing.T) {
	cut, _ := (Hole{Shape: HoleSegment, Start: V{.3, -.4}, End: V{.7, -.4}, Width: .2}).rockCut()
	v := Vine{Parent: -1, Points: []VinePoint{{V{.1, .5}, .01}, {V{.9, .5}, .01}}}
	fragments := splitVine(v, cut.localPolygon(-1))
	if len(fragments) != 1 || !reflect.DeepEqual(fragments[0].points, v.Points) {
		t.Fatal("tangent centerline was removed")
	}
	plants, changed := cut.vegetation(vegetationGeometry{vines: []Vine{v}}, nil, -1)
	if !changed || len(plants.cuts) != 1 {
		t.Fatal("ribbon overlap without centerline intersection lost its render mask")
	}
}

func TestCarvePlantsAcrossSectionSeamsAndReload(t *testing.T) {
	data := []sectionData{
		{id: 0, foreground: RockGrid{hoverRect(.1, -.1, .8, .2)}, mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(.4, -.1)}}}},
		{id: 1, foreground: RockGrid{hoverRect(.1, .9, .8, .2)}, vines: []Vine{{Parent: -1, Points: []VinePoint{{V{.2, 1}, .003}, {V{.8, 1}, .003}}}}},
	}
	scene := queryScene(data...)
	if _, err := scene.CarveCircle(V{.4, -1.1}, .02); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.vegetation.mushrooms) != 0 {
		t.Fatal("padded mushroom retained support across a seam")
	}
	if _, err := scene.CarveSegment(V{.5, -1.05}, V{.5, -.95}, .04); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[1].geometry.vegetation.vines) != 2 {
		t.Fatal("neighbor-owned vine was not split across a seam")
	}
	for _, d := range data {
		want := scene.world.sections[d.id].geometry.vegetation
		mesh := prepareSection(d, ViewShaded)
		mesh.geometry = prepareTerrainGeometry(d, 0)
		finishSectionMesh(&mesh)
		scene.applyStoredCuts(&mesh)
		if !reflect.DeepEqual(want, mesh.geometry.vegetation) {
			t.Fatal("reloading restored unsupported mushrooms or uncut vines")
		}
	}
}

func TestCarvePlantsDuringEveryUploadStage(t *testing.T) {
	data := sectionData{foreground: RockGrid{hoverRect(.1, .4, .8, .3)}, mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(.5, .4)}}},
		vines: []Vine{{Parent: -1, Points: []VinePoint{{V{.1, .4}, .003}, {V{.9, .4}, .003}}}}}
	for stage := 0; stage <= 8; stage++ {
		t.Run(string(rune('0'+stage)), func(t *testing.T) {
			scene := queryScene()
			mesh := prepareSection(data, ViewShaded)
			mesh.geometry = prepareTerrainGeometry(data, 0)
			finishSectionMesh(&mesh)
			u := &sectionUpload{data: mesh, stage: stage, next: 3}
			if stage == 3 || stage == 4 {
				u.foreground = ebiten.NewImage(1, 1)
			}
			if stage >= 5 {
				u.vines, u.mushrooms, u.foregroundVines = ebiten.NewImage(1, 1), ebiten.NewImage(1, 1), ebiten.NewImage(1, 1)
				scene.world.sections[0] = &worldSection{geometry: mesh.geometry, vegetationPending: true}
			}
			scene.world.upload = u
			if _, err := scene.CarveCircle(V{.5, -.6}, .02); err != nil {
				t.Fatal(err)
			}
			plants := u.data.geometry.vegetation
			if len(plants.mushrooms) != 0 || len(plants.vines) != 2 || len(u.data.mushrooms.indices) != 0 || !u.data.mushroomsBounds.Empty() {
				t.Fatal("upload kept stale mushroom/vine meshes or crop bounds")
			}
			if stage >= 5 && (u.stage != 5 || u.next != 0 || u.vines != nil || u.mushrooms != nil || u.foregroundVines != nil) {
				t.Fatal("partially uploaded plants were not restarted")
			}
			if stage >= 5 && !reflect.DeepEqual(scene.world.sections[0].geometry.vegetation, plants) {
				t.Fatal("published section and pending plants disagree")
			}
			if u.foreground != nil {
				u.foreground.Deallocate()
			}
		})
	}
}

func TestCarvePlantCropBoundsRemainStableThroughReplay(t *testing.T) {
	data := sectionData{vines: []Vine{{Parent: -1, Points: []VinePoint{{V{.1, .5}, .004}, {V{.9, .5}, .004}}}}}
	scene := queryScene(data)
	if _, err := scene.CarveCircle(V{.5, -.5}, .1); err != nil {
		t.Fatal(err)
	}
	first := sectionMesh{geometry: scene.world.sections[0].geometry}
	scene.prepareCarvedVegetation(&first)
	for replay := 0; replay < 2; replay++ {
		mesh := prepareSection(data, ViewShaded)
		mesh.geometry = prepareTerrainGeometry(data, 0)
		finishSectionMesh(&mesh)
		scene.applyStoredCuts(&mesh)
		if mesh.vinesBounds == (image.Rectangle{}) || mesh.vinesBounds != first.vinesBounds || !reflect.DeepEqual(mesh.vines, first.vines) {
			t.Fatal("replay double-cropped vegetation or shifted its raster origin")
		}
	}
}

func TestAuthoredAndRuntimeHolesProduceTheSamePlants(t *testing.T) {
	content := SectionContent{Guides: []Guide{{Pts: []V{{.15, .4}, {.5, .35}, {.85, .45}}, Seed: 1234}}}
	load := func(id int64) SectionContent {
		if id == 0 {
			return content
		}
		return SectionContent{}
	}
	base := buildSectionMode(42, 0, StudyNone, load)
	if len(base.mushrooms) == 0 || len(base.vines) == 0 || len(base.foregroundVines) == 0 {
		t.Fatal("test needs generated mushrooms and both vine layers")
	}
	content.Holes = []Hole{{Shape: HoleCircle, Center: base.mushrooms[0].Mushrooms[0].Anchor, Radius: .024}}
	for _, vines := range [][]Vine{base.vines, base.foregroundVines} {
		vine := vines[0]
		point := vine.Points[len(vine.Points)/2].P
		content.Holes = append(content.Holes, Hole{Shape: HoleCircle, Center: point, Radius: .02})
	}
	scene := queryScene(base)
	for _, hole := range content.Holes {
		if _, err := scene.Carve(hole.translatedY(-1)); err != nil {
			t.Fatal(err)
		}
	}
	want := scene.world.sections[0].geometry.vegetation
	authored := buildSectionMode(42, 0, StudyNone, load)
	if got := sectionVegetation(authored); !reflect.DeepEqual(got, want) {
		t.Fatal("authored holes regrew or reshuffled vegetation instead of applying the same damage")
	}
	// Public section geometry carries the same clipped paths and whole plants.
	section, err := GenerateSectionWithConfig(Config{Seed: 42, LoadSection: load}, 0)
	if err != nil || !reflect.DeepEqual(section.Vines, want.vines) || !reflect.DeepEqual(section.ForegroundVines, want.foregroundVines) || !reflect.DeepEqual(section.Mushrooms, want.mushrooms) {
		t.Fatalf("public authored section lost vegetation edits: %v", err)
	}
}
