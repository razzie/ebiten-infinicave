package infinicave

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestGrowthPublishesWithoutReplacingRock(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	data := carvePixelFixture()
	options := render.RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1}
	base := render.PrepareRockBase(data, ViewShaded, options)
	base.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	w.startUpload(base)
	for tick := 0; tick < 100 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	rock, trim, source := s.foreground, s.trim, s.rockSource
	full := render.PrepareForegroundGrowth(data, ViewShaded, base, options)
	full.Geometry = base.Geometry
	w.startUpload(full)
	for tick := 0; tick < 200 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	if w.upload != nil || !s.growthReady(64) {
		t.Fatal("growth did not publish")
	}
	if s.foreground != rock || s.trim != trim || s.rockSource != source {
		t.Fatal("growth replaced or re-rasterized structural rocks")
	}
	if s.growth[surfaceGrowth].image == nil {
		t.Fatal("moss fixture did not publish an image")
	}
}

func TestLateGrowthJoinsPrivateRockBase(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	data := carvePixelFixture()
	options := render.RockGrowthOptions{Moss: 1, Hanging: 1}
	base := render.PrepareRockBase(data, ViewShaded, options)
	base.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	base.Foreground.Outlines = make([]render.TriangleMesh, uploadDrawsPerTick*256)
	w.savePending(base)
	receiveTestBudget(g)
	u := w.upload
	private, next := u.foreground, u.next
	full := render.PrepareForegroundGrowth(data, ViewShaded, base, options)
	w.savePending(full)
	for tick := 0; tick < 100 && (w.sections[0] == nil || !w.sections[0].growthReady(64)); tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s == nil || !s.growthReady(64) || s.foreground != nil || w.upload != u {
		t.Fatal("late growth waited for the private rock base")
	}
	if u.foreground != private || u.next < next || !u.data.ForegroundBaseOnly {
		t.Fatal("late growth changed the structural cursor or recipe")
	}
}

func TestGrowthImagesRetainWidthDuringResize(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	data := carvePixelFixture()
	mesh := render.PrepareForeground(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Hanging: 1})
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	w.startUpload(mesh)
	for tick := 0; tick < 200 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	old := s.growth[surfaceGrowth].image
	g.SetRenderWidth(96)
	mesh.Foreground.Outlines = make([]render.TriangleMesh, uploadDrawsPerTick*256)
	w.startUpload(mesh)
	for tick := 0; tick < 200 && s.growth[surfaceGrowth].pixels != 96; tick++ {
		receiveTestBudget(g)
	}
	if s.growth[surfaceGrowth].pixels != 96 || s.growth[surfaceGrowth].image == old || s.renderWidth() != 64 {
		t.Fatal("growth did not resize independently of rocks")
	}
	replacement := s.growth[surfaceGrowth].image
	g.SetRenderWidth(128)
	if s.growth[surfaceGrowth].image != replacement || s.growth[surfaceGrowth].pixels != 96 || w.upload != nil {
		t.Fatal("resize discarded a published growth product")
	}
}

