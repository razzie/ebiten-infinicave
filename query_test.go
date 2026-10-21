package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestQueryBatsOptInAndHit(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		t.Run(orientation.String(), func(t *testing.T) {
			scene, err := NewScene(Config{Seed: 42, BatsPerMinute: 7.5, Orientation: orientation})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(scene.Close)
			scene.bats.Step(render.Viewport{Y: -2, Height: 1}, 6)
			bat := scene.bats.Bats[0]
			bat.Age, bat.Start, bat.End = 0, V{X: .5, Y: -1.5}, V{X: .8, Y: -1.5}
			scene.bats.Bats = []render.Bat{bat}
			point := terrain.WorldPoint(orientation, bat.Start)
			options := QueryOptions{Targets: TargetBat}
			r := mustQuery(t, scene, Ray{Origin: point}, options)
			if !r.Found || !r.Complete || r.Hit.Kind != TargetBat || r.Hit.BatID == (BatID{}) ||
				r.Hit.FormationID != (FormationID{}) || r.Hit.GuideID != (GuideID{}) ||
				!r.Hit.StartedInside || r.Hit.Point != point || r.Hit.Normal != (V{}) || r.Hit.Distance != 0 {
				t.Fatalf("bat-only point query over unloaded terrain: %+v", r)
			}
			if scene.world.queries != nil || len(scene.world.sections) != 0 || scene.bats.Bats[0] != bat {
				t.Fatal("bat query loaded/indexed terrain or changed the bat")
			}
			id := r.Hit.BatID
			direction := terrain.WorldPoint(orientation, V{X: 1})
			ray := Ray{Origin: point.Sub(direction.Mul(.1)), Direction: direction.Mul(8), MaxDistance: .2}
			r = mustQuery(t, scene, ray, options)
			if !r.Found || !r.Complete || r.Hit.BatID != id || r.Hit.StartedInside ||
				r.Hit.Distance < .07 || r.Hit.Distance > .1 || math.Abs(r.Hit.Normal.Len()-1) > 1e-9 ||
				r.Hit.Normal.Dot(direction) > 0 || r.Hit.Point.Sub(ray.Origin.Add(direction.Mul(r.Hit.Distance))).Len() > 1e-9 {
				t.Fatalf("world-space bat ray: %+v", r)
			}
			ray.MaxDistance = .05
			if r := mustQuery(t, scene, ray, options); r.Found || !r.Complete {
				t.Fatalf("short bat ray: %+v", r)
			}
			if r := mustQuery(t, scene, Ray{Origin: point}, QueryOptions{}); r.Found {
				t.Fatal("default targets included bats")
			}
			foreign := queryScene()
			foreign.bats = render.NewBatFlock(42, 7.5, orientation)
			foreign.bats.Bats = []render.Bat{bat}
			if foreign.HitBat(id) || scene.HitBat(BatID{}) {
				t.Fatal("zero or foreign bat ID accepted")
			}
			revision := scene.GeometryRevision()
			if !scene.HitBat(id) || scene.HitBat(id) {
				t.Fatal("hit did not start the fall exactly once")
			}
			if r := mustQuery(t, scene, Ray{Origin: point}, options); r.Found || !r.Complete {
				t.Fatalf("falling bat remained queryable: %+v", r)
			}
			if scene.GeometryRevision() != revision || len(scene.world.cuts) != 0 || len(scene.world.sections) != 0 {
				t.Fatal("hitting a bat changed terrain")
			}
			scene.Reset(42)
			scene.bats.Step(render.Viewport{Y: -2, Height: 1}, 6)
			if scene.HitBat(id) {
				t.Fatal("reset accepted a stale bat ID")
			}
			scene.Close()
			if scene.HitBat(id) {
				t.Fatal("closed scene accepted a bat ID")
			}
			if r := mustQuery(t, scene, Ray{Origin: point}, options); r.Found || r.Complete {
				t.Fatalf("closed scene bat query: %+v", r)
			}
		})
	}
	disabled := queryScene()
	if r := mustQuery(t, disabled, Ray{}, QueryOptions{Targets: TargetBat}); r.Found || !r.Complete {
		t.Fatalf("disabled bats: %+v", r)
	}
}

