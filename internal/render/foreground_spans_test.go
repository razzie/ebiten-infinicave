package render

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestForegroundGrowthReusesStructureAndMatchesCompleteRecipe(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for _, authored := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/authored=%v", mode, authored), func(t *testing.T) {
				point := func(p geom.V) geom.V {
					if mode == terrain.Horizontal {
						return geom.V{X: 1 - p.Y, Y: p.X}
					}
					return p
				}
				var loader terrain.SectionLoader
				if authored {
					loader = func(int64) terrain.SectionContent {
						return terrain.SectionContent{Guides: []terrain.Guide{terrain.SplineGuide([]geom.V{point(geom.V{X: .1, Y: .4}), point(geom.V{X: .9, Y: .4})}, 42)}, Holes: []terrain.Hole{{Shape: terrain.HoleCircle, Center: point(geom.V{X: .5, Y: .4}), Radius: .04}}}
					}
				}
				builder := terrain.NewSectionBuilder(42, loader, mode)
				builder.Shape = terrain.ShapeOptions{EdgeRoughness: .7, SpikeDensity: .8}
				build := builder.Begin(context.Background(), 0)
				if build == nil || !build.MaterializeForeground(context.Background()) {
					t.Fatal("early foreground failed")
				}
				options := RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1}
				data := build.Data()
				base := PrepareRockBase(data, ViewShaded, options)
				before := PrepareRockBase(data, ViewShaded, options)
				earlyGrowth := PrepareForegroundGrowth(data, ViewShaded, base, options)
				if !build.Materialize(context.Background()) {
					t.Fatal("material failed")
				}
				got := PrepareForegroundGrowth(build.Data(), ViewShaded, base, options)
				want := PrepareForeground(build.Data(), ViewShaded, options)
				if !reflect.DeepEqual(earlyGrowth, want) {
					t.Fatal("growth depended on background compatibility material")
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("growth-only preparation changed geometry, IDs, order, or authored cuts")
				}
				if !reflect.DeepEqual(base, before) {
					t.Fatal("growth preparation mutated the published base")
				}
				if len(base.Foreground.Faces.Vertices) == 0 || &base.Foreground.Faces.Vertices[0] != &got.Foreground.Faces.Vertices[0] {
					t.Fatal("growth rebuilt structural vertices")
				}
				if &base.Foreground.Faces.Indices[0] != &got.Foreground.Faces.Indices[0] {
					t.Fatal("growth rebuilt structural indices")
				}
			})
		}
	}
}

func TestForegroundMasksRetainSupportIndices(t *testing.T) {
	grid := GridMesh{Growth: []Attachment{
		{Kind: HangingAttachment, Underlay: true}, {Kind: EdgeAttachment, Underlay: true},
		{Kind: SurfaceMoss}, {Kind: EdgeAttachment}, {Kind: GrassAttachment},
	}}
	grid.GrowthIndex = AttachmentIndex(grid.Growth)
	for _, span := range []ForegroundSpan{StructuralSpan, StructuralTopSpan, UnderGrowthSpan, SurfaceGrowthSpan} {
		masked := MaskForeground(grid, span)
		if len(masked.Growth) != len(grid.Growth) || masked.GrowthIndex != grid.GrowthIndex {
			t.Fatal("mask invalidated attachment indices")
		}
		for i, a := range masked.Growth {
			if a.Dead == span.Contains(grid.Growth[i]) || grid.Growth[i].Dead {
				t.Fatal("mask changed source liveness or drew another span")
			}
		}
	}
}