// Executed inside the existing pixel-test game loop. Compare the ordered
// texture span with the previous monolithic draw order, then carve and reload.
func checkGrowthCompositionPixels(t *testing.T) {
	t.Helper()
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, aa := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			g, err := NewScene(Config{DisableBackground: true, Orientation: orientation, Antialiasing: aa})
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			g.world.close()
			w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256, disableBackground: true, antialiasing: aa}
			g.world = w
			data := carvePixelFixture()
			data.Shape = terrain.ShapeOptions{EdgeRoughness: 1, SpikeDensity: .8}
			mesh := render.PrepareForeground(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1})
			mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
			finish := func() {
				for tick := 0; tick < 1000 && w.upload != nil; tick++ {
					receiveTestBudget(g)
				}
				if w.upload != nil {
					t.Fatal("growth composition upload stalled")
				}
			}
			w.startUpload(mesh)
			finish()
			read := func(img *ebiten.Image) []byte {
				pixels := make([]byte, 4*img.Bounds().Dx()*img.Bounds().Dy())
				img.ReadPixels(pixels)
				return pixels
			}
			draw := func() []byte {
				g.flushCarveImages()
				dst := ebiten.NewImage(256, 256)
				defer dst.Deallocate()
				w.DrawForegroundRocks(dst, -1, 1, 0, render.RenderTransform{Pixels: 256})
				return read(dst)
			}
			classic := ebiten.NewImage(256, 256)
			scaled := render.ScaleGridMesh(mesh.Foreground, 256, 0)
			cursor := uploadCursor{}
			budget := func() *uploadBudget { return &uploadBudget{start: time.Now().Add(time.Hour)} }
			for !w.uploadAttachments(g, &cursor, classic, scaled.Growth, false, true, budget()) {
			}
			cursor = uploadCursor{}
			for !w.uploadFaces(g, &cursor, classic, scaled.Faces, terrain.SectionWindowTop(0), true, budget()) {
			}
			for _, outline := range scaled.Outlines {
				outline.DrawWithAntialiasing(classic, w.white, aa.GeometryAA())
			}
			cursor = uploadCursor{}
			for !w.uploadAttachments(g, &cursor, classic, scaled.Growth, false, false, budget()) {
			}
			want, got := read(classic), draw()
			classic.Deallocate()
			maxDelta := 0
			for i, value := range want {
				delta := int(value) - int(got[i])
				if delta < 0 {
					delta = -delta
				}
				maxDelta = max(maxDelta, delta)
			}
			// Separate RGBA caches add one rounding boundary at each composition span.
			if maxDelta > 3 {
				t.Fatalf("%v/AA=%v: foreground order changed (maximum channel difference %d)", orientation, aa, maxDelta)
			}
			s := w.sections[0]
			rock, trim := s.foreground, s.trim
			if trim == nil {
				t.Fatal("fixture did not exercise structural trim")
			}
			before := draw()
			for _, cut := range []struct {
				point  V
				radius float64
			}{{V{X: .45, Y: -.65}, .04}, {V{X: .48, Y: -.65}, .035}, {V{X: .5, Y: -.375}, .025}, {V{X: .51, Y: -.375}, .022}, {V{X: .5, Y: -.1}, .02}} {
				if _, err := g.CarveCircle(terrain.WorldPoint(orientation, cut.point), cut.radius); err != nil {
					t.Fatal(err)
				}
			}
			if bytes.Equal(before, draw()) {
				t.Fatal("carving did not change the composed foreground")
			}
			after := draw()
			if s.foreground != rock || s.trim != trim {
				t.Fatal("carving replaced published structural targets")
			}
			s.deallocate()
			delete(w.sections, 0)
			w.startUpload(mesh)
			finish()
			reload := draw()
			// Native AA can quantize region edges slightly differently; cut interiors
			// and texture placement must still agree with regeneration.
			difference := 0
			for i := range after {
				if after[i] != reload[i] {
					difference++
				}
			}
			if difference > len(after)/200 {
				t.Fatalf("%v/AA=%v: growth/trim replay differs from reload at %d channels", orientation, aa, difference)
			}
		}
	}
}

func TestSurfaceGrowthUsesBudgetThatCannotFitUnderlay(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	for {
		budget := uploadBudget{start: time.Now().Add(time.Hour)}
		if w.prepareAtlas(render.MossAtlas, &budget) {
			break
		}
	}
	mesh := render.PrepareForeground(carvePixelFixture(), ViewShaded, render.RockGrowthOptions{Moss: 1, Hanging: 1})
	mesh.Geometry = w.sections[0].geometry
	// Existing terrain stays drawable; this upload needs only its details.
	w.sections[0].pixels = 64
	huge := mesh.Foreground.Faces
	huge.Texture = render.NoAtlas
	huge.Indices = make([]uint32, (uploadIndicesPerTick/3+1)*3)
	mesh.Foreground.Growth = append([]render.Attachment{{Kind: render.HangingAttachment, Underlay: true, Mesh: huge}}, mesh.Foreground.Growth...)
	w.savePending(mesh)
	budget := uploadBudget{start: time.Now().Add(time.Hour), draws: 1}
	w.receiveBudget(g, &budget)
	s := w.sections[0]
	if s.growth[surfaceGrowth].image == nil || s.growth[underGrowth].pixels != 0 || w.upload == nil {
		t.Fatal("a blocked underlay prevented runnable surface growth from publishing")
	}
	source := *s.mesh
	source.ForegroundBaseOnly = true // The cached manifest can still be the early base.
	s.mesh = &source
	if g.Readiness(Viewport{Y: -1, Height: 1}).Decorations {
		t.Fatal("unknown hanging growth counted as decoration-ready")
	}
}

