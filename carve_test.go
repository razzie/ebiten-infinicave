package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func rockAt(t *testing.T, scene *Scene, p V) QueryResult {
	t.Helper()
	return mustQuery(t, scene, Ray{Origin: p}, QueryOptions{Targets: TargetRock})
}

func TestCarveCircleHoleAndIdentity(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{
		hoverRect(.1, .1, .6, .6), hoverRect(.8, .2, .1, .1),
	}, guides: []Guide{splineGuide([]V{{.2, .2}, {.6, .2}}, 1)}})
	parent := rockAt(t, scene, V{.4, -.6}).Hit.FormationID
	untouched := rockAt(t, scene, V{.85, -.75}).Hit.FormationID
	guide := mustQuery(t, scene, Ray{Origin: V{.3, -.8}}, QueryOptions{Targets: TargetGuide}).Hit.GuideID
	result, err := scene.CarveCircle(V{.4, -.6}, .1)
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
	if !ok || len(f.Polygons) != 2 || f.Contains(V{.4, -.6}) || !f.Contains(V{.4, -.45}) {
		t.Fatalf("blast hole missing: %+v", f)
	}
	area, holes := 0.0, 0
	for _, poly := range f.Polygons {
		area += faceArea(poly)
		if faceArea(poly) < 0 {
			holes++
		}
	}
	wantArea := .36 - 96*.1*.1*math.Sin(2*math.Pi/96)/2
	if holes != 1 || math.Abs(area-wantArea) > 1e-9 {
		t.Fatalf("hole winding/area: %d holes, area %v, want %v", holes, area, wantArea)
	}
	geometry, _ := scene.CollisionGeometry(0)
	if geometry.Contains(V{.4, -.6}) || rockAt(t, scene, V{.4, -.6}).Found {
		t.Fatal("removed blast center is still solid")
	}
	ray := mustQuery(t, scene, Ray{Origin: V{.4, -.6}, Direction: V{1, 0}, MaxDistance: .3}, QueryOptions{Targets: TargetRock})
	if !ray.Found || math.Abs(ray.Hit.Distance-.1) > 1e-9 || ray.Hit.Normal.X > -.99 {
		t.Fatalf("ray did not hit blast wall: %+v", ray)
	}
	h := scene.world.sections[0].geometry
	if h.hit(V{.4, .4}).geometry != nil {
		t.Fatal("hover still selects blast hole")
	}
	// Repeating a cut removes no new area and preserves the survivor ID.
	again, err := scene.CarveCircle(V{.4, -.6}, .1)
	if err != nil || len(again.Changes) != 0 || len(again.SectionIDs) != 0 {
		t.Fatalf("repeated cut was not a no-op: %+v / %v", again, err)
	}
	if _, ok := scene.Formation(f.ID); !ok {
		t.Fatal("no-op cut expired formation ID")
	}
}

func TestCarveSegmentSplitsAndDestroys(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{hoverRect(.1, .1, .8, .8)}})
	parent := rockAt(t, scene, V{.2, -.5}).Hit.FormationID
	result, err := scene.CarveSegment(V{.5, -.95}, V{.5, -.05}, .04)
	if err != nil || !result.Complete || len(result.Changes) != 1 ||
		result.Changes[0].Before != parent || len(result.Changes[0].Remaining) != 2 {
		t.Fatalf("split result: %+v / %v", result, err)
	}
	a := rockAt(t, scene, V{.2, -.5}).Hit.FormationID
	b := rockAt(t, scene, V{.8, -.5}).Hit.FormationID
	if a == b || a == parent || b == parent || rockAt(t, scene, V{.5, -.5}).Found {
		t.Fatal("split pieces not independently queryable")
	}
	// A second cut splits the left piece; the right piece's ID survives.
	second, err := scene.CarveSegment(V{.05, -.5}, V{.5, -.5}, .04)
	if err != nil || len(second.Changes) != 1 || second.Changes[0].Before != a || len(second.Changes[0].Remaining) != 2 {
		t.Fatalf("second split result: %+v / %v", second, err)
	}
	if _, ok := scene.Formation(b); !ok {
		t.Fatal("unaffected split piece lost its ID")
	}
	dead := rockAt(t, scene, V{.8, -.5}).Hit.FormationID
	third, err := scene.CarveCircle(V{.75, -.5}, .6)
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
	scene := queryScene(sectionData{foreground: RockGrid{
		hoverRect(.2, .15, .1, .7), hoverRect(.3, .15, .4, .1),
		hoverRect(.3, .45, .4, .1), hoverRect(.3, .75, .4, .1),
	}})
	result, err := scene.CarveSegment(V{.25, -.9}, V{.25, -.1}, .12)
	if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 3 {
		t.Fatalf("three-way split: %+v / %v", result, err)
	}
	ids := make(map[FormationID]bool)
	for _, y := range []float64{-.8, -.5, -.2} {
		ids[rockAt(t, scene, V{.5, y}).Hit.FormationID] = true
	}
	if len(ids) != 3 {
		t.Fatal("three detached teeth were rejoined by old identity")
	}
}

