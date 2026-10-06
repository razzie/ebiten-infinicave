package infinicave

import "math"

// Cached images and shader samples use pixels. Generation, collision, cameras,
// and CPU spatial fields use scene units independently of this raster resolution.
const rasterPixelsPerUnit = 1000

func rasterAlignedY(y float64) float64 {
	return math.Round(y*rasterPixelsPerUnit) / rasterPixelsPerUnit
}
