package infinicave

import (
	"image/color"
	"math"
	"testing"
	"time"
)

func hoverRock(poly []V) RockCell {
	return RockCell{Polygon: poly, Center: faceCenter(poly), Raised: true, Color: color.NRGBA{A: 255}}
}

func hoverRect(x, y, width, height float64) RockCell {
	return hoverRock([]V{{x, y}, {x + width, y}, {x + width, y + height}, {x, y + height}})
}

func TestHoverConnectedBlocks(t *testing.T) {
	grid := RockGrid{
		hoverRect(30, 1100, 20, 40),
		// Two shorter edges share the first cell's long edge.
		hoverRect(50, 1100, 30, 20),
		hoverRect(50, 1120, 30, 20),
		// Corner contact and empty space must not join formations.
		hoverRect(80, 1140, 20, 20),
		hoverRect(130, 1100, 30, 40),
		// Deep concavity: its empty notch remains unselectable.
		hoverRock([]V{{200, 1100}, {240, 1100}, {240, 1110}, {210, 1110}, {210, 1140}, {200, 1140}}),
	}
	hidden := hoverRect(300, 1100, 40, 40)
	hidden.Color.A = 0
	grid = append(grid, hidden)
	background := hoverRect(350, 1100, 40, 40)
	background.Raised = false
	grid = append(grid, background)
	h := prepareTerrainGeometry(sectionData{foreground: grid}, 0)
	if len(h.blocks) != 4 {
		t.Fatalf("got %d blocks, want 4", len(h.blocks))
	}
	first := h.hit(V{40, 1110})
	for _, p := range []V{{60, 1110}, {60, 1130}, {50, 1120}} {
		if got := h.hit(p); got != first {
			t.Fatalf("connected face at %v did not select the entire block", p)
		}
	}
	for _, p := range []V{{90, 1150}, {140, 1110}, {205, 1130}} {
		if got := h.hit(p); got.geometry == nil || got == first {
			t.Fatalf("separate block at %v selected incorrectly", p)
		}
	}
	for _, p := range []V{{120, 1120}, {230, 1130}, {320, 1120}, {370, 1120}} {
		if got := h.hit(p); got.geometry != nil {
			t.Fatalf("empty, transparent, or background point %v selected a block", p)
		}
	}
}

func TestHoverGuidePriorityAndScreenInset(t *testing.T) {
	g := splineGuide([]V{{30, 1100}, {100, 1100}}, 1)
	h := prepareTerrainGeometry(sectionData{
		foreground: RockGrid{hoverRect(0, 1080, 120, 60)}, guides: []Guide{g},
	}, 0)
	for _, p := range []V{{60, 1100}, {60, 1106}, {27, 1100}} {
		if got := h.hit(p); got.geometry != h || !got.guide {
			t.Fatalf("point %v should highlight only the guide", p)
		}
	}
	if got := h.hit(V{60, 1107}); got.geometry != h || got.guide {
		t.Fatal("rock outside the guide hover margin did not select its block")
	}
	if got := h.hit(V{17, 1100}); got.geometry != nil {
		t.Fatal("inset rock margin was selectable")
	}
	if h.faces[0].min.X != foregroundScreenInset {
		t.Fatal("hover geometry differs from the rendered rock inset")
	}
}

func TestHoverWorldCoordinatesAndMissingSections(t *testing.T) {
	w := &world{sections: map[int64]*worldSection{}}
	for _, id := range []int64{0, 1} {
		h := prepareTerrainGeometry(sectionData{id: id, foreground: RockGrid{hoverRect(30, 1000, 30, 30)}}, 0)
		w.sections[id] = &worldSection{geometry: h}
	}
	for _, tc := range []struct {
		camera float64
		cursor V
		id     int64
	}{
		{-1000.49, V{40, 10}, 0},
		{-1000.49, V{40, 0}, 0},
		{-2000.49, V{40, 10}, 1},
		{-1500, V{40, 510}, 0},
	} {
		target := w.hoverAt(tc.cursor, tc.camera, 800)
		if target.geometry != w.sections[tc.id].geometry {
			t.Fatalf("camera %v and cursor %v did not match the rendered section", tc.camera, tc.cursor)
		}
	}
	for _, tc := range []struct {
		camera float64
		cursor V
	}{
		{-3000, V{40, 10}}, // missing section
		{-800, V{-1, 10}},
		{-800, V{W, 10}},
		{-800, V{40, -1}},
		{-800, V{40, 800}},
		{0, V{40, 0}}, // below world floor
	} {
		if target := w.hoverAt(tc.cursor, tc.camera, 800); target.geometry != nil {
			t.Fatalf("out-of-view cursor %v with camera %v selected terrain", tc.cursor, tc.camera)
		}
	}
}