func TestQueryBatsNearestAndTerrainCoverage(t *testing.T) {
	for _, test := range []struct {
		name     string
		point    V
		data     []terrain.SectionData
		ray      Ray
		targets  TargetMask
		kind     TargetMask
		complete bool
	}{
		{"bat before rock", V{X: .3, Y: -.5}, []terrain.SectionData{{Foreground: RockGrid{terrainRect(.6, .4, .1, .2)}}},
			Ray{Origin: V{X: .1, Y: -.5}, Direction: V{X: 1}, MaxDistance: .8}, TargetRock | TargetBat, TargetBat, true},
		{"rock before bat", V{X: .6, Y: -.5}, []terrain.SectionData{{Foreground: RockGrid{terrainRect(.3, .4, .1, .2)}}},
			Ray{Origin: V{X: .1, Y: -.5}, Direction: V{X: 1}, MaxDistance: .8}, TargetRock | TargetBat, TargetRock, true},
		{"guide before bat", V{X: .6, Y: -.5}, []terrain.SectionData{{Guides: []Guide{{Pts: []V{{X: .3, Y: .4}, {X: .3, Y: .6}}}}}},
			Ray{Origin: V{X: .1, Y: -.5}, Direction: V{X: 1}, MaxDistance: .8}, TargetGuide | TargetBat, TargetGuide, true},
		{"bat wins rock tie", V{X: .5, Y: -.5}, []terrain.SectionData{{Foreground: RockGrid{terrainRect(.4, .4, .2, .2)}}},
			Ray{Origin: V{X: .5, Y: -.5}}, TargetRock | TargetBat, TargetBat, true},
		{"guide wins bat tie", V{X: .5, Y: -.5}, []terrain.SectionData{{Guides: []Guide{{Pts: []V{{X: .5, Y: .4}, {X: .5, Y: .6}}}}}},
			Ray{Origin: V{X: .5, Y: -.5}}, TargetGuide | TargetBat, TargetGuide, true},
		{"bat before unloaded band", V{X: .5, Y: -.7}, []terrain.SectionData{{}},
			Ray{Origin: V{X: .5, Y: -.5}, Direction: V{Y: -1}, MaxDistance: 2}, TargetRock | TargetBat, TargetBat, true},
		{"bat beyond unloaded band", V{X: .5, Y: -2.5}, []terrain.SectionData{{}},
			Ray{Origin: V{X: .5, Y: -.5}, Direction: V{Y: -1}, MaxDistance: 3}, TargetRock | TargetBat, 0, false},
		{"bat-only beyond unloaded band", V{X: .5, Y: -2.5}, nil,
			Ray{Origin: V{X: .5, Y: -.5}, Direction: V{Y: -1}, MaxDistance: 3}, TargetBat, TargetBat, true},
		{"bat in side margin", V{X: -.2, Y: -.5}, nil,
			Ray{Origin: V{X: -.4, Y: -.5}, Direction: V{X: 1}, MaxDistance: .3}, TargetRock | TargetBat, TargetBat, true},
		{"bat before entering unloaded world", V{X: -.2, Y: -.5}, nil,
			Ray{Origin: V{X: -.4, Y: -.5}, Direction: V{X: 1}, MaxDistance: 1}, TargetRock | TargetBat, TargetBat, true},
	} {
		for _, orientation := range []Orientation{Vertical, Horizontal} {
			t.Run(test.name+"/"+orientation.String(), func(t *testing.T) {
				scene := queryScene(test.data...)
				scene.orientation = orientation
				scene.bats = render.NewBatFlock(42, 7.5, orientation)
				scene.bats.Step(render.Viewport{Y: -2, Height: 1}, 6)
				bat := scene.bats.Bats[0]
				bat.Age, bat.Start, bat.End = 0, test.point, test.point.Add(V{X: .3})
				scene.bats.Bats = []render.Bat{bat}
				ray := test.ray
				ray.Origin, ray.Direction = terrain.WorldPoint(orientation, ray.Origin), terrain.WorldPoint(orientation, ray.Direction)
				r := mustQuery(t, scene, ray, QueryOptions{Targets: test.targets})
				if r.Complete != test.complete || r.Found != (test.kind != 0) || r.Hit.Kind != test.kind {
					t.Fatalf("nearest/coverage: %+v", r)
				}
			})
		}
	}
}