func TestCarveBlastSplitsFormation(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{hoverRect(.1, .35, .8, .1)}})
	result, err := scene.CarveCircle(V{.5, -.6}, .15)
	if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 2 {
		t.Fatalf("blast split: %+v / %v", result, err)
	}
	if rockAt(t, scene, V{.2, -.6}).Hit.FormationID == rockAt(t, scene, V{.8, -.6}).Hit.FormationID {
		t.Fatal("blast pieces remained connected")
	}
}

func TestCarveGeneratedTerrainAndOverlappingCuts(t *testing.T) {
	data := []sectionData{buildSection(42, 0), buildSection(42, 1)}
	scene := queryScene(data...)
	baseline := []CollisionGeometry{
		prepareTerrainGeometry(data[0], 0).collision,
		prepareTerrainGeometry(data[1], 0).collision,
	}
	holes := []Hole{
		{Shape: HoleCircle, Center: V{.4, -.95}, Radius: .12},
		{Shape: HoleSegment, Start: V{.2, -.85}, End: V{.7, -1.15}, Width: .035},
		{Shape: HoleCircle, Center: V{.42, -1.08}, Radius: .06},
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
					p := V{x, y + sectionTop(int64(owner))}
					want := baseline[owner].Contains(p)
					for _, hole := range holes {
						cut, _ := hole.rockCut()
						if (CollisionGeometry{Polygons: [][]V{cut.poly}}).Contains(p) {
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
			for _, cell := range scene.world.sections[int64(owner)].geometry.grid {
				if faceArea(cell.Polygon) <= 0 || len(faceTriangles(cell.Polygon)) != len(cell.Polygon)-2 {
					t.Fatal("cut produced an invalid render face")
				}
			}
		}
	}
	check()
	// Regenerating every section must reproduce the same carved geometry.
	for _, d := range data {
		mesh := sectionMesh{id: d.id, geometry: prepareTerrainGeometry(d, 0)}
		scene.applyStoredCuts(&mesh)
		scene.world.sections[d.id].geometry = mesh.geometry
	}
	scene.world.revision++
	check()
}

func TestCarveAcrossSeamsAndReload(t *testing.T) {
	data := []sectionData{
		{id: 0, foreground: RockGrid{hoverRect(.2, -.2, .6, .4)}},
		{id: 1, foreground: RockGrid{hoverRect(.2, .8, .6, .4)}},
	}
	scene := queryScene(data...)
	parent := rockAt(t, scene, V{.3, -.95}).Hit.FormationID
	result, err := scene.CarveSegment(V{.5, -1.3}, V{.5, -.7}, .06)
	if err != nil || !result.Complete || len(result.Changes) != 1 ||
		result.Changes[0].Before != parent || len(result.Changes[0].Remaining) != 2 ||
		!reflect.DeepEqual(result.SectionIDs, []int64{0, -1}) {
		t.Fatalf("cross-section split: %+v / %v", result, err)
	}
	left := rockAt(t, scene, V{.3, -.95}).Hit.FormationID
	for _, p := range []V{{.3, -1.05}, {.7, -.95}, {.7, -1.05}} {
		r := rockAt(t, scene, p)
		if !r.Found || (r.Hit.FormationID == left) != (p.X < .5) {
			t.Fatalf("split changed across seam at %v: %+v", p, r)
		}
	}
	// Evict and reload a neighbor exactly as the worker/upload handoff does.
	delete(scene.world.sections, 1)
	scene.world.revision++
	scene.world.queries.expire(scene.world)
	mesh := sectionMesh{id: 1, geometry: prepareTerrainGeometry(data[1], 0)}
	scene.applyStoredCuts(&mesh)
	scene.world.sections[1] = &worldSection{geometry: mesh.geometry}
	scene.world.revision++
	if rockAt(t, scene, V{.5, -1.05}).Found || rockAt(t, scene, V{.3, -1.05}).Hit.FormationID != left {
		t.Fatal("evicted cut was lost or survivor identity changed")
	}
	for _, id := range []int64{0, -1} {
		collision, _ := scene.CollisionGeometry(id)
		if collision.Contains(V{.5, -1}) {
			t.Fatal("collision seam closed the hole")
		}
	}
}

func TestCarveUnloadedTerrainAndQueuedMesh(t *testing.T) {
	scene := queryScene(sectionData{})
	result, err := scene.CarveCircle(V{.4, -2.5}, .1)
	if err != nil || result.Complete || len(result.Changes) != 0 {
		t.Fatalf("unloaded blast: %+v / %v", result, err)
	}
	data := sectionData{id: 2, foreground: RockGrid{hoverRect(.2, .2, .4, .6)}}
	mesh := sectionMesh{id: 2, geometry: prepareTerrainGeometry(data, 0)}
	scene.applyStoredCuts(&mesh)
	if mesh.geometry.collision.Contains(V{.4, -2.5}) || len(mesh.foreground.faces.indices) == 0 {
		t.Fatal("queued mesh ignored stored blast")
	}
	checkMesh(t, mesh.foreground.faces)
	// A cut to an incomplete formation must not claim a definitive split.
	partial := queryScene(sectionData{foreground: RockGrid{hoverRect(.2, -.1, .4, .3)}})
	result, err = partial.CarveCircle(V{.4, -.95}, .02)
	if err != nil || result.Complete || len(result.Changes) != 1 || result.Changes[0].Complete {
		t.Fatalf("partial formation reported complete: %+v / %v", result, err)
	}
}

func TestCarveConcaveFaceAndRectangleEnds(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{hoverRock([]V{
		{.1, .1}, {.8, .1}, {.8, .3}, {.3, .3}, {.3, .8}, {.1, .8},
	})}})
	_, err := scene.CarveSegment(V{.15, -.75}, V{.45, -.75}, .08)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		p     V
		solid bool
	}{
		{V{.2, -.75}, false}, {V{.125, -.75}, true},
		{V{.5, -.8}, true}, {V{.5, -.5}, false}, {V{.2, -.3}, true},
	} {
		if rockAt(t, scene, tc.p).Found != tc.solid {
			t.Fatalf("concave rectangle cut at %v, want solid=%v", tc.p, tc.solid)
		}
	}
}

