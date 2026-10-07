package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCarveCircleHoleAndIdentity(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{
		terrainRect(.1, .1, .6, .6), terrainRect(.8, .2, .1, .1),
	}, Guides: []Guide{terrain.SplineGuide([]V{{X: .2, Y: .2}, {X: .6, Y: .2}}, 1)}})
	parent := rockAt(t, scene, V{X: .4, Y: -.6}).Hit.FormationID
	untouched := rockAt(t, scene, V{X: .85, Y: -.75}).Hit.FormationID
	guide := mustQuery(t, scene, Ray{Origin: V{X: .3, Y: -.8}}, QueryOptions{Targets: TargetGuide}).Hit.GuideID
	result, err := scene.CarveCircle(V{X: .4, Y: -.6}, .1)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Changes) != 1 || result.Changes[0].Before != parent ||
		len(result.Changes[0].Remaining) != 1 || !reflect.DeepEqual(result.SectionIDs, []int64{0}) {
		t.Fatalf("blast result: %+v", result)
	}
	if _, ok := scene.Formation(parent); ok {
		t.Fatal("affected ID did not expire")
	}
	if _, ok := scene.Formation(untouched); !ok {
		t.Fatal("untouched formation ID expired")
	}
	if _, ok := scene.Guide(guide); !ok {
		t.Fatal("guide ID expired")
	}
	f, ok := scene.Formation(result.Changes[0].Remaining[0])
	if !ok || len(f.Polygons) != 2 || f.Contains(V{X: .4, Y: -.6}) || !f.Contains(V{X: .4, Y: -.45}) {
		t.Fatalf("blast hole missing: %+v", f)
	}
	area, holes := 0.0, 0
	for _, poly := range f.Polygons {
		area += geom.PolygonArea(poly)
		if geom.PolygonArea(poly) < 0 {
			holes++
		}
	}
	wantArea := .36 - 96*.1*.1*math.Sin(2*math.Pi/96)/2
	if holes != 1 || math.Abs(area-wantArea) > 1e-9 {
		t.Fatalf("hole winding/area: %d holes, area %v, want %v", holes, area, wantArea)
	}
	geometry, _ := scene.CollisionGeometry(0)
	if geometry.Contains(V{X: .4, Y: -.6}) || rockAt(t, scene, V{X: .4, Y: -.6}).Found {
		t.Fatal("removed blast center is still solid")
	}
	ray := mustQuery(t, scene, Ray{Origin: V{X: .4, Y: -.6}, Direction: V{X: 1, Y: 0}, MaxDistance: .3}, QueryOptions{Targets: TargetRock})
	if !ray.Found || math.Abs(ray.Hit.Distance-.1) > 1e-9 || ray.Hit.Normal.X > -.99 {
		t.Fatalf("ray did not hit blast wall: %+v", ray)
	}
	// Repeating a cut removes no new area and preserves the survivor ID.
	again, err := scene.CarveCircle(V{X: .4, Y: -.6}, .1)
	if err != nil || len(again.Changes) != 0 || len(again.SectionIDs) != 0 {
		t.Fatalf("repeated cut was not a no-op: %+v / %v", again, err)
	}
	if _, ok := scene.Formation(f.ID); !ok {
		t.Fatal("no-op cut expired formation ID")
	}
}

func TestCarveSegmentSplitsAndDestroys(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .1, .8, .8)}})
	parent := rockAt(t, scene, V{X: .2, Y: -.5}).Hit.FormationID
	result, err := scene.CarveSegment(V{X: .5, Y: -.95}, V{X: .5, Y: -.05}, .04)
	if err != nil || !result.Complete || len(result.Changes) != 1 ||
		result.Changes[0].Before != parent || len(result.Changes[0].Remaining) != 2 {
		t.Fatalf("split result: %+v / %v", result, err)
	}
	a := rockAt(t, scene, V{X: .2, Y: -.5}).Hit.FormationID
	b := rockAt(t, scene, V{X: .8, Y: -.5}).Hit.FormationID
	if a == b || a == parent || b == parent || rockAt(t, scene, V{X: .5, Y: -.5}).Found {
		t.Fatal("split pieces not independently queryable")
	}
	// A second cut splits the left piece; the right piece's ID survives.
	second, err := scene.CarveSegment(V{X: .05, Y: -.5}, V{X: .5, Y: -.5}, .04)
	if err != nil || len(second.Changes) != 1 || second.Changes[0].Before != a || len(second.Changes[0].Remaining) != 2 {
		t.Fatalf("second split result: %+v / %v", second, err)
	}
	if _, ok := scene.Formation(b); !ok {
		t.Fatal("unaffected split piece lost its ID")
	}
	dead := rockAt(t, scene, V{X: .8, Y: -.5}).Hit.FormationID
	third, err := scene.CarveCircle(V{X: .75, Y: -.5}, .6)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range third.Changes {
		if change.Before == dead {
			found = true
			if len(change.Remaining) != 0 {
				t.Fatal("destroyed formation has survivors")
			}
		}
	}
	if !found {
		t.Fatalf("destroyed formation omitted: %+v", third)
	}
}