func TestQueryBatIDSurvivesTerrainChanges(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.2, .2, .1, .2)}})
	scene.bats = render.NewBatFlock(42, 7.5)
	scene.bats.Step(render.Viewport{Y: -2, Height: 1}, 6)
	bat := scene.bats.Bats[0]
	bat.Age, bat.Start, bat.End = 0, V{X: .7, Y: -.5}, V{X: .8, Y: -.5}
	scene.bats.Bats = []render.Bat{bat}
	ray, options := Ray{Origin: bat.Start}, QueryOptions{Targets: TargetBat}
	id := mustQuery(t, scene, ray, options).Hit.BatID
	// Build the terrain identities, then invalidate them with a terrain-only cut.
	mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.7}}, QueryOptions{})
	result, err := scene.CarveTerrain(Hole{Shape: HoleCircle, Center: V{X: .25, Y: -.7}, Radius: .02})
	if err != nil || len(result.SectionIDs) == 0 || scene.world.queries != nil {
		t.Fatalf("fixture did not invalidate terrain identities: %+v / %v", result, err)
	}
	if r := mustQuery(t, scene, ray, options); !r.Found || r.Hit.BatID != id {
		t.Fatalf("terrain cut expired a flying bat: %+v", r)
	}
	scene.world.prune(-10, 1, 0)
	if len(scene.world.sections) != 0 {
		t.Fatal("fixture did not evict terrain")
	}
	if r := mustQuery(t, scene, ray, options); !r.Found || r.Hit.BatID != id || !scene.HitBat(id) {
		t.Fatalf("terrain eviction expired a flying bat: %+v", r)
	}
}

func TestQueryFormationPointRayAndFetch(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{
		terrainRect(.2, .2, .1, .2), terrainRect(.3, .2, .1, .2),
		terrainRect(.6, .2, .1, .2),
	}})
	options := QueryOptions{Targets: TargetRock}
	point := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.7}}, options)
	if !point.Found || !point.Complete || !point.Hit.StartedInside || point.Hit.Distance != 0 || point.Hit.Normal != (V{}) {
		t.Fatalf("point query: %+v", point)
	}
	otherFace := mustQuery(t, scene, Ray{Origin: V{X: .35, Y: -.7}}, options)
	if otherFace.Hit.FormationID != point.Hit.FormationID {
		t.Fatal("connected cells have different formation IDs")
	}
	separate := mustQuery(t, scene, Ray{Origin: V{X: .65, Y: -.7}}, options)
	if !separate.Found || separate.Hit.FormationID == point.Hit.FormationID {
		t.Fatal("disconnected formations have the same ID")
	}
	hit := mustQuery(t, scene, Ray{Origin: V{X: .1, Y: -.7}, Direction: V{X: 10, Y: 0}, MaxDistance: .8}, options)
	if !hit.Found || !hit.Complete || hit.Hit.FormationID != point.Hit.FormationID ||
		math.Abs(hit.Hit.Distance-.1) > 1e-9 || hit.Hit.Point.Sub(V{X: .2, Y: -.7}).Len() > 1e-9 || hit.Hit.Normal != (V{X: -1, Y: 0}) {
		t.Fatalf("ray query: %+v", hit)
	}
	short := mustQuery(t, scene, Ray{Origin: V{X: .1, Y: -.7}, Direction: V{X: 1, Y: 0}, MaxDistance: .09}, options)
	if short.Found || !short.Complete {
		t.Fatalf("short cast should miss: %+v", short)
	}
	f, ok := scene.Formation(hit.Hit.FormationID)
	if !ok || !f.Complete || len(f.Polygons) != 1 || !f.Contains(V{X: .35, Y: -.7}) || f.Contains(V{X: .65, Y: -.7}) ||
		!reflect.DeepEqual(f.SectionIDs, []int64{0}) {
		t.Fatalf("formation fetch: %+v / %v", f, ok)
	}
	area := geom.PolygonArea(f.Polygons[0])
	if math.Abs(area-.04) > 1e-10 {
		t.Fatalf("union area %v, want .04", area)
	}
	f.Polygons[0][0].X = 100
	f.SectionIDs[0] = 100
	again, _ := scene.Formation(hit.Hit.FormationID)
	if !again.Contains(V{X: .25, Y: -.7}) || again.SectionIDs[0] != 0 {
		t.Fatal("fetch returned mutable cached slices")
	}
	// Direction zero overrides a positive MaxDistance, and vice versa.
	for _, ray := range []Ray{
		{Origin: V{X: .25, Y: -.7}, MaxDistance: 10},
		{Origin: V{X: .25, Y: -.7}, Direction: V{X: 1, Y: 0}},
	} {
		if result := mustQuery(t, scene, ray, options); !result.Hit.StartedInside || result.Hit.Distance != 0 {
			t.Fatalf("point-query convention: %+v", result)
		}
	}
}

