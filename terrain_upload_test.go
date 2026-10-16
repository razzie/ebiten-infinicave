package infinicave

import (
	"bytes"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestBackgroundPublishesBeforeForeground(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	mesh := resolutionTestMesh(0)
	mesh.Layers = render.ForegroundLayer | render.BackgroundLayer
	mesh.Foreground.Outlines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), mesh.Foreground.Outlines...)
	w.savePending(mesh)
	for tick := 0; tick < 20 && (w.sections[0] == nil || w.sections[0].terrain == nil); tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s == nil || s.terrain == nil || s.foreground != nil || w.upload == nil {
		t.Fatal("background waited for the foreground cursor")
	}
	if w.upload.stage != uploadForegroundOutlines || w.upload.next == 0 {
		t.Fatal("background publication reset foreground progress")
	}
}

func TestForegroundPublishesBeforeBackground(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	mesh := resolutionTestMesh(0)
	mesh.Layers = render.ForegroundLayer | render.BackgroundLayer
	mesh.Background.Outlines = make([]render.TriangleMesh, uploadDrawsPerTick*64)
	w.savePending(mesh)
	receiveTestBudget(g)
	s := w.sections[0]
	if s == nil || s.foreground == nil || s.terrain != nil || w.upload == nil {
		t.Fatal("foreground waited for the background cursor")
	}
}

func TestLateForegroundJoinsActiveBackground(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	background := resolutionTestMesh(0)
	background.Layers = render.BackgroundLayer
	background.Background.Outlines = make([]render.TriangleMesh, uploadDrawsPerTick*64)
	w.savePending(background)
	receiveTestBudget(g)
	u := w.upload
	if u == nil || u.backgroundCursor.next == 0 {
		t.Fatal("fixture did not start background")
	}
	progress, private := u.backgroundCursor.next, u.img
	foreground := background
	foreground.Layers = render.ForegroundLayer
	w.savePending(foreground)
	for tick := 0; tick < 20 && (w.sections[0] == nil || w.sections[0].foreground == nil); tick++ {
		receiveTestBudget(g)
	}
	if w.sections[0] == nil || w.sections[0].foreground == nil || w.sections[0].terrain != nil || w.upload != u {
		t.Fatal("late foreground waited for active background")
	}
	if u.img != private || u.backgroundCursor.next < progress {
		t.Fatal("late foreground restarted background")
	}
}

func TestTerrainProductsKeepSeparateAAAccumulators(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	w.antialiasing = AntialiasingFull
	mesh := resolutionTestMesh(0)
	mesh.Layers = render.ForegroundLayer | render.BackgroundLayer
	// Degenerate triangles exercise batching without changing the test's coverage.
	mesh.Foreground.Faces.Indices = make([]uint32, (uploadIndicesPerTick/3+1)*3)
	mesh.Background.Faces.Indices = make([]uint32, (uploadIndicesPerTick/3+1)*3)
	w.startUpload(mesh)
	first := uploadBudget{start: time.Now().Add(time.Hour), quantumEnd: 1}
	w.advanceUpload(g, w.upload, &first)
	u := w.upload
	if u == nil || u.surface == nil {
		t.Fatal("foreground did not retain AA accumulator")
	}
	second := uploadBudget{start: time.Now().Add(time.Hour), quantumEnd: 1}
	w.advanceUpload(g, u, &second)
	if u.surface == nil || u.backgroundCursor.surface == nil || u.surface == u.backgroundCursor.surface {
		t.Fatal("terrain products shared or discarded an active AA accumulator")
	}
}

func TestGrassPublishesBeforeForeground(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	data := carvePixelFixture()
	mesh := render.PrepareForeground(data, ViewShaded, render.RockGrowthOptions{Grass: 1})
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	mesh.Foreground.Outlines = append(make([]render.TriangleMesh, uploadDrawsPerTick*256), mesh.Foreground.Outlines...)
	w.savePending(mesh)
	for tick := 0; tick < 100 && (w.sections[0] == nil || w.sections[0].grass == nil); tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s == nil || s.grass == nil || s.foreground != nil || w.upload == nil {
		t.Fatal("grass waited for foreground publication")
	}
	if s.grassWidth() != 64 || s.grassSource == nil || s.grassPending {
		t.Fatal("grass publication did not retain its own recipe and width")
	}
}

func TestLateGrassJoinsPrivateRockBase(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	data := carvePixelFixture()
	geometry := terrain.PrepareTerrainGeometry(data, 0)
	options := render.RockGrowthOptions{Grass: 1}
	base := render.PrepareRockBase(data, ViewShaded, options)
	base.Geometry = geometry
	base.Foreground.Outlines = make([]render.TriangleMesh, uploadDrawsPerTick*256)
	w.savePending(base)
	receiveTestBudget(g)
	u := w.upload
	progress, private := u.next, u.foreground
	full := render.PrepareForeground(data, ViewShaded, options)
	full.Geometry = geometry
	w.savePending(full)
	for tick := 0; tick < 100 && (w.sections[0] == nil || w.sections[0].grass == nil); tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s == nil || s.grass == nil || s.foreground != nil || w.upload != u {
		t.Fatal("grass recipe waited for the private base to finish")
	}
	if u.foreground != private || u.next < progress || !u.data.ForegroundBaseOnly {
		t.Fatal("late grass replaced the active base recipe or cursor")
	}
}

