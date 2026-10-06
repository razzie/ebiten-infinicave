package infinicave

import (
	"math"
	"reflect"
	"testing"
)

func queryScene(data ...sectionData) *Scene {
	w := &world{sections: make(map[int64]*worldSection)}
	for _, d := range data {
		w.sections[d.id] = &worldSection{geometry: prepareTerrainGeometry(d, .01)}
	}
	return &Scene{world: w}
}

func mustQuery(t *testing.T, scene *Scene, ray Ray, options QueryOptions) QueryResult {
	t.Helper()
	result, err := scene.Query(ray, options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestQueryFormationPointRayAndFetch(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{
		hoverRect(.2, .2, .1, .2), hoverRect(.3, .2, .1, .2),
		hoverRect(.6, .2, .1, .2),
	}})
	options := QueryOptions{Targets: TargetRock}
	point := mustQuery(t, scene, Ray{Origin: V{.25, -.7}}, options)
	if !point.Found || !point.Complete || !point.Hit.StartedInside || point.Hit.Distance != 0 || point.Hit.Normal != (V{}) {
		t.Fatalf("point query: %+v", point)
	}
	otherFace := mustQuery(t, scene, Ray{Origin: V{.35, -.7}}, options)
	if otherFace.Hit.FormationID != point.Hit.FormationID {
		t.Fatal("connected cells have different formation IDs")
	}
	separate := mustQuery(t, scene, Ray{Origin: V{.65, -.7}}, options)
	if !separate.Found || separate.Hit.FormationID == point.Hit.FormationID {
		t.Fatal("disconnected formations have the same ID")
	}
	hit := mustQuery(t, scene, Ray{Origin: V{.1, -.7}, Direction: V{10, 0}, MaxDistance: .8}, options)
	if !hit.Found || !hit.Complete || hit.Hit.FormationID != point.Hit.FormationID ||
		math.Abs(hit.Hit.Distance-.1) > 1e-9 || hit.Hit.Point.Sub(V{.2, -.7}).Len() > 1e-9 || hit.Hit.Normal != (V{-1, 0}) {
		t.Fatalf("ray query: %+v", hit)
	}
	short := mustQuery(t, scene, Ray{Origin: V{.1, -.7}, Direction: V{1, 0}, MaxDistance: .09}, options)
	if short.Found || !short.Complete {
		t.Fatalf("short cast should miss: %+v", short)
	}
	f, ok := scene.Formation(hit.Hit.FormationID)
	if !ok || !f.Complete || len(f.Polygons) != 1 || !f.Contains(V{.35, -.7}) || f.Contains(V{.65, -.7}) ||
		!reflect.DeepEqual(f.SectionIDs, []int64{0}) {
		t.Fatalf("formation fetch: %+v / %v", f, ok)
	}
	area := faceArea(f.Polygons[0])
	if math.Abs(area-.04) > 1e-10 {
		t.Fatalf("union area %v, want .04", area)
	}
	f.Polygons[0][0].X = 100
	f.SectionIDs[0] = 100
	again, _ := scene.Formation(hit.Hit.FormationID)
	if !again.Contains(V{.25, -.7}) || again.SectionIDs[0] != 0 {
		t.Fatal("fetch returned mutable cached slices")
	}
	// Direction zero overrides a positive MaxDistance, and vice versa.
	for _, ray := range []Ray{
		{Origin: V{.25, -.7}, MaxDistance: 10},
		{Origin: V{.25, -.7}, Direction: V{1, 0}},
	} {
		if result := mustQuery(t, scene, ray, options); !result.Hit.StartedInside || result.Hit.Distance != 0 {
			t.Fatalf("point-query convention: %+v", result)
		}
	}
}