func TestQueryHoleAndConcavity(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{
		terrainRect(.2, .2, .3, .05), terrainRect(.2, .45, .3, .05),
		terrainRect(.2, .25, .05, .2), terrainRect(.45, .25, .05, .2),
	}})
	options := QueryOptions{Targets: TargetRock}
	if r := mustQuery(t, scene, Ray{Origin: V{X: .3, Y: -.65}}, options); r.Found || !r.Complete {
		t.Fatalf("point in hole selected rock: %+v", r)
	}
	r := mustQuery(t, scene, Ray{Origin: V{X: .3, Y: -.65}, Direction: V{X: 1, Y: 0}, MaxDistance: .5}, options)
	if !r.Found || math.Abs(r.Hit.Distance-.15) > 1e-9 || r.Hit.Normal != (V{X: -1, Y: 0}) {
		t.Fatalf("ray from hole: %+v", r)
	}
	f, _ := scene.Formation(r.Hit.FormationID)
	if len(f.Polygons) != 2 || f.Contains(V{X: .3, Y: -.65}) {
		t.Fatal("formation fetch lost its hole")
	}
	// Hit testing stays exact even when collision contours are simplified.
	concave := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRock([]V{
		{X: .2, Y: .2}, {X: .4, Y: .2}, {X: .4, Y: .3}, {X: .3, Y: .3}, {X: .3, Y: .4}, {X: .2, Y: .4},
	})}})
	if r := mustQuery(t, concave, Ray{Origin: V{X: .35, Y: -.65}}, options); r.Found {
		t.Fatal("empty concavity selected rock")
	}
}

