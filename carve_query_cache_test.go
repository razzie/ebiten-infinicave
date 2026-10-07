package infinicave

import (
	"reflect"
	"testing"
)

func TestCarveReusesUnchangedFormationBoundaries(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{
		terrainRect(.1, .2, .4, .6), terrainRect(.7, -.1, .2, .3),
	}})
	untouched := rockAt(t, scene, V{.8, -.95}).Hit.FormationID
	q := scene.world.queryIndex()
	before := q.formations[untouched.object]
	edges := q.formationEdges[untouched.object]
	if before.Complete {
		t.Fatal("fixture should have unloaded continuation")
	}
	if _, err := scene.CarveCircle(V{.3, -.5}, .05); err != nil {
		t.Fatal(err)
	}
	checkReuse := func(complete bool) {
		t.Helper()
		q = scene.world.queryIndex()
		f := q.formations[untouched.object]
		if f.ID != untouched || f.Complete != complete || &f.Polygons[0][0] != &before.Polygons[0][0] ||
			&q.formationEdges[untouched.object][0] != &edges[0] {
			t.Fatal("unchanged formation lost its cached union or coverage metadata")
		}
	}
	checkReuse(false)
	// Empty continuation changes completeness without changing the union.
	scene.world.sections[1] = &worldSection{geometry: prepareTerrainGeometry(sectionData{id: 1}, 0)}
	scene.world.revision++
	checkReuse(true)
	delete(scene.world.sections, 1)
	scene.world.revision++
	checkReuse(false)

	// A forced full reconstruction must produce the same IDs and boundaries.
	formations, boundaries := q.formations, q.formationEdges
	q.formationBlocks = nil
	q.ready = false
	q = scene.world.queryIndex()
	if !reflect.DeepEqual(q.formations, formations) || !reflect.DeepEqual(q.formationEdges, boundaries) {
		t.Fatal("cached query rebuild differs from full reconstruction")
	}
}