func TestQueryHoleAndConcavity(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{
		hoverRect(.2, .2, .3, .05), hoverRect(.2, .45, .3, .05),
		hoverRect(.2, .25, .05, .2), hoverRect(.45, .25, .05, .2),
	}})
	options := QueryOptions{Targets: TargetRock}
	if r := mustQuery(t, scene, Ray{Origin: V{.3, -.65}}, options); r.Found || !r.Complete {
		t.Fatalf("point in hole selected rock: %+v", r)
	}
	r := mustQuery(t, scene, Ray{Origin: V{.3, -.65}, Direction: V{1, 0}, MaxDistance: .5}, options)
	if !r.Found || math.Abs(r.Hit.Distance-.15) > 1e-9 || r.Hit.Normal != (V{-1, 0}) {
		t.Fatalf("ray from hole: %+v", r)
	}
	f, _ := scene.Formation(r.Hit.FormationID)
	if len(f.Polygons) != 2 || f.Contains(V{.3, -.65}) {
		t.Fatal("formation fetch lost its hole")
	}
	// Hit testing stays exact even when collision contours are simplified.
	concave := queryScene(sectionData{foreground: RockGrid{hoverRock([]V{
		{.2, .2}, {.4, .2}, {.4, .3}, {.3, .3}, {.3, .4}, {.2, .4},
	})}})
	if r := mustQuery(t, concave, Ray{Origin: V{.35, -.65}}, options); r.Found {
		t.Fatal("empty concavity selected rock")
	}
}

func TestQueryGuidesRadiusNearestAndFetch(t *testing.T) {
	g := splineGuide([]V{{.3, .2}, {.3, .4}}, 1)
	scene := queryScene(sectionData{guides: []Guide{g}, foreground: RockGrid{hoverRect(.5, .2, .1, .2)}})
	for _, tc := range []struct {
		options  QueryOptions
		kind     TargetMask
		distance float64
	}{
		{QueryOptions{}, TargetGuide, .2},
		{QueryOptions{Targets: TargetRock}, TargetRock, .4},
		{QueryOptions{Targets: TargetGuide, GuideRadius: .02}, TargetGuide, .18},
	} {
		r := mustQuery(t, scene, Ray{Origin: V{.1, -.7}, Direction: V{1, 0}, MaxDistance: .8}, tc.options)
		if !r.Found || !r.Complete || r.Hit.Kind != tc.kind || math.Abs(r.Hit.Distance-tc.distance) > 1e-9 ||
			r.Hit.Normal.Sub(V{-1, 0}).Len() > 1e-9 {
			t.Fatalf("nearest target: %+v, want %v at %v", r, tc.kind, tc.distance)
		}
	}
	r := mustQuery(t, scene, Ray{Origin: V{.31, -.7}}, QueryOptions{Targets: TargetGuide, GuideRadius: .02})
	if !r.Found || !r.Hit.StartedInside || r.Hit.Normal != (V{}) {
		t.Fatalf("guide proximity: %+v", r)
	}
	guide, ok := scene.Guide(r.Hit.GuideID)
	if !ok || math.Abs(guide.S[len(guide.S)-1]-.2) > 1e-9 || guide.Points[0] != (V{.3, -.8}) {
		t.Fatalf("guide fetch: %+v / %v", guide, ok)
	}
	guide.Points[0].X = 100
	guide.S[0] = 100
	guide, _ = scene.Guide(r.Hit.GuideID)
	if guide.Points[0].X != .3 || guide.S[0] != 0 {
		t.Fatal("guide fetch exposed mutable slices")
	}
	// Both guides use the same seed; their geometry must still distinguish IDs.
	scene2 := queryScene(sectionData{guides: []Guide{
		{Pts: []V{{.3, .2}, {.3, .4}}, Seed: 7},
		{Pts: []V{{.6, .2}, {.6, .4}}, Seed: 7},
	}})
	a := mustQuery(t, scene2, Ray{Origin: V{.3, -.7}}, QueryOptions{Targets: TargetGuide})
	b := mustQuery(t, scene2, Ray{Origin: V{.6, -.7}}, QueryOptions{Targets: TargetGuide})
	if !a.Found || !b.Found || a.Hit.GuideID == b.Hit.GuideID {
		t.Fatal("distinct custom guides with equal seeds were merged")
	}
}

func TestQueryNearestRockAndGuideTie(t *testing.T) {
	for _, guideX := range []float64{.2, .4} {
		scene := queryScene(sectionData{
			foreground: RockGrid{hoverRect(.2, .2, .1, .2)},
			guides:     []Guide{{Pts: []V{{guideX, .2}, {guideX, .4}}}},
		})
		want := TargetRock
		if guideX == .2 {
			want = TargetGuide
		}
		for i := 0; i < 10; i++ { // map iteration must not change tie outcomes
			r := mustQuery(t, scene, Ray{Origin: V{.1, -.7}, Direction: V{1, 0}, MaxDistance: .8}, QueryOptions{})
			if !r.Found || r.Hit.Kind != want || math.Abs(r.Hit.Distance-.1) > 1e-9 {
				t.Fatalf("nearest target or guide tie preference: %+v", r)
			}
		}
	}
}