func TestCarveOneCutCreatesThreeParts(t *testing.T) {
	// A comb: remove its backbone to detach three independent teeth.
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{
		terrainRect(.2, .15, .1, .7), terrainRect(.3, .15, .4, .1),
		terrainRect(.3, .45, .4, .1), terrainRect(.3, .75, .4, .1),
	}})
	result, err := scene.CarveSegment(V{X: .25, Y: -.9}, V{X: .25, Y: -.1}, .12)
	if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 3 {
		t.Fatalf("three-way split: %+v / %v", result, err)
	}
	ids := make(map[FormationID]bool)
	for _, y := range []float64{-.8, -.5, -.2} {
		ids[rockAt(t, scene, V{X: .5, Y: y}).Hit.FormationID] = true
	}
	if len(ids) != 3 {
		t.Fatal("three detached teeth were rejoined by old identity")
	}
}

func TestCarveBlastSplitsFormation(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .35, .8, .1)}})
	result, err := scene.CarveCircle(V{X: .5, Y: -.6}, .15)
	if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 2 {
		t.Fatalf("blast split: %+v / %v", result, err)
	}
	if rockAt(t, scene, V{X: .2, Y: -.6}).Hit.FormationID == rockAt(t, scene, V{X: .8, Y: -.6}).Hit.FormationID {
		t.Fatal("blast pieces remained connected")
	}
}

func TestCarveGeneratedTerrainAndOverlappingCuts(t *testing.T) {
	data := []terrain.SectionData{terrain.BuildSection(42, 0), terrain.BuildSection(42, 1)}
	scene := queryScene(data...)
	baseline := []CollisionGeometry{
		terrain.PrepareTerrainGeometry(data[0], 0).Collision,
		terrain.PrepareTerrainGeometry(data[1], 0).Collision,
	}
	holes := []Hole{
		{Shape: HoleCircle, Center: V{X: .4, Y: -.95}, Radius: .12},
		{Shape: HoleSegment, Start: V{X: .2, Y: -.85}, End: V{X: .7, Y: -1.15}, Width: .035},
		{Shape: HoleCircle, Center: V{X: .42, Y: -1.08}, Radius: .06},
	}
	for _, hole := range holes {
		if _, err := scene.Carve(hole); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		for owner := range data {
			collision, _ := scene.CollisionGeometry(-int64(owner))
			for y := .023; y < 1; y += .053 {
				for x := .023; x < 1; x += .053 {
					p := V{X: x, Y: y + terrain.SectionTop(int64(owner))}
					want := baseline[owner].Contains(p)
					for _, hole := range holes {
						cut, _ := terrain.CutFromHole(hole)
						if (CollisionGeometry{Polygons: [][]V{cut.Poly}}).Contains(p) {
							want = false
							break
						}
					}
					r := rockAt(t, scene, p)
					if r.Found != want || !r.Complete || collision.Contains(p) != want {
						t.Fatalf("generated cuts disagree at %v: query=%+v collision=%v want=%v", p, r, collision.Contains(p), want)
					}
				}
			}
			for _, cell := range scene.world.sections[int64(owner)].geometry.Grid {
				if geom.PolygonArea(cell.Polygon) <= 0 || len(geom.Triangulate(cell.Polygon)) != len(cell.Polygon)-2 {
					t.Fatal("cut produced an invalid render face")
				}
			}
		}
	}
	check()
	// Regenerating every section must reproduce the same carved geometry.
	for _, d := range data {
		mesh := render.SectionMesh{ID: d.ID, Geometry: terrain.PrepareTerrainGeometry(d, 0)}
		scene.applyStoredCuts(&mesh)
		scene.world.sections[d.ID].geometry = mesh.Geometry
	}
	scene.world.revision++
	check()
}

