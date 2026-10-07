package infinicave

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
