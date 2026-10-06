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
		hoverRect(0.03, 0.1, 0.02, 0.04),
		// Two shorter edges share the first cell's long edge.
		hoverRect(0.05, 0.1, 0.03, 0.02),
		hoverRect(0.05, 0.12, 0.03, 0.02),
		// Corner contact and empty space must not join formations.
		hoverRect(0.08, 0.14, 0.02, 0.02),
		hoverRect(0.13, 0.1, 0.03, 0.04),
		// Deep concavity: its empty notch remains unselectable.
		hoverRock([]V{{0.2, 0.1}, {0.24, 0.1}, {0.24, 0.11}, {0.21, 0.11}, {0.21, 0.14}, {0.2, 0.14}}),
	}
	hidden := hoverRect(0.3, 0.1, 0.04, 0.04)
	hidden.Color.A = 0
	grid = append(grid, hidden)
	background := hoverRect(0.35, 0.1, 0.04, 0.04)
	background.Raised = false
	grid = append(grid, background)
	h := prepareTerrainGeometry(sectionData{foreground: grid}, 0)
	if len(h.blocks) != 4 {
		t.Fatalf("got %d blocks, want 4", len(h.blocks))
	}
	first := h.hit(V{0.04, 0.11})
	for _, p := range []V{{0.06, 0.11}, {0.06, 0.13}, {0.05, 0.12}} {
		if got := h.hit(p); got != first {
			t.Fatalf("connected face at %v did not select the entire block", p)
		}
	}
	for _, p := range []V{{0.09, 0.15}, {0.14, 0.11}, {0.205, 0.13}} {
		if got := h.hit(p); got.geometry == nil || got == first {
			t.Fatalf("separate block at %v selected incorrectly", p)
		}
	}
	for _, p := range []V{{0.12, 0.12}, {0.23, 0.13}, {0.32, 0.12}, {0.37, 0.12}} {
		if got := h.hit(p); got.geometry != nil {
			t.Fatalf("empty, transparent, or background point %v selected a block", p)
		}
	}
}

func TestHoverGuidePriorityAndScreenInset(t *testing.T) {
	g := splineGuide([]V{{0.03, 0.1}, {0.1, 0.1}}, 1)
	h := prepareTerrainGeometry(sectionData{
		foreground: RockGrid{hoverRect(0, 0.08, 0.12, 0.06)}, guides: []Guide{g},
	}, 0)
	for _, p := range []V{{0.06, 0.1}, {0.06, 0.106}, {0.027, 0.1}} {
		if got := h.hit(p); got.geometry != h || !got.guide {
			t.Fatalf("point %v should highlight only the guide", p)
		}
	}
	if got := h.hit(V{0.06, 0.107}); got.geometry != h || got.guide {
		t.Fatal("rock outside the guide hover margin did not select its block")
	}
	if got := h.hit(V{0.017, 0.1}); got.geometry != nil {
		t.Fatal("inset rock margin was selectable")
	}
	if h.faces[0].min.X != foregroundScreenInset {
		t.Fatal("hover geometry differs from the rendered rock inset")
	}
}

func TestHoverWorldCoordinatesAndMissingSections(t *testing.T) {
	w := &world{sections: map[int64]*worldSection{}}
	for _, id := range []int64{0, 1} {
		h := prepareTerrainGeometry(sectionData{id: id, foreground: RockGrid{hoverRect(0.03, 0, 0.03, 0.03)}}, 0)
		w.sections[id] = &worldSection{geometry: h}
	}
	for _, tc := range []struct {
		camera float64
		cursor V
		id     int64
	}{
		{-1.00049, V{0.04, 0.01}, 0},
		{-1.00049, V{0.04, 0}, 0},
		{-2.00049, V{0.04, 0.01}, 1},
		{-1.500, V{0.04, 0.51}, 0},
	} {
		target := w.hoverAt(tc.cursor, tc.camera, .800)
		if target.geometry != w.sections[tc.id].geometry {
			t.Fatalf("camera %v and cursor %v did not match the rendered section", tc.camera, tc.cursor)
		}
	}
	for _, tc := range []struct {
		camera float64
		cursor V
	}{
		{-3, V{0.04, 0.01}}, // missing section
		{-.800, V{-0.001, 0.01}},
		{-.800, V{generationWidth, 0.01}},
		{-.800, V{0.04, -0.001}},
		{-.800, V{0.04, 0.8}},
		{0, V{0.04, 0}}, // below world floor
	} {
		if target := w.hoverAt(tc.cursor, tc.camera, .800); target.geometry != nil {
			t.Fatalf("out-of-view cursor %v with camera %v selected terrain", tc.cursor, tc.camera)
		}
	}
}

func TestHoverBlockAcrossSections(t *testing.T) {
	// Both windows contain the same cells crossing the seam at world Y=-1.
	// A connected cell only in section 1 extends the selected formation there.
	data := []sectionData{
		{id: 0, foreground: RockGrid{hoverRect(0.03, -0.02, 0.03, 0.04), hoverRect(0.1, -0.02, 0.03, 0.04)}},
		{id: 1, foreground: RockGrid{hoverRect(0.03, 0.98, 0.03, 0.04), hoverRect(0.03, 0.94, 0.03, 0.04), hoverRect(0.1, 0.98, 0.03, 0.04)}},
	}
	w := &world{sections: map[int64]*worldSection{}}
	for _, d := range data {
		w.sections[d.id] = &worldSection{geometry: prepareTerrainGeometry(d, 0)}
	}
	target := w.sections[0].geometry.hit(V{0.04, 0.01})
	polys := w.hoverPolygons(target, 0, 1)
	area := 0.0
	for _, poly := range polys {
		area += faceArea(poly)
		for _, p := range poly {
			if p.X > .060 {
				t.Fatal("unrelated formation was included across the section seam")
			}
		}
	}
	if math.Abs(area-.0024) > 1e-12 {
		t.Fatalf("highlighted area %v, want the entire 0.0024-unit² formation without duplicated overlaps", area)
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
	h := prepareTerrainGeometry(sectionData{foreground: RockGrid{hoverRect(0.03, 0.1, 0.03, 0.03), hoverRect(0.06, 0.1, 0.03, 0.03)}}, 0)
	w := &world{sections: map[int64]*worldSection{0: {geometry: h}}}
	r.selectTarget(h.hit(V{0.04, 0.11}), w, 0, 0)
	if r.image == nil || r.image.Bounds().Dx() != 92 {
		t.Fatal("overlay did not span the entire connected block plus glow padding")
	}
	image := r.image
	r.selectTarget(h.hit(V{0.07, 0.11}), w, 0, 0)
	if r.image != image {
		t.Fatal("moving between connected cells rebuilt the hover overlay")
	}
	w.revision++
	r.selectTarget(h.hit(V{0.07, 0.11}), w, 0, 0)
	if r.image == image {
		t.Fatal("newly loaded terrain did not invalidate the overlay")
	}
	r.selectTarget(hoverTarget{}, w, 0, 0)
	if r.image != nil || r.target.geometry != nil {
		t.Fatal("moving into empty space retained a hover highlight")
	}
}

func TestDiagnosticWorkerRetainsTerrainGeometry(t *testing.T) {
	w := newWorld(42, ViewClay, 0, testLedgeSection)
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