func TestCarveAcrossSeamsAndReload(t *testing.T) {
	data := []terrain.SectionData{
		{ID: 0, Foreground: RockGrid{terrainRect(.2, -.2, .6, .4)}},
		{ID: 1, Foreground: RockGrid{terrainRect(.2, .8, .6, .4)}},
	}
	scene := queryScene(data...)
	parent := rockAt(t, scene, V{X: .3, Y: -.95}).Hit.FormationID
	result, err := scene.CarveSegment(V{X: .5, Y: -1.3}, V{X: .5, Y: -.7}, .06)
	if err != nil || !result.Complete || len(result.Changes) != 1 ||
		result.Changes[0].Before != parent || len(result.Changes[0].Remaining) != 2 ||
		!reflect.DeepEqual(result.SectionIDs, []int64{0, -1}) {
		t.Fatalf("cross-section split: %+v / %v", result, err)
	}
	left := rockAt(t, scene, V{X: .3, Y: -.95}).Hit.FormationID
	for _, p := range []V{{X: .3, Y: -1.05}, {X: .7, Y: -.95}, {X: .7, Y: -1.05}} {
		r := rockAt(t, scene, p)
		if !r.Found || (r.Hit.FormationID == left) != (p.X < .5) {
			t.Fatalf("split changed across seam at %v: %+v", p, r)
		}
	}
	// Evict and reload a neighbor exactly as the worker/upload handoff does.
	delete(scene.world.sections, 1)
	scene.world.revision++
	scene.world.queries.expire(scene.world)
	mesh := render.SectionMesh{ID: 1, Geometry: terrain.PrepareTerrainGeometry(data[1], 0)}
	scene.applyStoredCuts(&mesh)
	scene.world.sections[1] = &worldSection{geometry: mesh.Geometry}
	scene.world.revision++
	if rockAt(t, scene, V{X: .5, Y: -1.05}).Found || rockAt(t, scene, V{X: .3, Y: -1.05}).Hit.FormationID != left {
		t.Fatal("evicted cut was lost or survivor identity changed")
	}
	for _, id := range []int64{0, -1} {
		collision, _ := scene.CollisionGeometry(id)
		if collision.Contains(V{X: .5, Y: -1}) {
			t.Fatal("collision seam closed the hole")
		}
	}
}

func TestCarveUnloadedTerrainAndQueuedMesh(t *testing.T) {
	scene := queryScene(terrain.SectionData{})
	result, err := scene.CarveCircle(V{X: .4, Y: -2.5}, .1)
	if err != nil || result.Complete || len(result.Changes) != 0 {
		t.Fatalf("unloaded blast: %+v / %v", result, err)
	}
	data := terrain.SectionData{ID: 2, Foreground: RockGrid{terrainRect(.2, .2, .4, .6)}}
	mesh := render.SectionMesh{ID: 2, Geometry: terrain.PrepareTerrainGeometry(data, 0)}
	scene.applyStoredCuts(&mesh)
	if mesh.Geometry.Collision.Contains(V{X: .4, Y: -2.5}) || len(mesh.Foreground.Faces.Indices) == 0 {
		t.Fatal("queued mesh ignored stored blast")
	}
	checkMesh(t, mesh.Foreground.Faces)
	// A cut to an incomplete formation must not claim a definitive split.
	partial := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.2, -.1, .4, .3)}})
	result, err = partial.CarveCircle(V{X: .4, Y: -.95}, .02)
	if err != nil || result.Complete || len(result.Changes) != 1 || result.Changes[0].Complete {
		t.Fatalf("partial formation reported complete: %+v / %v", result, err)
	}
}

func TestCarveConcaveFaceAndRectangleEnds(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRock([]V{
		{X: .1, Y: .1}, {X: .8, Y: .1}, {X: .8, Y: .3}, {X: .3, Y: .3}, {X: .3, Y: .8}, {X: .1, Y: .8},
	})}})
	_, err := scene.CarveSegment(V{X: .15, Y: -.75}, V{X: .45, Y: -.75}, .08)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		p     V
		solid bool
	}{
		{V{X: .2, Y: -.75}, false}, {V{X: .125, Y: -.75}, true},
		{V{X: .5, Y: -.8}, true}, {V{X: .5, Y: -.5}, false}, {V{X: .2, Y: -.3}, true},
	} {
		if rockAt(t, scene, tc.p).Found != tc.solid {
			t.Fatalf("concave rectangle cut at %v, want solid=%v", tc.p, tc.solid)
		}
	}
}