func TestCarvingPrivateGrowthKeepsSiblingProgress(t *testing.T) {
	data := carvePixelFixture()
	g := queryScene(data)
	options := render.RockGrowthOptions{Moss: 1, Hanging: 1}
	base := render.PrepareRockBase(data, ViewShaded, options)
	base.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	full := render.PrepareForegroundGrowth(data, ViewShaded, base, options)
	root := V{X: .5, Y: .6}
	full.Foreground.Growth = append([]render.Attachment{{Kind: render.HangingAttachment, Underlay: true, Anchors: []V{root}, Min: root.Sub(V{X: .01, Y: .01}), Max: root.Add(V{X: .01, Y: .01})}}, full.Foreground.Growth...)
	full.Foreground.GrowthIndex = nil
	u := &sectionUpload{data: base, grassData: &full, initialized: true,
		needed:       render.ForegroundLayer | render.BackgroundLayer | underGrowthUploadLayer | surfaceGrowthUploadLayer,
		uploadCursor: uploadCursor{stage: uploadForegroundOutlines, next: 8}, backgroundCursor: uploadCursor{stage: uploadBackgroundOutlines, next: 9}}
	u.growth[underGrowth] = growthUpload{initialized: true, image: ebiten.NewImage(1, 1), cursor: uploadCursor{next: 3}}
	u.growth[surfaceGrowth] = growthUpload{initialized: true, cursor: uploadCursor{next: 7}}
	t.Cleanup(u.deallocate)
	g.world.upload = u
	root.Y += terrain.SectionTop(0)
	if _, err := g.CarveCircle(root, .002); err != nil {
		t.Fatal(err)
	}
	g.flushCarveImages()
	if u.growth[underGrowth].initialized || u.growth[underGrowth].image != nil || u.growth[underGrowth].cursor.next != 0 {
		t.Fatal("damaged private hanging growth was not reconciled")
	}
	if !u.growth[surfaceGrowth].initialized || u.growth[surfaceGrowth].cursor.next != 7 || u.next != 8 || u.backgroundCursor.next != 9 {
		t.Fatal("hanging support removal restarted another product")
	}
}

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

