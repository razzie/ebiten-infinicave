package render

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Prepared meshes and material coordinates use this reference scale. Cached
// images are rasterized separately at the destination's native resolution.
const RasterPixelsPerUnit = 1000

func RenderAlignedY(y float64, pixels int) float64 {
	return math.Round(y*float64(pixels)) / float64(pixels)
}

func scaleRasterBounds(bounds image.Rectangle, pixels int) image.Rectangle {
	if bounds.Empty() {
		return image.Rectangle{}
	}
	scale := float64(pixels) / RasterPixelsPerUnit
	return image.Rect(int(math.Floor(float64(bounds.Min.X)*scale)), int(math.Floor(float64(bounds.Min.Y)*scale)),
		int(math.Ceil(float64(bounds.Max.X)*scale)), int(math.Ceil(float64(bounds.Max.Y)*scale)))
}

// Copy destination coordinates only. Material samples and CPU geometry retain
// their reference scale, so resizing cannot change generation or mineral grain.
func scaleTriangleMesh(mesh TriangleMesh, scale float64, offset image.Point) TriangleMesh {
	if scale == 1 && offset == (image.Point{}) {
		return mesh
	}
	mesh.Vertices = append([]ebiten.Vertex(nil), mesh.Vertices...)
	mesh.Index = nil
	transformVertices(mesh.Vertices, scale, offset)
	if mesh.MaterialFrame.Pixels != 0 {
		mesh.MaterialFrame.Pixels *= scale
		mesh.MaterialFrame.OriginX = mesh.MaterialFrame.OriginX*scale - float64(offset.X)/mesh.MaterialFrame.Samples
		mesh.MaterialFrame.OriginY = mesh.MaterialFrame.OriginY*scale - float64(offset.Y)/mesh.MaterialFrame.Samples
	}
	return mesh
}

// Render the owned band directly; padded CPU geometry is clipped by the image.
func ScaleGridMesh(mesh GridMesh, pixels, offsetX int) GridMesh {
	mesh.Growth = ScaleAttachments(mesh.Growth, pixels, image.Pt(offsetX, -pixels))
	scale := float64(pixels) / RasterPixelsPerUnit
	mesh.Faces = scaleTriangleMesh(mesh.Faces, scale, image.Pt(offsetX, -pixels))
	mesh.Grass = scaleTriangleMesh(mesh.Grass, scale, image.Pt(offsetX, -pixels))
	mesh.Outlines = append([]TriangleMesh(nil), mesh.Outlines...)
	for i, outline := range mesh.Outlines {
		mesh.Outlines[i] = scaleTriangleMesh(outline, scale, image.Pt(offsetX, -pixels))
	}
	return mesh
}

func scaleVegetationMeshes(meshes []TriangleMesh, bounds image.Rectangle, pixels int) ([]TriangleMesh, image.Rectangle) {
	native := scaleRasterBounds(bounds, pixels)
	scale := float64(pixels) / RasterPixelsPerUnit
	meshes = append([]TriangleMesh(nil), meshes...)
	for i, mesh := range meshes {
		mesh = scaleTriangleMesh(mesh, scale, image.Point{})
		// Cropped meshes start at their reference bounds. Preserve the exact
		// world origin even when its scaled position lies between pixels.
		if scale != 1 {
			for j := range mesh.Vertices {
				mesh.Vertices[j].DstX += float32(float64(bounds.Min.X)*scale - float64(native.Min.X))
				mesh.Vertices[j].DstY += float32(float64(bounds.Min.Y)*scale - float64(native.Min.Y))
			}
		}
		meshes[i] = mesh
	}
	return meshes, native
}

func ScaleSectionMesh(mesh SectionMesh, pixels int) SectionMesh {
	mesh.Background = ScaleGridMesh(mesh.Background, pixels, -BackgroundRasterBounds(pixels).Min.X)
	mesh.Foreground = ScaleGridMesh(mesh.Foreground, pixels, 0)
	return ScaleVegetationMesh(mesh, pixels)
}

func ScaleVegetationMesh(mesh SectionMesh, pixels int) SectionMesh {
	mask := mesh.LayerMask()
	if mask&MushroomsLayer != 0 {
		mesh.Plants = scaleCroppedAttachments(mesh.Plants, mesh.MushroomsBounds, pixels)
		mushrooms, bounds := scaleVegetationMeshes([]TriangleMesh{mesh.Mushrooms}, mesh.MushroomsBounds, pixels)
		mesh.Mushrooms, mesh.MushroomsBounds = mushrooms[0], bounds
	}
	if mask&WebsLayer != 0 {
		mesh.Webs = scaleCroppedAttachments(mesh.Webs, mesh.SpiderWebsBounds, pixels)
		webs, bounds := scaleVegetationMeshes([]TriangleMesh{mesh.SpiderWebs}, mesh.SpiderWebsBounds, pixels)
		mesh.SpiderWebs, mesh.SpiderWebsBounds = webs[0], bounds
	}
	if mask&BackgroundVinesLayer != 0 {
		mesh.Vines, mesh.VinesBounds = scaleVegetationMeshes(mesh.Vines, mesh.VinesBounds, pixels)
	}
	if mask&ForegroundVinesLayer != 0 {
		mesh.ForegroundVines, mesh.ForegroundVinesBounds = scaleVegetationMeshes(mesh.ForegroundVines, mesh.ForegroundVinesBounds, pixels)
	}

	return mesh
}

// The cave occupies a centered square in landscape targets. Portrait targets
// retain their full width. Effects pass this transform through padded buffers.
type RenderTransform struct {
	Pixels  int
	OffsetX float64
}

func TargetTransform(dst *ebiten.Image) RenderTransform {
	size := dst.Bounds().Size()
	pixels := min(size.X, size.Y)
	return RenderTransform{Pixels: pixels, OffsetX: float64(size.X-pixels) / 2}
}

func horizontalFade(x float64) float64 {
	return geom.Smoothstep(terrain.BackgroundMinX, 0, x) * (1 - geom.Smoothstep(terrain.Width, terrain.BackgroundMaxX, x))
}

func transformVerticesScalar(vertices []ebiten.Vertex, scale float64, offset image.Point) {
	for i := range vertices {
		v := &vertices[i]
		v.DstX = float32(float64(v.DstX)*scale) + float32(offset.X)
		v.DstY = float32(float64(v.DstY)*scale) + float32(offset.Y)
	}
}

// MaterialFrame reconstructs reference artwork coordinates from the actual
// destination sample center. Interpolated UVs can round to opposite sides of a
// grain-cell boundary when a target is cropped or supersampled.
type MaterialFrame struct {
	OriginX, OriginY float64
	Pixels, Samples  float64
}

func ReferenceMaterialFrame() MaterialFrame {
	return MaterialFrame{Pixels: RasterPixelsPerUnit, Samples: 1}
}

// Keep cached render targets out of the atlas: reallocation must not change
// their raster origin and introduce subpixel differences on revisiting.
func NewSectionImageAt(height, pixels int) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, pixels, height*pixels), &ebiten.NewImageOptions{Unmanaged: true})
}

func DrawVegetation(dst, layer *ebiten.Image, bounds image.Rectangle, top, y float64, pixels int, view RenderTransform) {
	if layer == nil {
		return
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(bounds.Min.X), (top-y)*float64(pixels)+float64(bounds.Min.Y))
	scale := float64(view.Pixels) / float64(pixels)
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(view.OffsetX, 0)
	dst.DrawImage(layer, op)
}

func NewVegetationImage(bounds image.Rectangle) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), &ebiten.NewImageOptions{Unmanaged: true})
}