func TestQueryGuidesRadiusNearestAndFetch(t *testing.T) {
	g := terrain.SplineGuide([]V{{X: .3, Y: .2}, {X: .3, Y: .4}}, 1)
	scene := queryScene(terrain.SectionData{Guides: []Guide{g}, Foreground: RockGrid{terrainRect(.5, .2, .1, .2)}})
	for _, tc := range []struct {
		options  QueryOptions
		kind     TargetMask
		distance float64
	}{
		{QueryOptions{}, TargetGuide, .2},
		{QueryOptions{Targets: TargetRock}, TargetRock, .4},
		{QueryOptions{Targets: TargetGuide, GuideRadius: .02}, TargetGuide, .18},
	} {
		r := mustQuery(t, scene, Ray{Origin: V{X: .1, Y: -.7}, Direction: V{X: 1, Y: 0}, MaxDistance: .8}, tc.options)
		if !r.Found || !r.Complete || r.Hit.Kind != tc.kind || math.Abs(r.Hit.Distance-tc.distance) > 1e-9 ||
			r.Hit.Normal.Sub(V{X: -1, Y: 0}).Len() > 1e-9 {
			t.Fatalf("nearest target: %+v, want %v at %v", r, tc.kind, tc.distance)
		}
	}
	r := mustQuery(t, scene, Ray{Origin: V{X: .31, Y: -.7}}, QueryOptions{Targets: TargetGuide, GuideRadius: .02})
	if !r.Found || !r.Hit.StartedInside || r.Hit.Normal != (V{}) {
		t.Fatalf("guide proximity: %+v", r)
	}
	guide, ok := scene.Guide(r.Hit.GuideID)
	if !ok || math.Abs(guide.S[len(guide.S)-1]-.2) > 1e-9 || guide.Points[0] != (V{X: .3, Y: -.8}) {
		t.Fatalf("guide fetch: %+v / %v", guide, ok)
	}
	guide.Points[0].X = 100
	guide.S[0] = 100
	guide, _ = scene.Guide(r.Hit.GuideID)
	if guide.Points[0].X != .3 || guide.S[0] != 0 {
		t.Fatal("guide fetch exposed mutable slices")
	}
	// Both guides use the same seed; their geometry must still distinguish IDs.
	scene2 := queryScene(terrain.SectionData{Guides: []Guide{
		{Pts: []V{{X: .3, Y: .2}, {X: .3, Y: .4}}, Seed: 7},
		{Pts: []V{{X: .6, Y: .2}, {X: .6, Y: .4}}, Seed: 7},
	}})
	a := mustQuery(t, scene2, Ray{Origin: V{X: .3, Y: -.7}}, QueryOptions{Targets: TargetGuide})
	b := mustQuery(t, scene2, Ray{Origin: V{X: .6, Y: -.7}}, QueryOptions{Targets: TargetGuide})
	if !a.Found || !b.Found || a.Hit.GuideID == b.Hit.GuideID {
		t.Fatal("distinct custom guides with equal seeds were merged")
	}
}

func TestQueryNearestRockAndGuideTie(t *testing.T) {
	for _, guideX := range []float64{.2, .4} {
		scene := queryScene(terrain.SectionData{
			Foreground: RockGrid{terrainRect(.2, .2, .1, .2)},
			Guides:     []Guide{{Pts: []V{{X: guideX, Y: .2}, {X: guideX, Y: .4}}}},
		})
		want := TargetRock
		if guideX == .2 {
			want = TargetGuide
		}
		for i := 0; i < 10; i++ { // map iteration must not change tie outcomes
			r := mustQuery(t, scene, Ray{Origin: V{X: .1, Y: -.7}, Direction: V{X: 1, Y: 0}, MaxDistance: .8}, QueryOptions{})
			if !r.Found || r.Hit.Kind != want || math.Abs(r.Hit.Distance-.1) > 1e-9 {
				t.Fatalf("nearest target or guide tie preference: %+v", r)
			}
		}
	}
}

