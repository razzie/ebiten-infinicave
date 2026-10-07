package render

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

const maxBatchVertices = 16384

// Merge consecutive ordinary triangle lists, preserving their primitive order
// and alpha mode. Nonzero/even-odd fills must retain separate coverage masks.
func batchTriangleMeshes(meshes []TriangleMesh) []TriangleMesh {
	var result []TriangleMesh
	for _, mesh := range meshes {
		if len(mesh.Indices) == 0 {
			continue
		}
		if mesh.fillRule != ebiten.FillRuleFillAll || len(mesh.Vertices) > maxBatchVertices {
			result = append(result, mesh)
			continue
		}
		if len(result) == 0 || result[len(result)-1].fillRule != mesh.fillRule ||
			result[len(result)-1].premultiplied != mesh.premultiplied ||
			len(result[len(result)-1].Vertices)+len(mesh.Vertices) > maxBatchVertices {
			result = append(result, TriangleMesh{fillRule: mesh.fillRule, premultiplied: mesh.premultiplied})
		}
		batch := &result[len(result)-1]
		offset := uint32(len(batch.Vertices))
		batch.Vertices = append(batch.Vertices, mesh.Vertices...)
		for _, index := range mesh.Indices {
			batch.Indices = append(batch.Indices, index+offset)
		}
	}
	return result
}

// Bounds live in the original padded-window pixels. Integer origins preserve
// raster alignment, and padding leaves room for antialiasing at the edges.
func triangleMeshesBounds(meshes []TriangleMesh) image.Rectangle {
	loX, loY, hiX, hiY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, mesh := range meshes {
		for _, index := range mesh.Indices {
			v := mesh.Vertices[index]
			loX, loY = min(loX, float64(v.DstX)), min(loY, float64(v.DstY))
			hiX, hiY = max(hiX, float64(v.DstX)), max(hiY, float64(v.DstY))
		}
	}
	if math.IsInf(loX, 1) {
		return image.Rectangle{}
	}
	const padding = 3
	return image.Rect(int(math.Floor(loX))-padding, int(math.Floor(loY))-padding,
		int(math.Ceil(hiX))+padding, int(math.Ceil(hiY))+padding).
		Intersect(image.Rect(0, 0, RasterPixelsPerUnit, terrain.GenerationHeight*RasterPixelsPerUnit))
}

func translateTriangleMeshes(meshes []TriangleMesh, origin image.Point) {
	for _, mesh := range meshes {
		for i := range mesh.Vertices {
			mesh.Vertices[i].DstX -= float32(origin.X)
			mesh.Vertices[i].DstY -= float32(origin.Y)
		}
	}
}

// Called only on freshly prepared worker-owned meshes. Once cropped, the
// meshes and their bounds move together to the game thread.
func FinishSectionMesh(mesh *SectionMesh) {
	mesh.VinesBounds = triangleMeshesBounds(mesh.Vines)
	mesh.ForegroundVinesBounds = triangleMeshesBounds(mesh.ForegroundVines)
	mesh.MushroomsBounds = triangleMeshesBounds([]TriangleMesh{mesh.Mushrooms})
	mesh.Vines = batchTriangleMeshes(mesh.Vines)
	mesh.ForegroundVines = batchTriangleMeshes(mesh.ForegroundVines)
	translateTriangleMeshes(mesh.Vines, mesh.VinesBounds.Min)
	translateTriangleMeshes(mesh.ForegroundVines, mesh.ForegroundVinesBounds.Min)
	translateTriangleMeshes([]TriangleMesh{mesh.Mushrooms}, mesh.MushroomsBounds.Min)
}
