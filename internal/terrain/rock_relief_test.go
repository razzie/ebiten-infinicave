package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBackgroundMaterialDoesNotBakeForegroundShadows(t *testing.T) {
	background := RockGrid{{Center: geom.V{X: .5, Y: .55}, Polygon: []geom.V{{X: 0, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 2}, {X: 0, Y: 2}}, Normal: geom.V3{Z: 1}}}
	foreground := RockGrid{{Center: geom.V{X: .5, Y: .5}, Polygon: []geom.V{{X: .45, Y: .45}, {X: .55, Y: .45}, {X: .55, Y: .53}, {X: .45, Y: .53}}, Z: .07, Normal: geom.V3{Z: 1}, Raised: true}}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	shadeRockGrids(background, foreground, noise)
	withRock := background[0]
	shadeRockGrids(background, nil, noise)
	if !reflect.DeepEqual(withRock, background[0]) {
		t.Fatal("foreground silhouette changed the cached background material")
	}
	if background[0].Color != backgroundSurfaceColor(background[0].Center, noise, background[0].Normal) {
		t.Fatal("background still contains baked cast-shadow darkening")
	}
}

func TestArtisticFacetSelectionMatchesOverlappingWindows(t *testing.T) {
	a := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	b := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 1.4}, {X: 0.9, Y: 1.4}}, 1)}
	var sitesA, sitesB []geom.V
	for y := 0.41; y < 0.65; y += 0.025 {
		for x := 0.125; x < 0.875; x += 0.025 {
			sitesA = append(sitesA, geom.V{X: x, Y: y})
			sitesB = append(sitesB, geom.V{X: x, Y: y + 1})
		}
	}
	left, right := artisticRockSeeds(sitesA, a, 42, 0), artisticRockSeeds(sitesB, b, 42, -1)
	if len(left) >= len(sitesA) || len(left) != len(right) {
		t.Fatal("flank facet selection is not stable or has no effect")
	}
	for i, p := range left {
		q := right[i]
		q.Y -= 1
		if p.Sub(q).Len() > 1e-9 {
			t.Fatal("facet placement changes across windows")
		}
	}
}

func TestReliefProfileHasBevelCrestAndBroadFlank(t *testing.T) {
	const width, height, bevel = .180, .072, .016
	if ridgeProfile(-0.001, width, height, bevel) != 0 || ridgeProfile(width, width, height, bevel) != 0 {
		t.Fatal("relief extends outside its footprint")
	}
	if ridgeProfile(0, width, height, bevel) <= 0 || ridgeProfile(bevel, width, height, bevel) != height {
		t.Fatal("exposed lip has no thickness or crest")
	}
	for sample := 1; sample < 180; sample++ {
		d := float64(sample) * .001
		change := ridgeProfile(d, width, height, bevel) - ridgeProfile(d-0.001, width, height, bevel)
		if (d <= bevel && change <= 0) || (d > bevel && change >= 0) {
			t.Fatalf("relief changes slope at the wrong place: d=%v", d)
		}
	}
}

func TestReliefHeightAndSiteDeformationUseWorldCoordinates(t *testing.T) {
	a := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.5, Y: 0.35}, {X: 0.9, Y: 0.4}}, 1)}
	b := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.5, Y: 0.35}, {X: 0.9, Y: 0.4}}, 1)}
	b[0].translateY(1)
	n1, n2 := NewPerlin(rand.New(rand.NewSource(42))), NewPerlin(rand.New(rand.NewSource(42)))
	n2.OffsetY = -1
	for y := 0.35; y < 0.7; y += 0.013 {
		p, q := geom.V{X: 0.45, Y: y}, geom.V{X: 0.45, Y: y + 1}
		if math.Abs(reliefHeight(p, a, n1, nil)-reliefHeight(q, b, n2, nil)) > 1e-9 {
			t.Fatal("relief changes between generation windows")
		}
		x, z := reliefSeeds([]geom.V{p}, a)[0], reliefSeeds([]geom.V{q}, b)[0]
		z.Y -= 1
		if x.Sub(z).Len() > 1e-9 {
			t.Fatal("facet deformation changes between generation windows")
		}
	}
}

func TestReliefFadesBeforeHorizontalScreenInset(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: -0.1, Y: 0.5}, {X: 0.9, Y: 0.5}}, 1)}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	atEdge := reliefHeight(geom.V{X: 0, Y: 0.5}, guides, noise, nil)
	atInset := reliefHeight(geom.V{X: foregroundScreenInset, Y: 0.5}, guides, noise, nil)
	inset := reliefHeight(geom.V{X: 0.09, Y: 0.5}, guides, noise, nil)
	if atEdge != 0 || atInset >= rockContourHeight {
		t.Fatalf("foreground relief survives at the screen edge: edge=%v inset=%v", atEdge, atInset)
	}
	if inset <= rockContourHeight {
		t.Fatalf("edge fade erased interior foreground relief: %v", inset)
	}
}

func TestReliefTapersBeforeOpenGuideTips(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .2, Y: .5}, {X: .8, Y: .5}}, 1)
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, x := range []float64{.19, .2, .8, .81} {
		if height := reliefHeight(geom.V{X: x, Y: .51}, []Guide{guide}, noise, nil); height != 0 {
			t.Fatalf("open tip extends a raised cap beyond the guide at x=%v: height %v", x, height)
		}
	}
	for _, x := range []float64{.25, .5, .75} {
		if height := reliefHeight(geom.V{X: x, Y: .51}, []Guide{guide}, noise, nil); height <= rockContourHeight {
			t.Fatalf("tip taper erased the full-height ridge at x=%v: height %v", x, height)
		}
	}
}

func TestLightingSeparatesShapeFromMaterialAndVisibility(t *testing.T) {
	up := RockSurfaceColor(rockLight, 1, 1)
	front := RockSurfaceColor(geom.V3{Z: 1}, 1, 1)
	down := RockSurfaceColor((geom.V3{X: 0, Y: 1, Z: .3}).Norm(), 1, 1)
	shadow := RockSurfaceColor(rockLight, 0, .5)
	if up.R <= front.R || front.R <= down.R || shadow.R >= front.R || down.R <= 8 || down.R > 40 || up.R-shadow.R > 160 {
		t.Fatalf("lighting does not describe the relief: up=%v front=%v down=%v shadow=%v", up, front, down, shadow)
	}
	for _, c := range []color.NRGBA{up, front, down, shadow} {
		if c.A != 255 {
			t.Fatal("lighting makes solid rock transparent")
		}
	}
}