// Run in the GPU readback loop: coverage must extend past the structural
// silhouette, including rough crests and nonintegral raster coordinates.
func checkGrassEdgePixels(t *testing.T) {
	t.Helper()
	white := ebiten.NewImage(1, 1)
	white.Fill(color.White)
	defer white.Deallocate()
	for _, aa := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
		data := carvePixelFixture()
		data.Shape = terrain.ShapeOptions{EdgeRoughness: 1, SpikeDensity: 1}
		mesh := render.PrepareForeground(data, ViewShaded, render.RockGrowthOptions{Grass: 1})
		var patch render.Attachment
		for _, a := range mesh.Foreground.Growth {
			if a.Kind == render.GrassAttachment && a.Mesh.Texture == render.NoAtlas {
				patch = a
				break
			}
		}
		if len(patch.Mesh.Indices) == 0 {
			t.Fatal("edge fixture has no foreground sod")
		}
		for _, pixels := range []int{256, 501, 1000} {
			layer := ebiten.NewImage(pixels, pixels)
			scaled := render.ScaleGridMesh(render.GridMesh{Growth: []render.Attachment{patch}}, pixels, 0)
			scaled.Growth[0].Mesh.DrawWithAntialiasing(layer, white, aa.GeometryAA())
			rgba := make([]byte, 4*pixels*pixels)
			layer.ReadPixels(rgba)
			layer.Deallocate()
			for step := -10; step <= 10; step++ {
				x := int((patch.Anchor.X + float64(step)*.001) * float64(pixels))
				y := int((.35 - .0005) * float64(pixels))
				if alpha := rgba[4*(y*pixels+x)+3]; alpha < 200 {
					t.Fatalf("AA=%v width=%d: pale rock edge remains under sod at x=%d (coverage %d)", aa, pixels, x, alpha)
				}
			}
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

// Runs inside the carving test's game loop. A published plant must accept edits
// even while both its supporting rock image and sibling vines are unfinished.
func checkPartialVegetationCarvePixels(t *testing.T) {
	t.Helper()
	g, err := NewScene(Config{DisableBackground: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.world.close()
	data := carvePixelFixture()
	geometry := terrain.PrepareTerrainGeometry(data, 0)
	w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256,
		disableBackground: true, collision: map[int64]*terrain.Geometry{0: geometry}}
	g.world = w
	mesh := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{})
	mesh.Geometry = geometry
	render.FinishSectionMesh(&mesh)
	mesh.Foreground.Outlines = append(make([]render.TriangleMesh, uploadDrawsPerTick*128), mesh.Foreground.Outlines...)
	w.startUpload(mesh)
	for tick := 0; tick < 100 && (w.sections[0] == nil || w.sections[0].mushrooms == nil); tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s == nil || s.mushrooms == nil || s.foreground != nil || w.upload == nil {
		t.Fatal("fixture did not publish mushrooms before the unfinished rock image")
	}
	read := func() []byte {
		if s.mushrooms == nil {
			return nil
		}
		pixels := make([]byte, 4*s.mushrooms.Bounds().Dx()*s.mushrooms.Bounds().Dy())
		s.mushrooms.ReadPixels(pixels)
		return pixels
	}
	before := read()
	root := s.mesh.Plants[0].Anchors[0].Add(V{Y: terrain.SectionTop(0)})
	if _, err := g.CarveCircle(root, .006); err != nil {
		t.Fatal(err)
	}
	g.flushCarveImages()
	if !s.mesh.Plants[0].Dead || bytes.Equal(before, read()) {
		t.Fatal("carving left the already-published mushroom unchanged")
	}
	finish := func() {
		for tick := 0; tick < 500 && w.upload != nil; tick++ {
			receiveTestBudget(g)
		}
		if w.upload != nil {
			t.Fatal("partial-publication fixture stalled")
		}
	}
	finish()
	want := read()
	s.deallocate()
	delete(w.sections, 0)
	w.startUpload(mesh)
	finish()
	s = w.sections[0]
	if !bytes.Equal(want, read()) {
		t.Fatal("cutting an independently published product differs from replay on reload")
	}
	// Replay the same cut when the vegetation recipe arrives during an existing
	// terrain upload, rather than in its initial envelope.
	s.deallocate()
	delete(w.sections, 0)
	initial := mesh
	initial.Layers = render.ForegroundLayer | render.BackgroundVinesLayer
	initial.TerrainOnly = true
	w.startUpload(initial)
	receiveTestBudget(g)
	late := mesh
	late.Layers = render.MushroomsLayer | render.WebsLayer | render.ForegroundVinesLayer
	w.savePending(late)
	finish()
	s = w.sections[0]
	if !bytes.Equal(want, read()) {
		t.Fatal("late CPU recipe restored carved vegetation")
	}

}

// Ignore wall-clock submission time in scheduler tests. Draw/index/pixel limits
// still apply, so assertions don't depend on shader warmup or machine speed.
func receiveTestBudget(g *Scene) uploadBudget {
	b := uploadBudget{start: time.Now().Add(time.Hour)}
	g.world.receiveBudget(g, &b)
	return b
}

func TestVegetationPublishesWithoutWaitingForVines(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	w.appearanceFade = &render.FadeRenderer{} // Publication clocks need no shader.
	data := carvePixelFixture()
	mesh := render.PrepareVegetation(data, ViewShaded)
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	mesh.Vines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), mesh.Vines...)
	w.sections[0].vegetationPending = true
	w.savePending(mesh)
	for tick := 0; tick < 100 && w.sections[0].mushrooms == nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s.mushrooms == nil || s.vines != nil || !s.vegetationPending {
		t.Fatal("mushrooms waited for the unfinished background vine product")
	}
	if s.plantWidth(mushroomsProduct) != 64 || s.plantReveal(mushroomsProduct).Progress() != 0 {
		t.Fatal("publication did not set the product's width and reveal clock")
	}
	mushrooms := s.mushrooms
	for range 10 {
		s.plantReveal(mushroomsProduct).Update()
	}
	progress := s.plantReveal(mushroomsProduct).Progress()
	for tick := 0; tick < 200 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	if w.upload != nil || s.vegetationPending || s.vines == nil {
		t.Fatal("remaining vegetation did not complete")
	}
	if s.mushrooms != mushrooms || s.plantReveal(mushroomsProduct).Progress() != progress || s.plantReveal(backgroundVinesProduct).Progress() != 0 {
		t.Fatal("a sibling publication replaced the mushroom image or restarted its reveal")
	}
}

