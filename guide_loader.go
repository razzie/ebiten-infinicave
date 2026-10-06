package infinicave

import "math"

// GuideLoader loads guides for one square section. IDs start at 0 at the floor
// and decrease upward: -1, -2, ... . Guide.Pts are polylines in section-local
// scene units, with the owned square spanning (0, 0) to (1, 1). The loader owns
// its returned slices; generation copies them and recomputes S, Min, and Max.
// BrightSign defaults to 1; use -1 to reverse the lit side. A zero Seed receives
// a stable seed based on the world seed, section ID, and guide's slice index.
// Guides with nonfinite coordinates or fewer than two distinct consecutive
// points are ignored. Returning nil or an empty slice leaves that section
// without guides; it does not request random guides.
//
// Generation also loads neighboring sections for seamless padding. IDs may be
// requested repeatedly and in any order, so return consistent guides per ID.
// Scene calls the loader on its background generation worker; synchronous
// generation calls it on the calling goroutine. Reset can overlap an old worker,
// so loaders sharing mutable state must be safe for concurrent calls.
type GuideLoader func(id int64) []Guide

// Internal section indices increase upward; only the public loader sees IDs.
func loadedWorldGuides(seed, id int64, load GuideLoader) []Guide {
	var guides []Guide
	top := sectionWindowTop(id)
	// Match the random generator's padding, including shoulders beyond it.
	for owner := id + 2; owner >= max(0, id-2); owner-- {
		for i, source := range load(-owner) {
			g, ok := loadedGuide(source)
			if !ok {
				continue
			}
			if g.Seed == 0 {
				g.Seed = sectionSeed(sectionSeed(seed, owner), int64(i+1)) | 1
			}
			g.translateY(sectionTop(owner) - top)
			guides = append(guides, g)
		}
	}
	return guides
}

func loadedGuide(source Guide) (Guide, bool) {
	g := Guide{BrightSign: source.BrightSign, Seed: source.Seed}
	if g.BrightSign == 0 {
		g.BrightSign = 1
	}
	for _, p := range source.Pts {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return Guide{}, false
		}
		p = p.Mul(generationWidth)
		if len(g.Pts) == 0 || p != g.Pts[len(g.Pts)-1] {
			g.Pts = append(g.Pts, p)
		}
	}
	if len(g.Pts) < 2 {
		return Guide{}, false
	}
	g.S = make([]float64, len(g.Pts))
	g.Min, g.Max = g.Pts[0], g.Pts[0]
	for i, p := range g.Pts {
		if i > 0 {
			g.S[i] = g.S[i-1] + p.Sub(g.Pts[i-1]).Len()
		}
		g.Min.X, g.Min.Y = min(g.Min.X, p.X), min(g.Min.Y, p.Y)
		g.Max.X, g.Max.Y = max(g.Max.X, p.X), max(g.Max.Y, p.Y)
	}
	return g, true
}
