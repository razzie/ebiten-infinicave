package terrain

import "fmt"

// BranchingStrategy selects how rock spurs grow from the solid side of guides.
type BranchingStrategy uint8

const (
	BranchingRecursive BranchingStrategy = iota
	BranchingSimple
	BranchingLoops
)

func (s BranchingStrategy) String() string {
	switch s {
	case BranchingSimple:
		return "simple"
	case BranchingRecursive:
		return "recursive"
	case BranchingLoops:
		return "loops"
	default:
		return fmt.Sprintf("BranchingStrategy(%d)", s)
	}
}

func ParseBranchingStrategy(name string) (BranchingStrategy, error) {
	switch name {
	case "simple":
		return BranchingSimple, nil
	case "recursive":
		return BranchingRecursive, nil
	case "loops":
		return BranchingLoops, nil
	default:
		return BranchingRecursive, fmt.Errorf("unknown branching strategy %q: use simple, recursive, or loops", name)
	}
}

// ShapeOptions affects solid geometry, including collision and vegetation.
type ShapeOptions struct {
	BranchingStrategy  BranchingStrategy
	BranchMinThickness float64
	BranchMaxThickness float64
	SpikeDensity       float64
	EdgeRoughness      float64
}

func DefaultShapeOptions() ShapeOptions {
	return ShapeOptions{BranchingStrategy: BranchingRecursive, BranchMinThickness: .048, BranchMaxThickness: .08, SpikeDensity: .65, EdgeRoughness: .75}
}

// BranchThicknessBounds resolves zero endpoints to the defaults. Thickness is
// twice the nominal profile width; the soft shoulder and tip taper extend or
// narrow the actual contour.
func (s ShapeOptions) BranchThicknessBounds() (float64, float64) {
	lo, hi := s.BranchMinThickness, s.BranchMaxThickness
	if lo == 0 {
		lo = DefaultShapeOptions().BranchMinThickness
	}
	if hi == 0 {
		hi = DefaultShapeOptions().BranchMaxThickness
	}
	return lo, hi
}

func (s ShapeOptions) branchWidth(sample float64) float64 {
	lo, hi := s.BranchThicknessBounds()
	return (lo + (hi-lo)*sample) * .5
}