func TestReadySectionsShareUploadQuanta(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	for id := int64(0); id < 2; id++ {
		mesh := resolutionTestMesh(id)
		mesh.Layers = render.VegetationLayer
		mesh.Vines = make([]render.TriangleMesh, uploadDrawsPerTick*8)
		mesh.VinesBounds = image.Rect(10, 1000, 330, 1320)
		w.sections[id].vegetationPending = true
		w.savePending(mesh)
	}
	b := receiveTestBudget(g)
	progress := 0
	for id := int64(0); id < 2; id++ {
		u := w.parked[id]
		if w.upload != nil && w.upload.data.ID == id {
			u = w.upload
		}
		if u == nil || u.plants[backgroundVinesProduct].cursor.next == 0 {
			t.Fatal("an equally urgent section monopolized the upload budget")
		}
		progress += u.plants[backgroundVinesProduct].cursor.next
	}
	if progress > uploadDrawsPerTick || b.draws > uploadDrawsPerTick {
		t.Fatal("sections did not share the same draw budget")
	}
}

func TestSmallQueuedProductsPublishInOneUpdate(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 16
	for _, id := range []int64{10, 11} {
		mesh := resolutionTestMesh(id)
		mesh.Layers, mesh.TerrainOnly = render.ForegroundLayer, true
		w.savePending(mesh)
	}
	receiveTestBudget(g)
	for _, id := range []int64{10, 11} {
		if s := w.sections[id]; s == nil || s.foreground == nil {
			t.Fatal("starting or publishing another product introduced a mandatory tick")
		}
	}
	if w.upload != nil || len(w.parked) != 0 || len(w.pending) != 0 {
		t.Fatal("completed products retained staging ownership")
	}
}

func TestSmallProductUsesBudgetThatCannotFitTerrain(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	mesh := resolutionTestMesh(0)
	mesh.Foreground.Growth = nil
	mesh.MushroomsBounds = image.Rect(10, 1000, 30, 1020)
	w.savePending(mesh)
	// The 2x rock pass cannot fit; the cropped mushroom product can.
	b := uploadBudget{start: time.Now().Add(time.Hour), draws: 1, pixels: 3 * 1024 * 1024}
	w.receiveBudget(g, &b)
	s := w.sections[0]
	if s == nil || s.mushrooms == nil || s.foreground != nil {
		t.Fatal("a terrain pass blocked an independently runnable small product")
	}
}

func TestVegetationResizeKeepsEachPublishedWidth(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	mesh := render.PrepareVegetation(carvePixelFixture(), ViewShaded)
	mesh.Geometry = w.sections[0].geometry
	w.sections[0].vegetationPending = true
	w.startUpload(mesh)
	for tick := 0; tick < 100 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s.mushrooms == nil || s.vines == nil {
		t.Fatal("fixture did not publish vegetation")
	}
	oldVines := s.vines
	g.SetRenderWidth(96)
	mesh.Vines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), mesh.Vines...)
	w.startUpload(mesh)
	for tick := 0; tick < 100 && s.plantWidth(mushroomsProduct) != 96; tick++ {
		receiveTestBudget(g)
	}
	if s.plantWidth(mushroomsProduct) != 96 || s.plantWidth(backgroundVinesProduct) != 64 || s.vines != oldVines {
		t.Fatal("partial resize changed an unfinished product's displayed width or fallback")
	}
	g.SetRenderWidth(128)
	if w.upload != nil || len(w.parked) != 0 || s.plantWidth(mushroomsProduct) != 96 || s.vines != oldVines {
		t.Fatal("resize discarded published products instead of private staging")
	}
}