func TestQueryCapsuleEndCapsTangencyAndCollinear(t *testing.T) {
	a, b := V{.3, -.8}, V{.5, -.8}
	for _, tc := range []struct {
		origin, direction V
		radius, want      float64
	}{
		{V{.1, -.8}, V{1, 0}, 0, .2},
		{V{.1, -.8}, V{1, 0}, .02, .18},
		{V{.1, -.82}, V{1, 0}, .02, .2},
		{V{.4, -.9}, V{0, 1}, .02, .08},
	} {
		d, n, inside, ok := rayCapsule(tc.origin, tc.direction, a, b, tc.radius, 1)
		if !ok || inside || math.Abs(d-tc.want) > 1e-8 || math.Abs(n.Len()-1) > 1e-9 {
			t.Fatalf("capsule %+v: %v %v %v %v", tc, d, n, inside, ok)
		}
	}
	if _, _, _, ok := rayCapsule(V{.1, -.83}, V{1, 0}, a, b, .02, 1); ok {
		t.Fatal("parallel ray outside guide radius hit")
	}
}

func TestQueryAcrossSectionsAndNoInternalSeam(t *testing.T) {
	data := []sectionData{
		{id: 0, foreground: RockGrid{hoverRect(.2, -.1, .1, .2)},
			guides: []Guide{{Pts: []V{{.5, -.1}, {.5, .1}}}}},
		{id: 1, foreground: RockGrid{hoverRect(.2, .9, .1, .2)},
			guides: []Guide{{Pts: []V{{.5, .9}, {.5, 1.1}}}}},
	}
	scene := queryScene(data...)
	options := QueryOptions{Targets: TargetRock}
	a := mustQuery(t, scene, Ray{Origin: V{.25, -.95}}, options)
	b := mustQuery(t, scene, Ray{Origin: V{.25, -1.05}}, options)
	if !a.Found || !b.Found || a.Hit.FormationID != b.Hit.FormationID {
		t.Fatal("formation identity changed at a section seam")
	}
	f, _ := scene.Formation(a.Hit.FormationID)
	if !f.Complete || len(f.Polygons) != 1 || math.Abs(faceArea(f.Polygons[0])-.02) > 1e-9 ||
		!reflect.DeepEqual(f.SectionIDs, []int64{0, -1}) {
		t.Fatalf("cross-section union: %+v", f)
	}
	for _, edge := range scene.world.queryIndex().formationEdges[f.ID.object] {
		if edge.A.Y == -1 && edge.B.Y == -1 {
			t.Fatal("internal cache seam became a collision surface")
		}
	}
	ga := mustQuery(t, scene, Ray{Origin: V{.5, -.95}}, QueryOptions{Targets: TargetGuide})
	gb := mustQuery(t, scene, Ray{Origin: V{.5, -1.05}}, QueryOptions{Targets: TargetGuide})
	if !ga.Found || !gb.Found || ga.Hit.GuideID != gb.Hit.GuideID {
		t.Fatal("padded guide copies received different IDs")
	}
	// Reloading one section preserves IDs while the other portion stays loaded.
	scene.world.sections[1].geometry = prepareTerrainGeometry(data[1], 0)
	scene.world.revision++
	if r := mustQuery(t, scene, Ray{Origin: V{.25, -1.05}}, options); r.Hit.FormationID != f.ID {
		t.Fatal("reloading neighbor changed formation identity")
	}
	delete(scene.world.sections, 1)
	scene.world.revision++
	partial, ok := scene.Formation(f.ID)
	if !ok || partial.Complete || len(partial.SectionIDs) != 1 {
		t.Fatalf("partial formation: %+v / %v", partial, ok)
	}
}

