package infinicave

import (
	"reflect"
	"slices"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCollisionReadyDuringUploadIncludesStoredCutsAndOwnsPolygons(t *testing.T) {
	busy := render.SectionMesh{Background: render.GridMesh{Outlines: make([]render.TriangleMesh, uploadDrawsPerTick*2)}}
	w := &world{
		sections: make(map[int64]*worldSection), terrain: make(chan sectionTerrain, 1),
		jobs: make(chan sectionJob, 1), done: make(chan struct{}), working: true,
		// Empty draw batches keep an unrelated upload busy.
		upload: &sectionUpload{stage: uploadBackgroundOutlines, initialized: true, needed: render.BackgroundLayer,
			img: render.NewBackgroundImageAt(1000), data: busy, raster: busy},
	}
	g, err := NewScene(Config{})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	g.world = w
	defer g.Close()
	cut, _ := terrain.CutFromHole((Hole{Shape: HoleCircle, Center: V{X: .5, Y: -1.5}, Radius: .1}))
	w.appendCut(Hole{Shape: HoleCircle, Center: V{X: .5, Y: -1.5}, Radius: .1}, cut)
	early := terrain.PrepareTerrainGeometry(terrain.SectionData{ID: 1, Foreground: RockGrid{terrainRect(.2, .2, .6, .6)}}, 0)
	w.terrain <- sectionTerrain{id: 1, geometry: early}
	called := 0
	g.onCollisionReady = func(geometry CollisionGeometry) {
		called++
		if w.upload == nil || w.sections[1] != nil || !w.working {
			t.Fatal("collision waited for generation or upload to finish")
		}
		cached, ok := g.CollisionGeometry(-1)
		if !ok || geometry.ID != -1 || geometry.Top != -2 || !reflect.DeepEqual(geometry, cached) {
			t.Fatal("callback and collision accessor disagree")
		}
		if geometry.Contains(V{X: .5, Y: -1.5}) || !geometry.Contains(V{X: .3, Y: -1.3}) {
			t.Fatal("early collision did not apply stored runtime cuts")
		}
		geometry.Polygons[0][0].X = 100
		cached.Polygons[0][0].X = 200
	}
	viewport := Viewport{Y: -.8, Height: .8}
	if g.Update(viewport) || called != 1 {
		t.Fatal("Update did not notify before rendering was ready")
	}
	cached, ok := g.CollisionGeometry(-1)
	if !ok || cached.Polygons[0][0].X > 1 || !early.Collision.Contains(V{X: .5, Y: -1.5}) {
		t.Fatal("game-thread edits or callback modified worker-owned geometry")
	}
	// A runtime edit must update early geometry and report its section for physics.
	w.upload.img.Deallocate()
	w.upload = nil
	result, err := g.CarveCircle(V{X: .3, Y: -1.3}, .03)
	if err != nil || !reflect.DeepEqual(result.SectionIDs, []int64{-1}) {
		t.Fatalf("early carve did not report edited collisions: %+v, %v", result, err)
	}
	cached, _ = g.CollisionGeometry(-1)
	if cached.Contains(V{X: .3, Y: -1.3}) || called != 1 {
		t.Fatal("runtime carve left early collision stale or repeated notification")
	}
	w.prune(-100.8, .8, 0)
	if _, ok := g.CollisionGeometry(-1); ok {
		t.Fatal("distant early collision was not evicted")
	}
}

func TestCollisionReadyCallbackSurvivesResetAndCanCloseScene(t *testing.T) {
	called := 0
	var g *Scene
	var err error
	g, err = NewScene(Config{OnCollisionReady: func(CollisionGeometry) {
		called++
		g.Close()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.Reset(7)
	geometry := terrain.PrepareTerrainGeometry(terrain.SectionData{}, 0)
	request := g.world.nextSectionRequest()
	g.world.rememberRequest(0, request)
	g.world.terrain <- sectionTerrain{geometry: geometry, request: request, ctx: g.world.ctx}
	if g.Update(Viewport{Y: -.8, Height: .8}) || called != 1 || !g.closed {
		t.Fatal("reset lost callback or Update continued after callback closed scene")
	}
}

func TestSceneCollisionGeometryOwnershipAndAvailability(t *testing.T) {
	h := terrain.PrepareTerrainGeometry(terrain.SectionData{Foreground: RockGrid{terrainRect(0.03, 0.1, 0.03, 0.03)}}, 0)
	scene := &Scene{world: &world{sections: map[int64]*worldSection{0: {geometry: h}}}}
	geometry, ok := scene.CollisionGeometry(0)
	if !ok || len(geometry.Polygons) == 0 {
		t.Fatal("cached collision geometry is unavailable")
	}
	if geometry.Top != -1 || !geometry.Contains(V{X: .04, Y: -.89}) || geometry.Contains(V{X: .4, Y: -.89}) {
		t.Fatal("cached collision geometry does not use scene units")
	}
	before := h.Collision.Polygons[0][0]
	geometry.Polygons[0][0].X += 100
	if h.Collision.Polygons[0][0] != before {
		t.Fatal("caller modified the cached collision geometry")
	}
	copy, ok := scene.CollisionGeometry(0)
	if !ok || !copy.Contains(V{X: .04, Y: -.89}) {
		t.Fatal("repeated collision access changed the cached geometry")
	}
	upper := terrain.PrepareTerrainGeometry(terrain.SectionData{ID: 1, Foreground: RockGrid{terrainRect(0.03, 0.1, 0.03, 0.03)}}, 0)
	scene.world.sections[1] = &worldSection{geometry: upper}
	if geometry, ok := scene.CollisionGeometry(-1); !ok || geometry.ID != -1 || geometry.Top != -2 || !geometry.Contains(V{X: .04, Y: -1.89}) {
		t.Fatal("negative section ID did not resolve the correct cached geometry")
	}
	if _, ok := scene.CollisionGeometry(1); ok {
		t.Fatal("positive section ID accepted")
	}
	if _, ok := scene.CollisionGeometry(-2); ok {
		t.Fatal("missing section reported ready")
	}
	scene.closed = true
	if _, ok := scene.CollisionGeometry(0); ok {
		t.Fatal("closed scene exposed stale geometry")
	}
}

func TestForceGuidesUpShapesRockBelowAuthoredGuide(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, reverse := range []bool{false, true} {
			points := []V{{X: .2, Y: .4}, {X: .8, Y: .4}}
			if reverse {
				slices.Reverse(points)
			}
			original := slices.Clone(points)
			for _, forced := range []bool{false, true} {
				config := Config{Seed: 42, Orientation: orientation, ForceGuidesUp: forced, LoadSection: func(id int64) SectionContent {
					if id == 0 {
						return SectionContent{Guides: []Guide{{Pts: points}}}
					}
					return SectionContent{}
				}}
				section, err := GenerateSectionWithConfig(config, 0)
				if err != nil {
					t.Fatal(err)
				}
				below := section.Collision.Contains(V{X: .5, Y: .43}.Add(section.Origin))
				above := section.Collision.Contains(V{X: .5, Y: .37}.Add(section.Origin))
				wantBelow := forced || !reverse
				if below != wantBelow || above == wantBelow {
					t.Fatalf("%v reverse=%v forced=%v: rock below=%v above=%v, want below=%v", orientation, reverse, forced, below, above, wantBelow)
				}
				if len(section.Guides) != 1 || !reflect.DeepEqual(points, original) {
					t.Fatal("forcing up mutated loader content")
				}
				for i, p := range section.Guides[0].Pts {
					if p.Sub(original[i]).Len() > 1e-12 {
						t.Fatal("forcing up changed guide shape or point order")
					}
				}
			}
		}
	}
}

func TestSynchronousCollisionReadyOwnershipAndEmptySections(t *testing.T) {
	for _, empty := range []bool{false, true} {
		called := 0
		var notified CollisionGeometry
		config := Config{Seed: 42, LoadSection: testCurlSection, CollisionTolerance: .002}
		if empty {
			config.LoadSection = func(int64) SectionContent { return SectionContent{} }
		}
		config.OnCollisionReady = func(geometry CollisionGeometry) {
			called++
			notified = terrain.CopyCollisionGeometry(geometry)
			if len(geometry.Polygons) > 0 {
				geometry.Polygons[0][0].X = 100
			}
		}
		section, err := GenerateSectionWithConfig(config, -1)
		if err != nil || called != 1 || !reflect.DeepEqual(notified, section.Collision) {
			t.Fatalf("synchronous notification changed collision: empty=%v, calls=%d, err=%v", empty, called, err)
		}
		if empty != (len(notified.Polygons) == 0) {
			t.Fatal("empty sections did not notify with empty collision")
		}
	}
}

func TestHorizontalGeneratedSectionAndAuthoredContent(t *testing.T) {
	points := []V{{X: .2, Y: .45}, {X: .75, Y: .45}}
	load := func(id int64) SectionContent {
		if id != -1 {
			return SectionContent{}
		}
		return SectionContent{Guides: []Guide{{Pts: points}}, Holes: []Hole{{Shape: HoleCircle, Center: V{X: .5, Y: .45}, Radius: .04}}}
	}
	var early CollisionGeometry
	config := Config{Seed: 42, Orientation: Horizontal, LoadSection: load, OnCollisionReady: func(g CollisionGeometry) { early = g }}
	section, err := GenerateSectionWithConfig(config, -1)
	if err != nil {
		t.Fatal(err)
	}
	if section.Orientation != Horizontal || section.Origin != (V{X: 1, Y: 0}) || section.WindowOrigin != (V{}) ||
		section.Min != (V{X: 1, Y: 0}) || section.Max != (V{X: 2, Y: 1}) || !reflect.DeepEqual(early, section.Collision) {
		t.Fatalf("horizontal section metadata: %+v / %+v", section.Origin, early)
	}
	if len(section.Guides) != 1 || len(section.Holes) != 1 {
		t.Fatal("authored content missing")
	}
	for i, p := range section.Guides[0].Pts {
		if p.Sub(points[i]).Len() > 1e-12 {
			t.Fatalf("authored local coordinate changed: %v / %v", p, points[i])
		}
	}
	if section.Collision.Contains(V{X: 1.5, Y: .45}) {
		t.Fatal("authored horizontal hole missing")
	}
	for _, c := range section.Foreground {
		for _, p := range c.Polygon {
			if p.Y < -1e-9 || p.Y > 1+1e-9 || p.X < -1-1e-9 || p.X > 2+1e-9 {
				t.Fatalf("horizontal foreground padding is incorrect: %v", p)
			}
		}
	}
	// The public local faces agree with world collision after adding Origin.
	for x := .03; x < .97; x += .07 {
		for y := .03; y < .97; y += .07 {
			p := V{X: x, Y: y}
			inside := false
			for _, c := range section.Foreground {
				inside = inside || (CollisionGeometry{Polygons: [][]V{c.Polygon}}).Contains(p)
			}
			if inside != section.Collision.Contains(p.Add(section.Origin)) {
				t.Fatalf("horizontal local faces and collision disagree at %v", p)
			}
		}
	}
	again, err := GenerateSectionWithConfig(config, -1)
	if err != nil || !reflect.DeepEqual(section, again) || !reflect.DeepEqual(points, []V{{X: .2, Y: .45}, {X: .75, Y: .45}}) {
		t.Fatal("horizontal generation is not reproducible or mutated loader content")
	}
}

func TestSectionLoaderHolesCarveGeneratedGeometry(t *testing.T) {
	guides := []Guide{{Pts: []V{{X: .15, Y: .4}, {X: .5, Y: .35}, {X: .85, Y: .45}}, Seed: 1234}}
	content := SectionContent{Guides: guides}
	load := func(id int64) SectionContent {
		if id == 0 {
			return content
		}
		return SectionContent{}
	}
	base := terrain.NewSectionBuilder(42, load).Build(0)
	var center V
	area := 0.0
	for _, cell := range base.ForegroundTopology.Grid {
		if cell.Center.Y > .1 && cell.Center.Y < .9 && geom.PolygonArea(cell.Polygon) > area {
			center, area = cell.Center, geom.PolygonArea(cell.Polygon)
		}
	}
	if area == 0 || !terrain.PrepareTerrainGeometry(base, 0).Collision.Contains(center.Add(V{Y: -1})) {
		t.Fatal("test needs a solid blast center")
	}
	content.Holes = []Hole{{Shape: HoleCircle, Center: center, Radius: .02}}
	carved := terrain.NewSectionBuilder(42, load).Build(0)
	geometry := terrain.PrepareTerrainGeometry(carved, 0)
	if geometry.Collision.Contains(center.Add(V{Y: -1})) || len(geometry.Cuts) != 1 || len(carved.Holes) != 1 {
		t.Fatal("authored hole did not reach collision and render geometry")
	}
	for _, cell := range carved.Foreground {
		if geom.InsidePolygon(center, cell.Polygon) {
			t.Fatal("exported foreground grid filled authored hole")
		}
	}
	if again := terrain.NewSectionBuilder(42, load).Build(0); !reflect.DeepEqual(carved, again) {
		t.Fatal("authored holes changed across regeneration")
	}
	mesh := render.PrepareSection(carved, ViewClay)
	checkMesh(t, mesh.Foreground.Faces)
	section, err := GenerateSectionWithConfig(Config{Seed: 42, LoadSection: load}, 0)
	if err != nil || len(section.Holes) != 1 || section.Collision.Contains(center.Add(V{Y: -1})) {
		t.Fatalf("public section lost authored hole: %v", err)
	}
}