func TestLateVegetationJoinsActiveUpload(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	data := carvePixelFixture()
	vines := render.PrepareVegetationParts(data, ViewShaded, terrain.BackgroundVinesPart)
	vines.Geometry = w.sections[0].geometry
	vines.Vines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), vines.Vines...)
	w.sections[0].vegetationPending = true
	w.savePending(vines)
	receiveTestBudget(g)
	u := w.upload
	if u == nil || u.plants[backgroundVinesProduct].cursor.next == 0 {
		t.Fatal("fixture did not start vines")
	}
	next := u.plants[backgroundVinesProduct].cursor.next
	mushrooms := render.PrepareVegetationParts(data, ViewShaded, terrain.MushroomsPart)
	mushrooms.Geometry = vines.Geometry
	w.savePending(mushrooms)
	for tick := 0; tick < 20 && w.sections[0].mushrooms == nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s.mushrooms == nil || w.upload != u || s.vines != nil {
		t.Fatal("late mushrooms waited for the active vine upload")
	}
	if u.plants[backgroundVinesProduct].cursor.next < next {
		t.Fatal("late recipe restarted vine progress")
	}
	if !s.vegetationPending {
		t.Fatal("unknown products counted as complete")
	}
	image := s.mushrooms
	// A final cumulative envelope repeats recipes without replacing their images.
	rest := render.PrepareVegetationParts(data, ViewShaded, terrain.WebsPart|terrain.ForegroundVinesPart)
	rest.Geometry = vines.Geometry
	final := render.MergeSectionMeshes(render.MergeSectionMeshes(vines, mushrooms), rest)
	w.savePending(final)
	for tick := 0; tick < 300 && (w.upload != nil || len(w.pending) != 0); tick++ {
		receiveTestBudget(g)
	}
	if w.upload != nil || s.vegetationPending || s.mushrooms != image {
		t.Fatal("final envelope stalled or republished an unchanged product")
	}
}

func TestEmptyVegetationProductsRemainIndependent(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	s := w.sections[0]
	s.vegetationPending = true
	for kind := vegetationProduct(0); kind < vegetationProducts; kind++ {
		mesh := render.SectionMesh{ID: 0, Layers: kind.layer(), TerrainOnly: true, Geometry: s.geometry}
		mesh.PlantRecipes[kind] = new(uint8)
		w.savePending(mesh)
		receiveTestBudget(g)
		if s.plants[kind].pixels != 64 {
			t.Fatalf("empty product %v was not published", kind)
		}
		if s.vegetationPending != (kind != foregroundVinesProduct) {
			t.Fatal("empty product completed its unknown siblings")
		}
		if s.needsRefresh(64, kind.layer()) {
			t.Fatal("published empty product schedules repeated refreshes")
		}
	}
}

func TestCancelAfterAllPlantsBeforeGrowthPreservesPublishedProducts(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	// All plants can finish while the foreground growth recipe is still running.
	source := resolutionTestMesh(0)
	source.ForegroundBaseOnly = true
	s := w.sections[0]
	s.mesh = &source
	s.vegetationPending = false
	if w.ensure(-.8, .8, 0) {
		t.Fatal("full readiness ignored unfinished foreground growth")
	}
	// Pending vegetation on its own is also incomplete, even with all four bits.
	plants := render.SectionMesh{ID: 0, Layers: render.VegetationLayer}
	w.pending = map[int64]render.SectionMesh{0: plants}
	w.discardUnpublished(0)
	if w.sections[0] != s || len(w.pending) != 0 {
		t.Fatal("cancellation removed published products or retained private recipes")
	}
}