func TestCarveValidationAndCoverage(t *testing.T) {
	scene := queryScene(terrain.SectionData{})
	for _, tc := range []struct {
		center V
		radius float64
	}{
		{V{}, 0}, {V{}, -1}, {V{}, math.NaN()}, {V{}, math.Inf(1)},
		{V{X: math.NaN(), Y: 0}, .1}, {V{X: math.MaxFloat64, Y: 0}, math.MaxFloat64},
		{V{}, math.SmallestNonzeroFloat64},
	} {
		if _, err := scene.CarveCircle(tc.center, tc.radius); err == nil {
			t.Fatalf("invalid circle accepted: %+v", tc)
		}
	}
	for _, tc := range []struct {
		start, end V
		width      float64
	}{
		{V{}, V{}, .1}, {V{}, V{X: 1, Y: 0}, 0}, {V{}, V{X: 1, Y: 0}, -1},
		{V{}, V{X: 1, Y: 0}, math.NaN()}, {V{}, V{X: math.Inf(1), Y: 0}, .1},
	} {
		if _, err := scene.CarveSegment(tc.start, tc.end, tc.width); err == nil {
			t.Fatalf("invalid segment accepted: %+v", tc)
		}
	}
	if len(scene.world.cuts) != 0 {
		t.Fatal("invalid cuts mutated world")
	}
	for _, tc := range []struct {
		sections []int64
		min, max V
		complete bool
	}{
		{[]int64{0, 1}, V{X: .2, Y: -1.5}, V{X: .4, Y: -.5}, true},
		{[]int64{0, 2}, V{X: .2, Y: -2.5}, V{X: .4, Y: -.5}, false},
		{[]int64{0}, V{X: .2, Y: -1}, V{X: .4, Y: 0}, true},
		{nil, V{X: 2, Y: -10}, V{X: 3, Y: -9}, true},
		{nil, V{X: .2, Y: 1}, V{X: .4, Y: 2}, true},
		{[]int64{0}, V{X: .2, Y: -1e12}, V{X: .4, Y: 0}, false},
	} {
		if got := (terrain.RockCut{Min: tc.min, Max: tc.max}).Covered(tc.sections); got != tc.complete {
			t.Fatalf("cut coverage %+v = %v", tc, got)
		}
	}
	scene.closed = true
	if _, err := scene.CarveCircle(V{X: .3, Y: -.5}, .1); err == nil {
		t.Fatal("closed scene accepted cut")
	}
}

func TestCarveOutsideWorldLeavesPaddingAndIdentityAlone(t *testing.T) {
	// This face extends below the world floor in the generation padding.
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.2, .8, .4, .5)}})
	id := rockAt(t, scene, V{X: .4, Y: -.1}).Hit.FormationID
	result, err := scene.CarveCircle(V{X: .4, Y: .15}, .05)
	if err != nil || !result.Complete || len(result.Changes) != 0 || len(result.SectionIDs) != 0 || len(scene.world.cuts) != 0 {
		t.Fatalf("outside-world cut changed terrain: %+v / %v", result, err)
	}
	if rockAt(t, scene, V{X: .4, Y: -.1}).Hit.FormationID != id {
		t.Fatal("outside-world cut changed a formation ID")
	}
}

func TestCarveDuringEveryUploadStage(t *testing.T) {
	data := terrain.SectionData{Foreground: RockGrid{terrainRect(.2, .2, .6, .6)}}
	for stage := 0; stage <= 8; stage++ {
		t.Run(string(rune('0'+stage)), func(t *testing.T) {
			scene := queryScene()
			u := &sectionUpload{stage: stage, next: 3, data: render.SectionMesh{ID: 0, Geometry: terrain.PrepareTerrainGeometry(data, 0)}}
			if stage == 3 || stage == 4 {
				u.foreground = ebiten.NewImage(1, 1)
			}
			scene.world.upload = u
			if stage >= 5 {
				scene.world.sections[0] = &worldSection{geometry: u.data.Geometry}
			}
			_, err := scene.CarveCircle(V{X: .5, Y: -.5}, .08)
			if err != nil {
				t.Fatal(err)
			}
			if u.data.Geometry.Collision.Contains(V{X: .5, Y: -.5}) {
				t.Fatal("upload geometry ignored cut")
			}
			if stage >= 2 && stage <= 4 && (u.stage != 2 || u.next != 0 || u.foreground != nil) {
				t.Fatal("partially drawn foreground was not restarted")
			}
			if stage >= 5 && scene.world.sections[0].geometry.Collision.Contains(V{X: .5, Y: -.5}) {
				t.Fatal("published terrain ignored cut during vegetation upload")
			}
		})
	}
}