// Called from the GPU test's game loop. Grass must retain its own recipe and
// native scale while the displayed rock is still at the previous width.
func checkIndependentGrassPixels(t *testing.T) {
	t.Helper()
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		g, err := NewScene(Config{DisableBackground: true, Antialiasing: AntialiasingOff, Orientation: orientation})
		if err != nil {
			t.Fatal(err)
		}
		defer g.Close()
		g.world.close()
		w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 64, disableBackground: true, antialiasing: AntialiasingOff}
		g.world = w
		data := carvePixelFixture()
		mesh := render.PrepareForeground(data, ViewShaded, render.RockGrowthOptions{Grass: 1})
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		// A broad ground patch makes native-width mask/replay errors visible
		// even when a cut removes the fixture's small blade cluster entirely.
		mesh.Foreground.Grass = mesh.Foreground.Faces
		finish := func() {
			for tick := 0; tick < 1000 && w.upload != nil; tick++ {
				receiveTestBudget(g)
			}
			if w.upload != nil {
				t.Fatal("grass fixture stalled")
			}
		}
		w.startUpload(mesh)
		finish()
		s := w.sections[0]
		oldRock := s.foreground
		g.SetRenderWidth(96)
		replacement := mesh
		replacement.Foreground.Outlines = make([]render.TriangleMesh, uploadDrawsPerTick*256)
		w.startUpload(replacement)
		for tick := 0; tick < 100 && s.grassWidth() != 96; tick++ {
			receiveTestBudget(g)
		}
		if s.grass == nil || s.grassWidth() != 96 || s.foreground != oldRock || s.renderWidth() != 64 {
			t.Fatal("grass did not resize independently from rocks")
		}
		image := s.grass
		read := func() []byte {
			if s.grass == nil {
				return nil
			}
			pixels := make([]byte, 4*s.grass.Bounds().Dx()*s.grass.Bounds().Dy())
			s.grass.ReadPixels(pixels)
			return pixels
		}
		before := read()
		drawn := ebiten.NewImage(96, 96)
		w.drawGrass(drawn, -1, 1, 0, render.RenderTransform{Pixels: 96})
		drawnPixels := make([]byte, 4*96*96)
		drawn.ReadPixels(drawnPixels)
		drawn.Deallocate()
		if !bytes.Equal(before, drawnPixels) {
			t.Fatal("grass draw used the old rock width")
		}
		var root V
		found := false
		for _, a := range s.grassSource.Foreground.Growth {
			if a.Kind == render.GrassAttachment && len(a.Anchors) > 0 && !a.Dead {
				root, found = a.Anchors[0], true
				break
			}
		}
		if !found {
			t.Fatal("fixture has no grounded grass")
		}
		root.Y += terrain.SectionTop(0)
		if _, err := g.CarveCircle(terrain.WorldPoint(orientation, root), .006); err != nil {
			t.Fatal(err)
		}
		g.flushCarveImages()
		if bytes.Equal(before, read()) {
			t.Fatal("carving used the older rock width or recipe for new grass")
		}
		finish()
		if s.grass != image || s.renderWidth() != 96 {
			t.Fatalf("rock publication changed grass: same image=%v, grass present=%v, rock width=%d, grass width=%d", s.grass == image, s.grass != nil, s.renderWidth(), s.grassWidth())
		}
		want := read()
		s.deallocate()
		delete(w.sections, 0)
		w.startUpload(mesh)
		finish()
		s = w.sections[0]
		if !bytes.Equal(want, read()) {
			t.Fatal("mixed-width grass carving differs from reload")
		}
	}
}

func TestCarvingPrivateGrassKeepsTerrainProgress(t *testing.T) {
	data := carvePixelFixture()
	g := queryScene(data)
	options := render.RockGrowthOptions{Grass: 1}
	base := render.PrepareRockBase(data, ViewShaded, options)
	base.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	full := render.PrepareForeground(data, ViewShaded, options)
	full.Geometry = base.Geometry
	var root V
	found := false
	for _, a := range full.Foreground.Growth {
		if a.Kind == render.GrassAttachment && len(a.Anchors) > 0 {
			root, found = a.Anchors[0], true
			break
		}
	}
	if !found {
		t.Fatal("fixture has no grass")
	}
	u := &sectionUpload{data: base, grassData: &full, initialized: true, needed: render.ForegroundLayer | render.BackgroundLayer | grassUploadLayer,
		grassInitialized: true, grassFacesDone: true, grassCursor: uploadCursor{stage: uploadGrass, next: 3},
		uploadCursor: uploadCursor{stage: uploadForegroundOutlines, next: 8}, backgroundCursor: uploadCursor{stage: uploadBackgroundOutlines, next: 9}}
	g.world.upload = u
	root.Y += terrain.SectionTop(0)
	if _, err := g.CarveCircle(root, .006); err != nil {
		t.Fatal(err)
	}
	g.flushCarveImages()
	if u.grassInitialized || u.grassFacesDone || u.grassCursor.next != 0 {
		t.Fatal("private grass did not reconcile removed attachments")
	}
	if u.stage != uploadForegroundOutlines || u.next != 8 || u.backgroundCursor.stage != uploadBackgroundOutlines || u.backgroundCursor.next != 9 {
		t.Fatal("grass edit restarted unrelated terrain work")
	}
	if len(full.VisualCuts) == 0 {
		t.Fatal("late grass recipe did not receive the cut")
	}
}