func TestQueryMissingTerrainAndOutOfWorld(t *testing.T) {
	scene := queryScene(
		sectionData{id: 0},
		sectionData{id: 2, foreground: RockGrid{hoverRect(.2, .2, .1, .2)}},
	)
	options := QueryOptions{Targets: TargetRock}
	for _, ray := range []Ray{
		{Origin: V{.25, -1.5}},
		{Origin: V{.25, -.5}, Direction: V{0, -1}, MaxDistance: 3},
		{Origin: V{.25, -.5}, Direction: V{0, -1}, MaxDistance: 1e9},
	} {
		if r := mustQuery(t, scene, ray, options); r.Complete || r.Found {
			t.Fatalf("missing terrain was searched through: %+v", r)
		}
	}
	for _, ray := range []Ray{
		{Origin: V{-.1, -.5}}, {Origin: V{.25, .1}},
		{Origin: V{.25, -.5}, Direction: V{0, 1}, MaxDistance: 1},
	} {
		if r := mustQuery(t, scene, ray, options); !r.Complete || r.Found {
			t.Fatalf("known-empty world range: %+v", r)
		}
	}
	scene.world.sections[0].geometry = prepareTerrainGeometry(sectionData{
		foreground: RockGrid{hoverRect(.2, .2, .1, .2)},
	}, 0)
	scene.world.revision++
	// A confirmed hit before the unknown part is still useful.
	r := mustQuery(t, scene, Ray{Origin: V{.25, -.5}, Direction: V{0, -1}, MaxDistance: 3}, options)
	if !r.Found || !r.Complete || math.Abs(r.Hit.Distance-.1) > 1e-9 {
		t.Fatalf("hit before unloaded terrain: %+v", r)
	}
	// A cast can enter the world from outside it.
	r = mustQuery(t, scene, Ray{Origin: V{-.1, -.7}, Direction: V{1, 0}, MaxDistance: .8}, options)
	if !r.Found || math.Abs(r.Hit.Distance-.3) > 1e-9 {
		t.Fatalf("cast entering world: %+v", r)
	}
}

func TestQueryIDMergesAndLifecycle(t *testing.T) {
	// Two loaded ends have no shared faces until their connecting section loads.
	bottom := sectionData{id: 0, foreground: RockGrid{hoverRect(.2, -.1, .1, .2)}}
	top := sectionData{id: 2, foreground: RockGrid{hoverRect(.2, .9, .1, .2)}}
	scene := queryScene(bottom, top)
	options := QueryOptions{Targets: TargetRock}
	a := mustQuery(t, scene, Ray{Origin: V{.25, -.95}}, options).Hit.FormationID
	b := mustQuery(t, scene, Ray{Origin: V{.25, -2.05}}, options).Hit.FormationID
	if a == b {
		t.Fatal("unconnected cached ends already share identity")
	}
	middle := sectionData{id: 1, foreground: RockGrid{
		hoverRect(.2, .9, .1, .2), hoverRect(.2, -.1, .1, .2), hoverRect(.2, .1, .1, .8),
	}}
	scene.world.sections[1] = &worldSection{geometry: prepareTerrainGeometry(middle, 0)}
	scene.world.revision++
	fa, oka := scene.Formation(a)
	fb, okb := scene.Formation(b)
	if !oka || !okb || fa.ID != fb.ID || !fa.Complete {
		t.Fatalf("IDs did not alias after merge: %v %v / %+v %+v", oka, okb, fa, fb)
	}
	foreign := queryScene(bottom)
	if _, ok := foreign.Formation(a); ok {
		t.Fatal("foreign world accepted formation ID")
	}
	if _, ok := scene.Formation(FormationID{}); ok {
		t.Fatal("zero formation ID accepted")
	}
	scene.world.sections = make(map[int64]*worldSection)
	scene.world.revision++
	if _, ok := scene.Formation(a); ok {
		t.Fatal("evicted formation still fetchable")
	}
	scene.world.sections[0] = &worldSection{geometry: prepareTerrainGeometry(bottom, 0)}
	scene.world.revision++
	newID := mustQuery(t, scene, Ray{Origin: V{.25, -.95}}, options).Hit.FormationID
	if newID == a {
		t.Fatal("expired formation ID was reused")
	}
	// Replacing the world mirrors Reset and rejects IDs even for the same seed.
	scene.world = queryScene(bottom).world
	if _, ok := scene.Formation(newID); ok {
		t.Fatal("reset world accepted stale ID")
	}
	scene.closed = true
	if r := mustQuery(t, scene, Ray{Origin: V{.25, -.95}}, options); r.Complete || r.Found {
		t.Fatal("closed scene returned a query result")
	}
}