func TestQueryAcrossSectionsAndNoInternalSeam(t *testing.T) {
	data := []terrain.SectionData{
		{ID: 0, Foreground: RockGrid{terrainRect(.2, -.1, .1, .2)},
			Guides: []Guide{{Pts: []V{{X: .5, Y: -.1}, {X: .5, Y: .1}}}}},
		{ID: 1, Foreground: RockGrid{terrainRect(.2, .9, .1, .2)},
			Guides: []Guide{{Pts: []V{{X: .5, Y: .9}, {X: .5, Y: 1.1}}}}},
	}
	scene := queryScene(data...)
	options := QueryOptions{Targets: TargetRock}
	a := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.95}}, options)
	b := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -1.05}}, options)
	if !a.Found || !b.Found || a.Hit.FormationID != b.Hit.FormationID {
		t.Fatal("formation identity changed at a section seam")
	}
	f, _ := scene.Formation(a.Hit.FormationID)
	if !f.Complete || len(f.Polygons) != 1 || math.Abs(geom.PolygonArea(f.Polygons[0])-.02) > 1e-9 ||
		!reflect.DeepEqual(f.SectionIDs, []int64{0, -1}) {
		t.Fatalf("cross-section union: %+v", f)
	}
	for _, edge := range scene.world.queryIndex().formationEdges[f.ID.object] {
		if edge.A.Y == -1 && edge.B.Y == -1 {
			t.Fatal("internal cache seam became a collision surface")
		}
	}
	ga := mustQuery(t, scene, Ray{Origin: V{X: .5, Y: -.95}}, QueryOptions{Targets: TargetGuide})
	gb := mustQuery(t, scene, Ray{Origin: V{X: .5, Y: -1.05}}, QueryOptions{Targets: TargetGuide})
	if !ga.Found || !gb.Found || ga.Hit.GuideID != gb.Hit.GuideID {
		t.Fatal("padded guide copies received different IDs")
	}
	// Reloading one section preserves IDs while the other portion stays loaded.
	scene.world.sections[1].geometry = terrain.PrepareTerrainGeometry(data[1], 0)
	scene.world.revision++
	if r := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -1.05}}, options); r.Hit.FormationID != f.ID {
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
		terrain.SectionData{ID: 0},
		terrain.SectionData{ID: 2, Foreground: RockGrid{terrainRect(.2, .2, .1, .2)}},
	)
	options := QueryOptions{Targets: TargetRock}
	for _, ray := range []Ray{
		{Origin: V{X: .25, Y: -1.5}},
		{Origin: V{X: .25, Y: -.5}, Direction: V{X: 0, Y: -1}, MaxDistance: 3},
		{Origin: V{X: .25, Y: -.5}, Direction: V{X: 0, Y: -1}, MaxDistance: 1e9},
	} {
		if r := mustQuery(t, scene, ray, options); r.Complete || r.Found {
			t.Fatalf("missing terrain was searched through: %+v", r)
		}
	}
	for _, ray := range []Ray{
		{Origin: V{X: -.1, Y: -.5}}, {Origin: V{X: .25, Y: .1}},
		{Origin: V{X: .25, Y: -.5}, Direction: V{X: 0, Y: 1}, MaxDistance: 1},
	} {
		if r := mustQuery(t, scene, ray, options); !r.Complete || r.Found {
			t.Fatalf("known-empty world range: %+v", r)
		}
	}
	scene.world.sections[0].geometry = terrain.PrepareTerrainGeometry(terrain.SectionData{
		Foreground: RockGrid{terrainRect(.2, .2, .1, .2)},
	}, 0)
	scene.world.revision++
	// A confirmed hit before the unknown part is still useful.
	r := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.5}, Direction: V{X: 0, Y: -1}, MaxDistance: 3}, options)
	if !r.Found || !r.Complete || math.Abs(r.Hit.Distance-.1) > 1e-9 {
		t.Fatalf("hit before unloaded terrain: %+v", r)
	}
	// A cast can enter the world from outside it.
	r = mustQuery(t, scene, Ray{Origin: V{X: -.1, Y: -.7}, Direction: V{X: 1, Y: 0}, MaxDistance: .8}, options)
	if !r.Found || math.Abs(r.Hit.Distance-.3) > 1e-9 {
		t.Fatalf("cast entering world: %+v", r)
	}
}

