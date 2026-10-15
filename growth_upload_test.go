package infinicave

import (
	"bytes"
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
	if u.growth[underGrowth].initialized || u.growth[underGrowth].image != nil || u.growth[underGrowth].cursor.next != 0 {
		t.Fatal("damaged private hanging growth was not reconciled")
	}
	if !u.growth[surfaceGrowth].initialized || u.growth[surfaceGrowth].cursor.next != 7 || u.next != 8 || u.backgroundCursor.next != 9 {
		t.Fatal("hanging support removal restarted another product")
	}
}
