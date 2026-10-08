package terrain

import (
	"math"
	"sort"
)

type fragmentBin [2]int
type fragmentIndex map[fragmentBin][]int

func fragmentBins(f guideFragment, visit func(fragmentBin)) {
	const step = .05
	lo := fragmentBin{int(math.Floor((f.lo.X - mergeTolerance) / step)), int(math.Floor((f.lo.Y - mergeTolerance) / step))}
	hi := fragmentBin{int(math.Floor((f.hi.X + mergeTolerance) / step)), int(math.Floor((f.hi.Y + mergeTolerance) / step))}
	for y := lo[1]; y <= hi[1]; y++ {
		for x := lo[0]; x <= hi[0]; x++ {
			visit(fragmentBin{x, y})
		}
	}
}

func newFragmentIndex(faces []guideFragment) fragmentIndex {
	index := make(fragmentIndex)
	for i, f := range faces {
		index.add(i, f)
	}
	return index
}

func (index fragmentIndex) add(i int, f guideFragment) {
	if len(f.poly) < 3 {
		return
	}
	fragmentBins(f, func(bin fragmentBin) { index[bin] = append(index[bin], i) })
}

func (index fragmentIndex) remove(i int, f guideFragment) {
	fragmentBins(f, func(bin fragmentBin) {
		list := index[bin]
		for j, id := range list {
			if id == i {
				index[bin] = append(list[:j], list[j+1:]...)
				break
			}
		}
	})
}

func (index fragmentIndex) candidates(f guideFragment) []int {
	seen := make(map[int]bool)
	var candidates []int
	fragmentBins(f, func(bin fragmentBin) {
		for _, i := range index[bin] {
			if !seen[i] {
				seen[i] = true
				candidates = append(candidates, i)
			}
		}
	})
	// The merge's stable tie-break remains the original face order.
	sort.Ints(candidates)
	return candidates
}
