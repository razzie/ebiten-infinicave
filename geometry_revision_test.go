package infinicave

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestGeometryRevisionTracksPublicationGrowthAndEviction(t *testing.T) {
	// Padded copies join across world Y=-4. Section 4 additionally extends
	// the same formation; section 3 remains cached when section 4 is evicted.
	bottom := sectionData{id: 3, foreground: RockGrid{terrainRect(.03, -.02, .03, .04)}}
	top := sectionData{id: 4, foreground: RockGrid{
		terrainRect(.03, .98, .03, .04), terrainRect(.03, .94, .03, .04),
	}}
	g := queryScene(bottom)
	w := g.world
	w.done = make(chan struct{})
	w.terrain = make(chan sectionTerrain, 1)
	t.Cleanup(g.Close)
	hit := rockAt(t, g, V{.04, -3.99})
	before, _ := g.Formation(hit.Hit.FormationID)
	revision := g.GeometryRevision()
	geometry := prepareTerrainGeometry(top, 0)
	w.terrain <- sectionTerrain{id: top.id, geometry: geometry}
	called := false
	g.onCollisionReady = func(CollisionGeometry) {
		called = true
		if g.GeometryRevision() <= revision {
			t.Fatal("collision callback observed a stale revision")
		}
		if _, ok := g.CollisionGeometry(-4); !ok {
			t.Fatal("collision callback ran before collision publication")
		}
	}
	index := w.queryIndex()
	queryRevision := index.revision
	w.receiveCollision(g)
	if w.queryIndex().revision != queryRevision {
		t.Fatal("early collision unnecessarily rebuilt the query index")
	}
	if !called || g.GeometryRevision() <= revision {
		t.Fatal("early collision publication did not advance revision")
	}
	still, _ := g.Formation(before.ID)
	if still.Min != before.Min || len(still.SectionIDs) != 1 {
		t.Fatal("early collision unexpectedly published query geometry")
	}
	revision = g.GeometryRevision()
	w.upload = &sectionUpload{stage: 4, data: sectionMesh{id: top.id, geometry: geometry}}
	w.receive(g)
	grown, ok := g.Formation(before.ID)
	if g.GeometryRevision() <= revision || !ok || grown.ID != before.ID ||
		grown.Min.Y >= before.Min.Y || len(grown.SectionIDs) != 2 {
		t.Fatalf("growth was not observable under the original ID: %+v", grown)
	}
	revision = g.GeometryRevision()
	w.upload = nil
	w.prune(-.8, .8, 0)
	shrunk, ok := g.Formation(before.ID)
	if g.GeometryRevision() <= revision || !ok || shrunk.ID != before.ID ||
		shrunk.Min != before.Min || len(shrunk.SectionIDs) != 1 {
		t.Fatalf("partial eviction was not observable: %+v", shrunk)
	}
	revision = g.GeometryRevision()
	w.prune(-100.8, .8, 0)
	if g.GeometryRevision() <= revision {
		t.Fatal("full eviction did not advance revision")
	}
	if _, ok := g.Formation(before.ID); ok {
		t.Fatal("fully evicted formation remained fetchable")
	}
}

func TestGeometryRevisionEarlyEvictionAndCarving(t *testing.T) {
	g := queryScene()
	w := g.world
	w.done = make(chan struct{})
	w.terrain = make(chan sectionTerrain, 1)
	t.Cleanup(g.Close)
	w.terrain <- sectionTerrain{id: 4, geometry: prepareTerrainGeometry(sectionData{
		id: 4, foreground: RockGrid{terrainRect(.2, .2, .6, .6)},
	}, 0)}
	w.receiveCollision(g)
	revision := g.GeometryRevision()
	result, err := g.CarveCircle(V{.5, -4.5}, .1)
	if err != nil || len(result.SectionIDs) != 1 || g.GeometryRevision() <= revision {
		t.Fatalf("early collision carve did not advance revision: %+v, %v", result, err)
	}
	geometry, _ := g.CollisionGeometry(-4)
	if geometry.Contains(V{.5, -4.5}) {
		t.Fatal("early collision carve did not publish updated geometry")
	}
	revision = g.GeometryRevision()
	if _, err := g.CarveCircle(V{.5, -4.5}, .1); err != nil || g.GeometryRevision() != revision {
		t.Fatal("repeated cut advanced revision")
	}
	w.prune(-.8, .8, 0)
	if g.GeometryRevision() <= revision {
		t.Fatal("early collision eviction did not advance revision")
	}
	if _, ok := g.CollisionGeometry(-4); ok {
		t.Fatal("early collision remained available after eviction")
	}
}

func TestGeometryRevisionLifecycleReadsAndResize(t *testing.T) {
	g := resolutionTestScene(t)
	g.world.prune(-.8, .8, 0) // Eviction is separate from the resize below.
	revision := g.GeometryRevision()
	hit := rockAt(t, g, V{.5, -.5})
	g.Formation(hit.Hit.FormationID)
	g.Guide(GuideID{})
	g.CollisionGeometry(0)
	g.Update(Viewport{})
	image := ebiten.NewImage(1, 1)
	defer image.Deallocate()
	g.Draw(image, Viewport{})
	if g.GeometryRevision() != revision {
		t.Fatal("reads or invalid viewport advanced revision")
	}
	g.SetRenderWidth(500)
	finishResolutionRefresh(t, g, Viewport{Y: -.8, Height: .8})
	if g.GeometryRevision() != revision {
		t.Fatal("resize and rerasterization advanced revision")
	}
	if _, err := g.CarveCircle(V{.5, -.5}, .1); err != nil || g.GeometryRevision() <= revision {
		t.Fatal("loaded terrain carve did not advance revision")
	}
	revision = g.GeometryRevision()
	if _, err := g.CarveCircle(V{.5, -.5}, .1); err != nil || g.GeometryRevision() != revision {
		t.Fatal("repeated loaded cut advanced revision")
	}
	if _, err := g.CarveCircle(V{.5, .5}, .1); err != nil || g.GeometryRevision() != revision {
		t.Fatal("out-of-world cut advanced revision")
	}
	g.Reset(7)
	if g.GeometryRevision() <= revision {
		t.Fatal("reset restarted or retained the old revision")
	}
	revision = g.GeometryRevision()
	g.Reset(8)
	if g.GeometryRevision() <= revision {
		t.Fatal("second reset did not advance revision")
	}
	revision = g.GeometryRevision()
	g.Close()
	if g.GeometryRevision() <= revision {
		t.Fatal("close did not advance revision")
	}
	revision = g.GeometryRevision()
	g.Close()
	g.Reset(42)
	if g.GeometryRevision() != revision {
		t.Fatal("operations after close advanced revision")
	}
}