func TestQueryIDMergesAndLifecycle(t *testing.T) {
	// Two loaded ends have no shared faces until their connecting section loads.
	bottom := terrain.SectionData{ID: 0, Foreground: RockGrid{terrainRect(.2, -.1, .1, .2)}}
	top := terrain.SectionData{ID: 2, Foreground: RockGrid{terrainRect(.2, .9, .1, .2)}}
	scene := queryScene(bottom, top)
	options := QueryOptions{Targets: TargetRock}
	a := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.95}}, options).Hit.FormationID
	b := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -2.05}}, options).Hit.FormationID
	if a == b {
		t.Fatal("unconnected cached ends already share identity")
	}
	middle := terrain.SectionData{ID: 1, Foreground: RockGrid{
		terrainRect(.2, .9, .1, .2), terrainRect(.2, -.1, .1, .2), terrainRect(.2, .1, .1, .8),
	}}
	scene.world.sections[1] = &worldSection{geometry: terrain.PrepareTerrainGeometry(middle, 0)}
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
	scene.world.sections[0] = &worldSection{geometry: terrain.PrepareTerrainGeometry(bottom, 0)}
	scene.world.revision++
	newID := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.95}}, options).Hit.FormationID
	if newID == a {
		t.Fatal("expired formation ID was reused")
	}
	// Replacing the world mirrors Reset and rejects IDs even for the same seed.
	scene.world = queryScene(bottom).world
	if _, ok := scene.Formation(newID); ok {
		t.Fatal("reset world accepted stale ID")
	}
	scene.closed = true
	if r := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.95}}, options); r.Complete || r.Found {
		t.Fatal("closed scene returned a query result")
	}
}

