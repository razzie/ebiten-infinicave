package terrain

func surfaceVineLongEnough(v Vine) bool {
	length, span := v.extent()
	return length >= vineMinSurfaceTrunkLength && span >= vineMinSurfaceTrunkSpan
}

// Smoothing can shorten a marginal trunk. Drop its whole family and remap
// surviving attachments before adding guide runners, free twigs, or leaves.
func removeShortSurfaceVines(vines []Vine) []Vine {
	remap := make([]int, len(vines))
	var result []Vine
	for i, v := range vines {
		remap[i] = -1
		if v.Parent < 0 {
			if v.Foreground && !surfaceVineLongEnough(v) {
				continue
			}
		} else {
			if remap[v.Parent] < 0 {
				continue
			}
			v.Parent = remap[v.Parent]
		}
		remap[i] = len(result)
		result = append(result, v)
	}
	return result
}