func TestHoverBlockAcrossSections(t *testing.T) {
	// Both windows contain the same cells crossing the seam at world Y=-1000.
	// A connected cell only in section 1 extends the selected formation there.
	data := []sectionData{
		{id: 0, foreground: RockGrid{hoverRect(30, 980, 30, 40), hoverRect(100, 980, 30, 40)}},
		{id: 1, foreground: RockGrid{hoverRect(30, 1980, 30, 40), hoverRect(30, 1940, 30, 40), hoverRect(100, 1980, 30, 40)}},
	}
	w := &world{sections: map[int64]*worldSection{}}
	for _, d := range data {
		w.sections[d.id] = &worldSection{geometry: prepareTerrainGeometry(d, 0)}
	}
	target := w.sections[0].geometry.hit(V{40, 1010})
	polys := w.hoverPolygons(target, 0, 1)
	area := 0.0
	for _, poly := range polys {
		area += faceArea(poly)
		for _, p := range poly {
			if p.X > 60 {
				t.Fatal("unrelated formation was included across the section seam")
			}
		}
	}
	if math.Abs(area-2400) > 1e-6 {
		t.Fatalf("highlighted area %v, want the entire 2400 pixel formation without duplicated overlaps", area)
	}
	// Reloading the neighbor must discover its new geometry, not keep stale pointers.
	w.sections[1].geometry = prepareTerrainGeometry(data[1], 0)
	if got := w.hoverPolygons(target, 0, 1); len(got) != len(polys) {
		t.Fatal("reloaded neighbor broke connected hover highlighting")
	}
}

func TestHoverShaderAndOverlayCache(t *testing.T) {
	r, err := newHoverRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	h := prepareTerrainGeometry(sectionData{foreground: RockGrid{hoverRect(30, 1100, 30, 30), hoverRect(60, 1100, 30, 30)}}, 0)
	w := &world{sections: map[int64]*worldSection{0: {geometry: h}}}
	r.selectTarget(h.hit(V{40, 1110}), w, 0, 0)
	if r.image == nil || r.image.Bounds().Dx() != 92 {
		t.Fatal("overlay did not span the entire connected block plus glow padding")
	}
	image := r.image
	r.selectTarget(h.hit(V{70, 1110}), w, 0, 0)
	if r.image != image {
		t.Fatal("moving between connected cells rebuilt the hover overlay")
	}
	w.revision++
	r.selectTarget(h.hit(V{70, 1110}), w, 0, 0)
	if r.image == image {
		t.Fatal("newly loaded terrain did not invalidate the overlay")
	}
	r.selectTarget(hoverTarget{}, w, 0, 0)
	if r.image != nil || r.target.geometry != nil {
		t.Fatal("moving into empty space retained a hover highlight")
	}
}

func TestDiagnosticWorkerRetainsTerrainGeometry(t *testing.T) {
	w := newWorld(42, StudyLedge, ViewClay, 0)
	defer w.close()
	w.request(0)
	select {
	case mesh := <-w.results:
		if mesh.geometry == nil || len(mesh.geometry.blocks) == 0 || len(mesh.geometry.guides) == 0 || len(mesh.geometry.collision.Polygons) == 0 {
			t.Fatal("diagnostic view lost terrain or collision geometry")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("hover geometry preparation stalled")
	}
}
