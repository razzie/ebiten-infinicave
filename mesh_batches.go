package infinicave

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

const maxBatchVertices = 16384

// Merge consecutive ordinary triangle lists, preserving their primitive order
// and alpha mode. Nonzero/even-odd fills must retain separate coverage masks.
func batchTriangleMeshes(meshes []triangleMesh) []triangleMesh {
	var result []triangleMesh
	for _, mesh := range meshes {
		if len(mesh.indices) == 0 {
			continue
		}
		if mesh.fillRule != ebiten.FillRuleFillAll || len(mesh.vertices) > maxBatchVertices {
			result = append(result, mesh)
			continue
		}
		if len(result) == 0 || result[len(result)-1].fillRule != mesh.fillRule ||
			result[len(result)-1].premultiplied != mesh.premultiplied ||
			len(result[len(result)-1].vertices)+len(mesh.vertices) > maxBatchVertices {
			result = append(result, triangleMesh{fillRule: mesh.fillRule, premultiplied: mesh.premultiplied})
		}
		batch := &result[len(result)-1]
		offset := uint32(len(batch.vertices))
		batch.vertices = append(batch.vertices, mesh.vertices...)
		for _, index := range mesh.indices {
			batch.indices = append(batch.indices, index+offset)
		}
	}
	return result
}

// Bounds live in the original padded-window pixels. Integer origins preserve
// raster alignment, and padding leaves room for antialiasing at the edges.
func triangleMeshesBounds(meshes []triangleMesh) image.Rectangle {
	loX, loY, hiX, hiY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, mesh := range meshes {
		for _, index := range mesh.indices {
			v := mesh.vertices[index]
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
		Intersect(image.Rect(0, 0, rasterPixelsPerUnit, generationHeight*rasterPixelsPerUnit))
}

func translateTriangleMeshes(meshes []triangleMesh, origin image.Point) {
	for _, mesh := range meshes {
		for i := range mesh.vertices {
			mesh.vertices[i].DstX -= float32(origin.X)
			mesh.vertices[i].DstY -= float32(origin.Y)
		}
	}
}

// Called only on freshly prepared worker-owned meshes. Once cropped, the
// meshes and their bounds move together to the game thread.
func finishSectionMesh(mesh *sectionMesh) {
	mesh.vinesBounds = triangleMeshesBounds(mesh.vines)
	mesh.foregroundVinesBounds = triangleMeshesBounds(mesh.foregroundVines)
	mesh.mushroomsBounds = triangleMeshesBounds([]triangleMesh{mesh.mushrooms})
	mesh.vines = batchTriangleMeshes(mesh.vines)
	mesh.foregroundVines = batchTriangleMeshes(mesh.foregroundVines)
	translateTriangleMeshes(mesh.vines, mesh.vinesBounds.Min)
	translateTriangleMeshes(mesh.foregroundVines, mesh.foregroundVinesBounds.Min)
	translateTriangleMeshes([]triangleMesh{mesh.mushrooms}, mesh.mushroomsBounds.Min)
}
