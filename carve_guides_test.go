package infinicave

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCarveRetiresGuidesOnlyAfterLastFragment(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		t.Run(mode.String(), func(t *testing.T) {
			guide := terrain.SplineGuide([]V{{X: .2, Y: .3}, {X: .8, Y: .3}}, 1)
			scene := queryScene(terrain.SectionData{Guides: []Guide{guide}, Foreground: RockGrid{terrainRect(.1, .3, .8, .3)}})
			scene.orientation = mode
			original := scene.world.sections[0].geometry
			key := terrain.GuideKey(guide.Pts, original.Top)
			query := mustQuery(t, scene, Ray{Origin: terrain.WorldPoint(mode, V{X: .3, Y: -.7})}, QueryOptions{Targets: TargetGuide})
			if !query.Found {
				t.Fatal("missing original guide")
			}
			carve := func(x, width float64) {
				t.Helper()
				_, err := scene.CarveSegment(terrain.WorldPoint(mode, V{X: x, Y: -.8}), terrain.WorldPoint(mode, V{X: x, Y: -.3}), width)
				if err != nil {
					t.Fatal(err)
				}
			}
			carve(.5, .1)
			carve(.25, .5)
			if len(scene.world.retiredGuides) != 0 || len(scene.world.sections[0].geometry.Guides) != 1 {
				t.Fatal("guide retired while a detached fragment survived")
			}
			carve(.75, .5)
			if !scene.world.retiredGuides[key] || len(scene.world.sections[0].geometry.Guides) != 0 {
				t.Fatal("destroyed formation retained its guide")
			}
			if _, ok := scene.Guide(query.Hit.GuideID); ok {
				t.Fatal("retired guide remained queryable")
			}
			if len(original.Guides) != 1 {
				t.Fatal("cleanup mutated published geometry")
			}
			// A result generated before the cut must not resurrect the guide.
			mesh := render.SectionMesh{ID: 0, Geometry: original}
			scene.applyStoredCuts(&mesh)
			if len(mesh.Geometry.Guides) != 0 {
				t.Fatal("late worker result resurrected a retired guide")
			}
			delete(scene.world.sections, 0)
			scene.world.jobs = make(chan sectionJob, 1)
			scene.world.request(0)
			job := <-scene.world.jobs
			if !job.retiredGuides[key] {
				t.Fatal("eviction lost generation exclusions")
			}
		})
	}
}

func TestCarveKeepsGuideForSharedAndIncompleteFormations(t *testing.T) {
	guide := terrain.SplineGuide([]V{{X: .2, Y: .3}, {X: .8, Y: .3}}, 1)
	for _, test := range []struct {
		name string
		grid RockGrid
	}{
		{"shared", RockGrid{terrainRect(.1, .3, .3, .2), terrainRect(.6, .3, .3, .2)}},
		{"uncached continuation", RockGrid{terrainRect(.1, -.3, .3, .8)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			scene := queryScene(terrain.SectionData{Guides: []Guide{guide}, Foreground: test.grid})
			if _, err := scene.CarveSegment(V{X: .25, Y: -1.4}, V{X: .25, Y: -.4}, .4); err != nil {
				t.Fatal(err)
			}
			if len(scene.world.retiredGuides) != 0 || len(scene.world.sections[0].geometry.Guides) != 1 {
				t.Fatal("guide retired despite another formation or unloaded continuation")
			}
		})
	}
}

func TestCarveRetiresGuideAcrossSectionBoundary(t *testing.T) {
	points := []V{{X: .2, Y: -.05}, {X: .8, Y: -.05}}
	guide := terrain.SplineGuide(points, 1)
	neighbor := terrain.SplineGuide([]V{{X: .2, Y: .95}, {X: .8, Y: .95}}, 1)
	scene := queryScene(
		terrain.SectionData{ID: 0, Guides: []Guide{guide}, Foreground: RockGrid{terrainRect(.1, -.05, .8, .1)}},
		terrain.SectionData{ID: 1, Guides: []Guide{neighbor}, Foreground: RockGrid{terrainRect(.1, .95, .8, .1)}},
	)
	result, err := scene.CarveSegment(V{X: .05, Y: -1}, V{X: .95, Y: -1}, .2)
	if err != nil || !result.Complete || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 0 {
		t.Fatalf("cross-section destruction: %+v, %v", result, err)
	}
	if len(scene.world.retiredGuides) != 1 {
		t.Fatal("copies in neighboring windows were not retired as one guide")
	}
	for _, section := range scene.world.sections {
		if len(section.geometry.Guides) != 0 {
			t.Fatal("neighbor retained a copy of the retired guide")
		}
	}
}
