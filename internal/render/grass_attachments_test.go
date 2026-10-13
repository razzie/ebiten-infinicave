package render

import (
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestGrassPatchFollowsSurfaceAndBladesGrowUp(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		angle := .65
		anchor := geom.V{X: .5, Y: .5}
		point := func(p geom.V) geom.V { return anchor.Add(terrain.InternalPoint(mode, geom.Rotate(p, angle))) }
		poly := []geom.V{point(geom.V{X: -.06}), point(geom.V{X: .06}), point(geom.V{X: .06, Y: .06}), point(geom.V{X: -.06, Y: .06})}
		n := terrain.InternalPoint(mode, geom.Rotate(geom.V{Y: -.8}, angle+.30))
		c := terrain.WithRockOrientation(terrain.RockCell{Polygon: poly, Center: geom.PolygonCenter(poly), Normal: geom.V3{X: n.X, Y: n.Y, Z: .6}, Color: color.NRGBA{R: 140, A: 255}, Raised: true}, mode)
		guide := terrain.Guide{Pts: []geom.V{poly[0], poly[1]}}
		patch, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
		if !ok || len(patch.Mesh.Indices) == 0 || len(blades.Mesh.Indices) == 0 {
			t.Fatal("missing sloped grass")
		}
		if math.Abs(patch.Rotation-angle) > 1e-9 {
			t.Fatal("patch ignores the geometric edge")
		}
		// Lighting can change independently of the geometric supporting edge.
		changed := c
		n = terrain.InternalPoint(mode, geom.Rotate(geom.V{Y: -.8}, angle-.25))
		changed.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		other, _, ok := prepareGrassAttachments(changed, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
		if !ok || !reflect.DeepEqual(patch.Anchors, other.Anchors) || !reflect.DeepEqual(patch.Footprint, other.Footprint) {
			t.Fatal("lighting rotated or relocated the grass base")
		}
		for _, root := range patch.Anchors {
			inside := false
			for _, piece := range patch.Footprint {
				if geom.InsidePolygon(root, piece) {
					inside = true
					break
				}
			}
			if !inside {
				t.Fatal("blade root lies outside the ground strip")
			}
		}
		for _, v := range patch.Mesh.Vertices {
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			local := geom.Rotate(terrain.WorldPoint(mode, p.Sub(anchor)), -angle)
			if local.Y < -1e-6 || local.Y > .009 {
				t.Fatal("ground patch floats away from its facet")
			}
		}
		for i := 0; i < len(blades.Mesh.Vertices); i += 4 {
			a, b := blades.Mesh.Vertices[i], blades.Mesh.Vertices[i+1]
			d := terrain.WorldPoint(mode, geom.V{X: float64(b.DstX - a.DstX), Y: float64(b.DstY - a.DstY)})
			if math.Abs(math.Atan2(d.Y, d.X)) > .3 {
				t.Fatal("blades rotate with the whole slope instead of growing up")
			}
		}
		// Body-only damage is handled by texture erasure, with no deletion.
		var tip geom.V
		bestY := math.Inf(1)
		for _, v := range blades.Mesh.Vertices {
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			if y := terrain.WorldPoint(mode, p).Y; y < bestY {
				tip, bestY = p, y
			}
		}
		cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: tip, Radius: .0003})
		if err != nil {
			t.Fatal(err)
		}
		if patch.Hit(cut, 0) || blades.Hit(cut, 0) {
			t.Fatal("blade-tip damage removed a supported tuft")
		}
		cut, _ = terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: patch.Anchors[0], Radius: .0003})
		if !patch.Hit(cut, 0) || !blades.Hit(cut, 0) {
			t.Fatal("support damage separated grass blades from their patch")
		}
	}
}

func TestGrassBaseFitsOffsetGuideAndCellCorner(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		point := func(p geom.V) geom.V { return geom.V{X: .5, Y: .5}.Add(terrain.InternalPoint(mode, p)) }
		poly := []geom.V{point(geom.V{X: -.04}), point(geom.V{}), point(geom.V{X: .04, Y: .018}), point(geom.V{X: .04, Y: .08}), point(geom.V{X: -.04, Y: .08})}
		c := terrainRock(poly)
		c = terrain.WithRockOrientation(c, mode)
		n := terrain.InternalPoint(mode, geom.V{Y: -.8})
		c.Normal = geom.V3{X: n.X, Y: n.Y, Z: .6}
		guide := terrain.Guide{Pts: []geom.V{point(geom.V{X: -.04, Y: -.002}), point(geom.V{Y: -.002}), point(geom.V{X: .04, Y: .016})}}
		patch, blades, ok := prepareGrassAttachments(c, RockGrowthOptions{Grass: 1, Guides: []terrain.Guide{guide}})
		if !ok {
			t.Fatal("supported corner lost its tuft")
		}
		for _, m := range []TriangleMesh{patch.Mesh, blades.Mesh} {
			checkMesh(t, m)
		}
		for _, v := range patch.Mesh.Vertices {
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			if !geom.InsidePolygon(p, c.Polygon) {
				t.Fatal("ground strip crosses the cell boundary")
			}
		}
		for _, root := range patch.Anchors {
			_, distance := guide.NearestPoint(root)
			if distance > .010 {
				t.Fatal("root no longer follows the eligible crest")
			}
		}
	}
}