func TestQueryValidationAndGuideCoverage(t *testing.T) {
	scene := queryScene(sectionData{})
	for _, ray := range []Ray{
		{Origin: V{math.NaN(), 0}}, {Direction: V{math.Inf(1), 0}},
		{MaxDistance: -1}, {MaxDistance: math.Inf(1)}, {MaxDistance: math.NaN()},
		{Origin: V{math.MaxFloat64, 0}, Direction: V{1, 0}, MaxDistance: math.MaxFloat64},
	} {
		if _, err := scene.Query(ray, QueryOptions{}); err == nil {
			t.Fatalf("invalid ray accepted: %+v", ray)
		}
	}
	for _, options := range []QueryOptions{
		{Targets: 8}, {GuideRadius: -1}, {GuideRadius: math.NaN()}, {GuideRadius: math.Inf(1)},
	} {
		if _, err := scene.Query(Ray{}, options); err == nil {
			t.Fatalf("invalid options accepted: %+v", options)
		}
	}
	for _, direction := range []V{{math.SmallestNonzeroFloat64, 0}, {math.MaxFloat64, math.MaxFloat64}} {
		d, err := validateQuery(Ray{Direction: direction, MaxDistance: 1}, QueryOptions{})
		if err != nil || math.Abs(d.Len()-1) > 1e-10 {
			t.Fatalf("finite direction normalization: %v %v", d, err)
		}
	}
	r := mustQuery(t, scene, Ray{Origin: V{.3, -.995}}, QueryOptions{Targets: TargetGuide, GuideRadius: .01})
	if r.Complete {
		t.Fatal("guide radius crossing into uncached neighbor reported complete")
	}
}

func TestQueryGeneratedTerrainMatchesExactCollision(t *testing.T) {
	data := []sectionData{buildSection(42, 0), buildSection(42, 1)}
	scene := queryScene(data...)
	for _, d := range data {
		exact := prepareTerrainGeometry(d, 0).collision
		for y := .025; y < 1; y += .073 {
			for x := .025; x < 1; x += .073 {
				p := V{x, y + sectionTop(d.id)}
				r := mustQuery(t, scene, Ray{Origin: p}, QueryOptions{Targets: TargetRock})
				if !r.Complete || r.Found != exact.Contains(p) {
					t.Fatalf("world query and exact collision disagree at %v: %+v", p, r)
				}
				if r.Found {
					f, ok := scene.Formation(r.Hit.FormationID)
					if !ok || !f.Contains(p) {
						t.Fatal("queried formation does not contain the selected world point")
					}
				}
			}
		}
	}
}

func TestQueryIDsExpireOnPruneWithoutAnInterveningQuery(t *testing.T) {
	d := sectionData{foreground: RockGrid{hoverRect(.2, .2, .1, .2)},
		guides: []Guide{{Pts: []V{{.5, .2}, {.5, .4}}}}}
	scene := queryScene(d)
	f := mustQuery(t, scene, Ray{Origin: V{.25, -.7}}, QueryOptions{Targets: TargetRock}).Hit.FormationID
	g := mustQuery(t, scene, Ray{Origin: V{.5, -.7}}, QueryOptions{Targets: TargetGuide}).Hit.GuideID
	if _, ok := queryScene(d).Guide(g); ok {
		t.Fatal("foreign world accepted guide ID")
	}
	scene.world.prune(-30, .8, 0)
	// Reload immediately, without giving the lazy query cache a chance to see
	// the empty world. Old references must nevertheless remain expired.
	scene.world.sections[0] = &worldSection{geometry: prepareTerrainGeometry(d, 0)}
	scene.world.revision++
	if _, ok := scene.Formation(f); ok {
		t.Fatal("pruned formation ID revived")
	}
	if _, ok := scene.Guide(g); ok {
		t.Fatal("pruned guide ID revived")
	}
	newGuide := mustQuery(t, scene, Ray{Origin: V{.5, -.7}}, QueryOptions{Targets: TargetGuide}).Hit.GuideID
	if newGuide == g {
		t.Fatal("expired guide ID was reused")
	}
	scene.closed = true
	if _, ok := scene.Guide(newGuide); ok {
		t.Fatal("closed scene fetched a guide")
	}
}
