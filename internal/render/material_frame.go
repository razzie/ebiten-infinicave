package render

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
