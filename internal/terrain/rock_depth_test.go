package terrain

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestRaisedRockCastsShadowAndOccludesAmbientLight(t *testing.T) {
	background := RockGrid{{Center: geom.V{X: 0.5, Y: 1.5}, Polygon: []geom.V{{X: 0, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0, Y: generationMaxY}}, Normal: geom.V3{Z: 1}}}
	block := RockGrid{{Center: geom.V{X: 0.5, Y: 0.5}, Polygon: []geom.V{{X: 0.47, Y: 0.47}, {X: 0.53, Y: 0.47}, {X: 0.53, Y: 0.53}, {X: 0.47, Y: 0.53}}, Z: .070, Normal: geom.V3{Z: 1}, Raised: true}}
	d := newRockDepth(background, block)
	away := (geom.V{X: -rockLight.X, Y: -rockLight.Y}).Norm()
	shadowed := geom.V{X: 0.5, Y: 0.5}.Add(away.Mul(.055))
	lit := geom.V{X: 0.5, Y: 0.5}.Sub(away.Mul(.055))
	if d.visibility(shadowed, 0) > .1 || d.visibility(lit, 0) < .99 {
		t.Fatal("raised rock does not cast a directional shadow on lower terrain")
	}
	if d.ambient(geom.V{X: 0.535, Y: 0.5}, 0) >= d.ambient(geom.V{X: 0.75, Y: 0.5}, 0) {
		t.Fatal("contact with raised rock does not darken ambient light")
	}
	if d.visibility(geom.V{X: 0.5, Y: 0.5}, .070) < .99 {
		t.Fatal("flat exposed top shadows itself")
	}
	block[0].Z = 0
	flat := newRockDepth(background, block)
	if flat.visibility(shadowed, 0) < .99 {
		t.Fatal("zero-height rock still casts the raised rock's shadow")
	}
}
