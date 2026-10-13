package terrain

import (
	"crypto/sha256"
	"encoding/binary"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// GuideKey identifies a polyline across section windows, including authored
// guides that share a seed. It does not retain the points or projection cache.
func GuideKey(points []geom.V, top float64) [32]byte {
	bytes := make([]byte, 16*len(points))
	for i, p := range points {
		for axis, value := range []float64{p.X, p.Y + top} {
			value = math.Round(value*1e7) / 1e7
			if value == 0 {
				value = 0 // canonicalize negative zero
			}
			binary.LittleEndian.PutUint64(bytes[16*i+8*axis:], math.Float64bits(value))
		}
	}
	return sha256.Sum256(bytes)
}

// Support is assigned before any runtime cuts and inherited by every fragment.
// In particular, cutting away a lip must not retire its guide while a detached
// branch or another fragment of its original formation remains.
func (h *Geometry) prepareGuideSupport() {
	if len(h.Guides) == 0 || len(h.Shapes) == 0 {
		return
	}
	keys := make([][32]byte, len(h.Guides))
	for i, g := range h.Guides {
		keys[i] = GuideKey(g.Pts, h.Top)
	}
	h.guideSupport = make([][][32]byte, len(h.Shapes))
	for i, shape := range h.Shapes {
		seen := make(map[int]bool)
		for _, loop := range shape.Loops {
			for _, p := range loop {
				guide, distance := -1, math.Inf(1)
				for j := range h.Guides {
					g := &h.Guides[j]
					if len(g.S) == len(g.Pts) && g.distanceBound2(p) >= distance*distance {
						continue
					}
					_, d := g.NearestPoint(p)
					if d < distance {
						guide, distance = j, d
					}
				}
				if guide >= 0 {
					seen[guide] = true
				}
			}
		}
		// A guide enclosed by a merged formation need not be nearest to
		// any outer vertex. Keep it with that formation as well.
		for j, g := range h.Guides {
			for _, p := range g.Pts {
				if (CollisionGeometry{Polygons: shape.Loops}).Contains(p) {
					seen[j] = true
					break
				}
			}
			if seen[j] {
				h.guideSupport[i] = append(h.guideSupport[i], keys[j])
			}
		}
	}
}

// FaceGuideKeys returns immutable source identities for a structural face.
func (h *Geometry) FaceGuideKeys(face int) [][32]byte {
	if face >= len(h.guideSupport) {
		return nil
	}
	return h.guideSupport[face]
}

// WithoutGuides drops retired point/projection arrays without changing a
// published geometry value or retaining its original guide slice backing array.
func (h *Geometry) WithoutGuides(retired map[[32]byte]bool) *Geometry {
	if h == nil || len(retired) == 0 {
		return h
	}
	guides := liveGuides(h.Guides, h.Top, retired)
	if len(guides) == len(h.Guides) {
		return h
	}
	updated := *h
	updated.Guides = guides
	return &updated
}

func liveGuides(guides []Guide, top float64, retired map[[32]byte]bool) []Guide {
	if len(retired) == 0 {
		return guides
	}
	var live []Guide
	for i, g := range guides {
		if retired[GuideKey(g.Pts, top)] {
			if live == nil {
				live = make([]Guide, i, len(guides))
				copy(live, guides[:i])
			}
		} else if live != nil {
			live = append(live, g)
		}
	}
	if live == nil {
		return guides
	}
	if len(live) == 0 {
		return nil
	}
	return live
}