func TestCarveValidationAndCoverage(t *testing.T) {
	scene := queryScene(sectionData{})
	for _, tc := range []struct {
		center V
		radius float64
	}{
		{V{}, 0}, {V{}, -1}, {V{}, math.NaN()}, {V{}, math.Inf(1)},
		{V{math.NaN(), 0}, .1}, {V{math.MaxFloat64, 0}, math.MaxFloat64},
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
		{V{}, V{}, .1}, {V{}, V{1, 0}, 0}, {V{}, V{1, 0}, -1},
		{V{}, V{1, 0}, math.NaN()}, {V{}, V{math.Inf(1), 0}, .1},
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
		{[]int64{0, 1}, V{.2, -1.5}, V{.4, -.5}, true},
		{[]int64{0, 2}, V{.2, -2.5}, V{.4, -.5}, false},
		{[]int64{0}, V{.2, -1}, V{.4, 0}, true},
		{nil, V{2, -10}, V{3, -9}, true},
		{nil, V{.2, 1}, V{.4, 2}, true},
		{[]int64{0}, V{.2, -1e12}, V{.4, 0}, false},
	} {
		if got := (rockCut{min: tc.min, max: tc.max}).covered(tc.sections); got != tc.complete {
			t.Fatalf("cut coverage %+v = %v", tc, got)
		}
	}
	scene.closed = true
	if _, err := scene.CarveCircle(V{.3, -.5}, .1); err == nil {
		t.Fatal("closed scene accepted cut")
	}
}

func TestCarveOutsideWorldLeavesPaddingAndIdentityAlone(t *testing.T) {
	// This face extends below the world floor in the generation padding.
	scene := queryScene(sectionData{foreground: RockGrid{hoverRect(.2, .8, .4, .5)}})
	id := rockAt(t, scene, V{.4, -.1}).Hit.FormationID
	result, err := scene.CarveCircle(V{.4, .15}, .05)
	if err != nil || !result.Complete || len(result.Changes) != 0 || len(result.SectionIDs) != 0 || len(scene.world.cuts) != 0 {
		t.Fatalf("outside-world cut changed terrain: %+v / %v", result, err)
	}
	if rockAt(t, scene, V{.4, -.1}).Hit.FormationID != id {
		t.Fatal("outside-world cut changed a formation ID")
	}
}

func TestCarveDuringEveryUploadStage(t *testing.T) {
	data := sectionData{foreground: RockGrid{hoverRect(.2, .2, .6, .6)}}
	for stage := 0; stage <= 8; stage++ {
		t.Run(string(rune('0'+stage)), func(t *testing.T) {
			scene := queryScene()
			u := &sectionUpload{stage: stage, next: 3, data: sectionMesh{id: 0, geometry: prepareTerrainGeometry(data, 0)}}
			if stage == 3 || stage == 4 {
				u.foreground = ebiten.NewImage(1, 1)
			}
			scene.world.upload = u
			if stage >= 5 {
				scene.world.sections[0] = &worldSection{geometry: u.data.geometry}
			}
			_, err := scene.CarveCircle(V{.5, -.5}, .08)
			if err != nil {
				t.Fatal(err)
			}
			if u.data.geometry.collision.Contains(V{.5, -.5}) {
				t.Fatal("upload geometry ignored cut")
			}
			if stage >= 2 && stage <= 4 && (u.stage != 2 || u.next != 0 || u.foreground != nil) {
				t.Fatal("partially drawn foreground was not restarted")
			}
			if stage >= 5 && scene.world.sections[0].geometry.collision.Contains(V{.5, -.5}) {
				t.Fatal("published terrain ignored cut during vegetation upload")
			}
		})
	}
}
