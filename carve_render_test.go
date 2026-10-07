package infinicave

import (
	"image"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCarvePlantsAcrossSectionSeamsAndReload(t *testing.T) {
	data := []terrain.SectionData{
		{ID: 0, Foreground: RockGrid{terrainRect(.1, -.1, .8, .2)}, Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(.4, -.1)}}}},
		{ID: 1, Foreground: RockGrid{terrainRect(.1, .9, .8, .2)}, Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .2, Y: 1}, Radius: .003}, {P: V{X: .8, Y: 1}, Radius: .003}}}}},
	}
	scene := queryScene(data...)
	if _, err := scene.CarveCircle(V{X: .4, Y: -1.1}, .02); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.Vegetation.Mushrooms) != 0 {
		t.Fatal("padded mushroom retained support across a seam")
	}
	if _, err := scene.CarveSegment(V{X: .5, Y: -1.05}, V{X: .5, Y: -.95}, .04); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[1].geometry.Vegetation.Vines) != 2 {
		t.Fatal("neighbor-owned vine was not split across a seam")
	}
	for _, d := range data {
		want := scene.world.sections[d.ID].geometry.Vegetation
		mesh := render.PrepareSection(d, ViewShaded)
		mesh.Geometry = terrain.PrepareTerrainGeometry(d, 0)
		render.FinishSectionMesh(&mesh)
		scene.applyStoredCuts(&mesh)
		if !reflect.DeepEqual(want, mesh.Geometry.Vegetation) {
			t.Fatal("reloading restored unsupported mushrooms or uncut vines")
		}
	}
}

func TestCarvePlantsDuringEveryUploadStage(t *testing.T) {
	data := terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .4, .8, .3)}, Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(.5, .4)}}},
		Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .1, Y: .4}, Radius: .003}, {P: V{X: .9, Y: .4}, Radius: .003}}}}}
	for stage := 0; stage <= 8; stage++ {
		t.Run(string(rune('0'+stage)), func(t *testing.T) {
			scene := queryScene()
			mesh := render.PrepareSection(data, ViewShaded)
			mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
			render.FinishSectionMesh(&mesh)
			u := &sectionUpload{data: mesh, stage: stage, next: 3}
			if stage == 3 || stage == 4 {
				u.foreground = ebiten.NewImage(1, 1)
			}
			if stage >= 5 {
				u.vines, u.mushrooms, u.foregroundVines = ebiten.NewImage(1, 1), ebiten.NewImage(1, 1), ebiten.NewImage(1, 1)
				scene.world.sections[0] = &worldSection{geometry: mesh.Geometry, vegetationPending: true}
			}
			scene.world.upload = u
			if _, err := scene.CarveCircle(V{X: .5, Y: -.6}, .02); err != nil {
				t.Fatal(err)
			}
			plants := u.data.Geometry.Vegetation
			if len(plants.Mushrooms) != 0 || len(plants.Vines) != 2 || len(u.data.Mushrooms.Indices) != 0 || !u.data.MushroomsBounds.Empty() {
				t.Fatal("upload kept stale mushroom/vine meshes or crop bounds")
			}
			if stage >= 5 && (u.stage != 5 || u.next != 0 || u.vines != nil || u.mushrooms != nil || u.foregroundVines != nil) {
				t.Fatal("partially uploaded plants were not restarted")
			}
			if stage >= 5 && !reflect.DeepEqual(scene.world.sections[0].geometry.Vegetation, plants) {
				t.Fatal("published section and pending plants disagree")
			}
			if u.foreground != nil {
				u.foreground.Deallocate()
			}
		})
	}
}

func TestCarvePlantCropBoundsRemainStableThroughReplay(t *testing.T) {
	data := terrain.SectionData{Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .1, Y: .5}, Radius: .004}, {P: V{X: .9, Y: .5}, Radius: .004}}}}}
	scene := queryScene(data)
	if _, err := scene.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil {
		t.Fatal(err)
	}
	first := render.SectionMesh{Geometry: scene.world.sections[0].geometry}
	scene.prepareCarvedVegetation(&first)
	for replay := 0; replay < 2; replay++ {
		mesh := render.PrepareSection(data, ViewShaded)
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		render.FinishSectionMesh(&mesh)
		scene.applyStoredCuts(&mesh)
		if mesh.VinesBounds == (image.Rectangle{}) || mesh.VinesBounds != first.VinesBounds || !reflect.DeepEqual(mesh.Vines, first.Vines) {
			t.Fatal("replay double-cropped vegetation or shifted its raster origin")
		}
	}
}

func TestAuthoredAndRuntimeHolesProduceTheSamePlants(t *testing.T) {
	content := SectionContent{Guides: []Guide{{Pts: []V{{X: .15, Y: .4}, {X: .5, Y: .35}, {X: .85, Y: .45}}, Seed: 1234}}}
	load := func(id int64) SectionContent {
		if id == 0 {
			return content
		}
		return SectionContent{}
	}
	base := terrain.NewSectionBuilder(42, load).Build(0)
	if len(base.Mushrooms) == 0 || len(base.Vines) == 0 || len(base.ForegroundVines) == 0 {
		t.Fatal("test needs generated mushrooms and both vine layers")
	}
	content.Holes = []Hole{{Shape: HoleCircle, Center: base.Mushrooms[0].Mushrooms[0].Anchor, Radius: .024}}
	for _, vines := range [][]Vine{base.Vines, base.ForegroundVines} {
		vine := vines[0]
		point := vine.Points[len(vine.Points)/2].P
		content.Holes = append(content.Holes, Hole{Shape: HoleCircle, Center: point, Radius: .02})
	}
	scene := queryScene(base)
	for _, hole := range content.Holes {
		if _, err := scene.Carve(terrain.TranslateHoleY(hole, -1)); err != nil {
			t.Fatal(err)
		}
	}
	want := scene.world.sections[0].geometry.Vegetation
	authored := terrain.NewSectionBuilder(42, load).Build(0)
	if got := terrain.SectionVegetation(authored); !reflect.DeepEqual(got, want) {
		t.Fatal("authored holes regrew or reshuffled vegetation instead of applying the same damage")
	}
	// Public section geometry carries the same clipped paths and whole plants.
	section, err := GenerateSectionWithConfig(Config{Seed: 42, LoadSection: load}, 0)
	if err != nil || !reflect.DeepEqual(section.Vines, want.Vines) || !reflect.DeepEqual(section.ForegroundVines, want.ForegroundVines) || !reflect.DeepEqual(section.Mushrooms, want.Mushrooms) {
		t.Fatalf("public authored section lost vegetation edits: %v", err)
	}
}
