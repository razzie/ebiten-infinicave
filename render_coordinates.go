package infinicave

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Prepared meshes and material coordinates use this reference scale. Cached
// images are rasterized separately at the destination's native resolution.
const rasterPixelsPerUnit = 1000

func renderAlignedY(y float64, pixels int) float64 {
	return math.Round(y*float64(pixels)) / float64(pixels)
}

func (w *world) renderWidth() int {
	if w.pixels > 0 {
		return w.pixels
	}
	return rasterPixelsPerUnit
}

func (s *worldSection) renderWidth() int {
	if s.pixels > 0 {
		return s.pixels
	}
	return rasterPixelsPerUnit
}

func scaleRasterBounds(bounds image.Rectangle, pixels int) image.Rectangle {
	if bounds.Empty() {
		return image.Rectangle{}
	}
	scale := float64(pixels) / rasterPixelsPerUnit
	return image.Rect(int(math.Floor(float64(bounds.Min.X)*scale)), int(math.Floor(float64(bounds.Min.Y)*scale)),
		int(math.Ceil(float64(bounds.Max.X)*scale)), int(math.Ceil(float64(bounds.Max.Y)*scale)))
}

// Copy destination coordinates only. Material samples and CPU geometry retain
// their reference scale, so resizing cannot change generation or mineral grain.
func scaleTriangleMesh(mesh triangleMesh, scale float64, offset image.Point) triangleMesh {
	if scale == 1 && offset == (image.Point{}) {
		return mesh
	}
	mesh.vertices = append([]ebiten.Vertex(nil), mesh.vertices...)
	for i := range mesh.vertices {
		v := &mesh.vertices[i]
		v.DstX = float32(float64(v.DstX)*scale) + float32(offset.X)
		v.DstY = float32(float64(v.DstY)*scale) + float32(offset.Y)
	}
	return mesh
}

// Render the owned band directly; padded CPU geometry is clipped by the image.
func scaleGridMesh(mesh gridMesh, pixels, offsetX int) gridMesh {
	scale := float64(pixels) / rasterPixelsPerUnit
	mesh.faces = scaleTriangleMesh(mesh.faces, scale, image.Pt(offsetX, -pixels))
	mesh.outlines = append([]triangleMesh(nil), mesh.outlines...)
	for i, outline := range mesh.outlines {
		mesh.outlines[i] = scaleTriangleMesh(outline, scale, image.Pt(offsetX, -pixels))
	}
	return mesh
}

func scaleVegetationMeshes(meshes []triangleMesh, bounds image.Rectangle, pixels int) ([]triangleMesh, image.Rectangle) {
	native := scaleRasterBounds(bounds, pixels)
	scale := float64(pixels) / rasterPixelsPerUnit
	meshes = append([]triangleMesh(nil), meshes...)
	for i, mesh := range meshes {
		mesh = scaleTriangleMesh(mesh, scale, image.Point{})
		// Cropped meshes start at their reference bounds. Preserve the exact
		// world origin even when its scaled position lies between pixels.
		if scale != 1 {
			for j := range mesh.vertices {
				mesh.vertices[j].DstX += float32(float64(bounds.Min.X)*scale - float64(native.Min.X))
				mesh.vertices[j].DstY += float32(float64(bounds.Min.Y)*scale - float64(native.Min.Y))
			}
		}
		meshes[i] = mesh
	}
	return meshes, native
}

func scaleSectionMesh(mesh sectionMesh, pixels int) sectionMesh {
	mesh.background = scaleGridMesh(mesh.background, pixels, -backgroundRasterBounds(pixels).Min.X)
	mesh.foreground = scaleGridMesh(mesh.foreground, pixels, 0)
	mesh.vines, mesh.vinesBounds = scaleVegetationMeshes(mesh.vines, mesh.vinesBounds, pixels)
	mesh.foregroundVines, mesh.foregroundVinesBounds = scaleVegetationMeshes(mesh.foregroundVines, mesh.foregroundVinesBounds, pixels)
	mushrooms, bounds := scaleVegetationMeshes([]triangleMesh{mesh.mushrooms}, mesh.mushroomsBounds, pixels)
	mesh.mushrooms, mesh.mushroomsBounds = mushrooms[0], bounds
	return mesh
}

// The cave occupies a centered square in landscape targets. Portrait targets
// retain their full width. Effects pass this transform through padded buffers.
type renderTransform struct {
	pixels  int
	offsetX float64
}

func targetTransform(dst *ebiten.Image) renderTransform {
	size := dst.Bounds().Size()
	pixels := min(size.X, size.Y)
	return renderTransform{pixels: pixels, offsetX: float64(size.X-pixels) / 2}
}

const (
	backgroundMinX = -.5
	backgroundMaxX = 1.5
)

func horizontalFade(x float64) float64 {
	return smoothstep(backgroundMinX, 0, x) * (1 - smoothstep(Width, backgroundMaxX, x))
}
