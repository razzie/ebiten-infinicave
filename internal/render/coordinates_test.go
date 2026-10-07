package render

import (
	"image"
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestAmbientHorizontalFade(t *testing.T) {
	for _, tc := range []struct{ x, alpha float64 }{
		{-1, 0}, {-.5, 0}, {-.25, .5}, {0, 1}, {.5, 1}, {1, 1}, {1.25, .5}, {1.5, 0}, {2, 0},
	} {
		if got := horizontalFade(tc.x); math.Abs(got-tc.alpha) > 1e-12 {
			t.Fatalf("fade at %v: %v, want %v", tc.x, got, tc.alpha)
		}
	}
}

func TestNativeMeshScalingRetainsMaterialCoordinatesAndCropOrigin(t *testing.T) {
	base := resolutionTestMesh(0)
	base.Vines = []TriangleMesh{{Vertices: []ebiten.Vertex{{DstX: 2.25, DstY: 3.5, SrcX: .5, SrcY: .5}}, Indices: []uint32{0, 0, 0}}}
	base.VinesBounds = image.Rect(101, 1003, 110, 1020)
	saved := append([]ebiten.Vertex(nil), base.Background.Faces.Vertices...)
	for _, pixels := range []int{500, 501, 1000, 1920} {
		scaled := ScaleSectionMesh(base, pixels)
		for i, vertex := range base.Background.Faces.Vertices {
			native := scaled.Background.Faces.Vertices[i]
			if math.Abs(float64(native.DstX)-(float64(vertex.DstX)*float64(pixels)/1000-float64(BackgroundRasterBounds(pixels).Min.X))) > .001 {
				t.Fatal("background scaling lost its horizontal raster origin")
			}
			if math.Abs(float64(native.DstY)-(float64(vertex.DstY)*float64(pixels)/1000-float64(pixels))) > .001 || native.SrcX != vertex.SrcX || native.SrcY != vertex.SrcY {
				t.Fatal("terrain scaling lost owned-band alignment or material coordinates")
			}
		}
		p := scaled.Vines[0].Vertices[0]
		wantX := (2.25 + 101) * float64(pixels) / 1000
		wantY := (3.5 + 1003) * float64(pixels) / 1000
		if math.Abs(float64(p.DstX)+float64(scaled.VinesBounds.Min.X)-wantX) > .001 || math.Abs(float64(p.DstY)+float64(scaled.VinesBounds.Min.Y)-wantY) > .001 {
			t.Fatal("native vegetation crop moved its world origin")
		}
	}
	if !reflect.DeepEqual(base.Background.Faces.Vertices, saved) || base.Vines[0].Vertices[0].DstX != 2.25 {
		t.Fatal("native scaling mutated cached reference meshes")
	}
}