func TestQueryValidationAndGuideCoverage(t *testing.T) {
	scene := queryScene(terrain.SectionData{})
	for _, ray := range []Ray{
		{Origin: V{X: math.NaN(), Y: 0}}, {Direction: V{X: math.Inf(1), Y: 0}},
		{MaxDistance: -1}, {MaxDistance: math.Inf(1)}, {MaxDistance: math.NaN()},
		{Origin: V{X: math.MaxFloat64, Y: 0}, Direction: V{X: 1, Y: 0}, MaxDistance: math.MaxFloat64},
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
	for _, direction := range []V{{X: math.SmallestNonzeroFloat64, Y: 0}, {X: math.MaxFloat64, Y: math.MaxFloat64}} {
		d, err := validateQuery(Ray{Direction: direction, MaxDistance: 1}, QueryOptions{})
		if err != nil || math.Abs(d.Len()-1) > 1e-10 {
			t.Fatalf("finite direction normalization: %v %v", d, err)
		}
	}
	r := mustQuery(t, scene, Ray{Origin: V{X: .3, Y: -.995}}, QueryOptions{Targets: TargetGuide, GuideRadius: .01})
	if r.Complete {
		t.Fatal("guide radius crossing into uncached neighbor reported complete")
	}
}

func TestQueryGeneratedTerrainMatchesExactCollision(t *testing.T) {
	data := []terrain.SectionData{terrain.BuildSection(42, 0), terrain.BuildSection(42, 1)}
	scene := queryScene(data...)
	for _, d := range data {
		exact := terrain.PrepareTerrainGeometry(d, 0).Collision
		for y := .025; y < 1; y += .073 {
			for x := .025; x < 1; x += .073 {
				p := V{X: x, Y: y + terrain.SectionTop(d.ID)}
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
	d := terrain.SectionData{Foreground: RockGrid{terrainRect(.2, .2, .1, .2)},
		Guides: []Guide{{Pts: []V{{X: .5, Y: .2}, {X: .5, Y: .4}}}}}
	scene := queryScene(d)
	f := mustQuery(t, scene, Ray{Origin: V{X: .25, Y: -.7}}, QueryOptions{Targets: TargetRock}).Hit.FormationID
	g := mustQuery(t, scene, Ray{Origin: V{X: .5, Y: -.7}}, QueryOptions{Targets: TargetGuide}).Hit.GuideID
	if _, ok := queryScene(d).Guide(g); ok {
		t.Fatal("foreign world accepted guide ID")
	}
	scene.world.prune(-30, .8, 0)
	// Reload immediately, without giving the lazy query cache a chance to see
	// the empty world. Old references must nevertheless remain expired.
	scene.world.sections[0] = &worldSection{geometry: terrain.PrepareTerrainGeometry(d, 0)}
	scene.world.revision++
	if _, ok := scene.Formation(f); ok {
		t.Fatal("pruned formation ID revived")
	}
	if _, ok := scene.Guide(g); ok {
		t.Fatal("pruned guide ID revived")
	}
	newGuide := mustQuery(t, scene, Ray{Origin: V{X: .5, Y: -.7}}, QueryOptions{Targets: TargetGuide}).Hit.GuideID
	if newGuide == g {
		t.Fatal("expired guide ID was reused")
	}
	scene.closed = true
	if _, ok := scene.Guide(newGuide); ok {
		t.Fatal("closed scene fetched a guide")
	}
}

func TestTerrainConnectedBlocks(t *testing.T) {
	grid := RockGrid{
		terrainRect(0.03, 0.1, 0.02, 0.04),
		// Two shorter edges share the first cell's long edge.
		terrainRect(0.05, 0.1, 0.03, 0.02),
		terrainRect(0.05, 0.12, 0.03, 0.02),
		// Corner contact and empty space must not join formations.
		terrainRect(0.08, 0.14, 0.02, 0.02),
		terrainRect(0.13, 0.1, 0.03, 0.04),
		// Deep concavity: its empty notch remains unselectable.
		terrainRock([]V{{X: 0.2, Y: 0.1}, {X: 0.24, Y: 0.1}, {X: 0.24, Y: 0.11}, {X: 0.21, Y: 0.11}, {X: 0.21, Y: 0.14}, {X: 0.2, Y: 0.14}}),
	}
	hidden := terrainRect(0.3, 0.1, 0.04, 0.04)
	hidden.Color.A = 0
	grid = append(grid, hidden)
	background := terrainRect(0.35, 0.1, 0.04, 0.04)
	background.Raised = false
	grid = append(grid, background)
	h := terrain.PrepareTerrainGeometry(terrain.SectionData{Foreground: grid}, 0)
	if len(h.Blocks) != 4 {
		t.Fatalf("got %d blocks, want 4", len(h.Blocks))
	}
	scene := &Scene{world: &world{sections: map[int64]*worldSection{0: {geometry: h}}}}
	query := func(p V) QueryResult { return rockAt(t, scene, p.Add(V{Y: -1})) }
	first := query(V{X: 0.04, Y: 0.11}).Hit.FormationID
	for _, p := range []V{{X: 0.06, Y: 0.11}, {X: 0.06, Y: 0.13}, {X: 0.05, Y: 0.12}} {
		if got := query(p); !got.Found || got.Hit.FormationID != first {
			t.Fatalf("connected face at %v did not select the entire block", p)
		}
	}
	for _, p := range []V{{X: 0.09, Y: 0.15}, {X: 0.14, Y: 0.11}, {X: 0.205, Y: 0.13}} {
		if got := query(p); !got.Found || got.Hit.FormationID == first {
			t.Fatalf("separate block at %v selected incorrectly", p)
		}
	}
	for _, p := range []V{{X: 0.12, Y: 0.12}, {X: 0.23, Y: 0.13}, {X: 0.32, Y: 0.12}, {X: 0.37, Y: 0.12}} {
		if got := query(p); got.Found {
			t.Fatalf("empty, transparent, or background point %v selected a block", p)
		}
	}
}

func TestCarveReusesUnchangedFormationBoundaries(t *testing.T) {
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{
		terrainRect(.1, .2, .4, .6), terrainRect(.7, -.1, .2, .3),
	}})
	untouched := rockAt(t, scene, V{X: .8, Y: -.95}).Hit.FormationID
	q := scene.world.queryIndex()
	before := q.formations[untouched.object]
	edges := q.formationEdges[untouched.object]
	if before.Complete {
		t.Fatal("fixture should have unloaded continuation")
	}
	if _, err := scene.CarveCircle(V{X: .3, Y: -.5}, .05); err != nil {
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
	scene.world.sections[1] = &worldSection{geometry: terrain.PrepareTerrainGeometry(terrain.SectionData{ID: 1}, 0)}
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