func TestCarveRemovesWholeUnsupportedMushroomsIndividually(t *testing.T) {
	group := MushroomGroup{Mushrooms: []Mushroom{
		testGroundedMushroom(.3, .4), testGroundedMushroom(.5, .4), testGroundedMushroom(.7, .4),
	}}
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .4, .8, .3)}, Mushrooms: []MushroomGroup{group}})
	if _, err := scene.CarveCircle(V{X: .3, Y: -.6}, .014); err != nil {
		t.Fatal(err)
	}
	plants := scene.world.sections[0].geometry.Vegetation
	if len(plants.Mushrooms) != 1 || !reflect.DeepEqual(plants.Mushrooms[0].Mushrooms, group.Mushrooms[1:]) {
		t.Fatalf("unsupported mushroom was not removed independently: %+v", plants.Mushrooms)
	}
	if len(group.Mushrooms) != 3 || group.Mushrooms[0].Stem[0] != (V{X: .3, Y: .404}) {
		t.Fatal("carving modified source plant slices")
	}
	if _, err := scene.CarveSegment(V{X: .1, Y: -.6}, V{X: .9, Y: -.6}, .03); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.Vegetation.Mushrooms) != 0 {
		t.Fatal("empty mushroom groups were retained")
	}
}

func TestCarveRetainsMushroomWhileBuriedRootStillTouchesRock(t *testing.T) {
	m := testGroundedMushroom(.5, .4)
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .4, .8, .3)}, Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{m}}}})
	// Anchor is removed, but the buried root still has support.
	if _, err := scene.CarveCircle(V{X: .5, Y: -.6}, .0025); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scene.world.sections[0].geometry.Vegetation.Mushrooms, []MushroomGroup{{Mushrooms: []Mushroom{m}}}) {
		t.Fatal("mushroom with remaining root contact was removed or partially cut")
	}
	// A cut through the cap alone does not remove a supported mushroom.
	if _, err := scene.CarveCircle(m.CapCenter.Add(V{Y: -1}), .009); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.Vegetation.Mushrooms) != 1 {
		t.Fatal("cap overlap removed a grounded mushroom")
	}
}

func TestCarveSplitsBothVineLayersBetweenSparseSamples(t *testing.T) {
	vine := Vine{Parent: -1, Points: []VinePoint{{P: V{X: .1, Y: .5}, Radius: .003}, {P: V{X: .9, Y: .5}, Radius: .007}}}
	front := vine
	front.Foreground = true
	scene := queryScene(terrain.SectionData{Vines: []Vine{vine}, ForegroundVines: []Vine{front}})
	result, err := scene.CarveCircle(V{X: .5, Y: -.5}, .1)
	if err != nil || len(result.Changes) != 0 || len(result.SectionIDs) != 0 {
		t.Fatalf("vine-only cut should not report rock changes: %+v / %v", result, err)
	}
	plants := scene.world.sections[0].geometry.Vegetation
	for _, vines := range [][]Vine{plants.Vines, plants.ForegroundVines} {
		if len(vines) != 2 {
			t.Fatalf("sparse vine did not split: %+v", vines)
		}
		left, right := vines[0].Points[len(vines[0].Points)-1], vines[1].Points[0]
		if left.P.Sub(V{X: .4, Y: .5}).Len() > 1e-10 || right.P.Sub(V{X: .6, Y: .5}).Len() > 1e-10 ||
			math.Abs(left.Radius-.0045) > 1e-10 || math.Abs(right.Radius-.0055) > 1e-10 {
			t.Fatalf("cut endpoints/radii were not interpolated: %+v / %+v", left, right)
		}
	}
	if len(vine.Points) != 2 || vine.Points[1].P != (V{X: .9, Y: .5}) {
		t.Fatal("carving modified the original vine")
	}
	first := plants
	if _, err := scene.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, scene.world.sections[0].geometry.Vegetation) {
		t.Fatal("repeated hole changed surviving vines or added duplicate masks")
	}
}
