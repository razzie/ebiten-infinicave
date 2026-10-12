package infinicave

import "github.com/razzie/ebiten-infinicave/internal/terrain"

// BranchingStrategy controls rock branches on the solid side of each guide.
type BranchingStrategy = terrain.BranchingStrategy

const (
	// BranchingSimple uses short tapering spurs with one generation of forks.
	BranchingSimple = terrain.BranchingSimple
	// BranchingRecursive grows longer limbs with several generations of forks.
	// It is the default, including for a zero Config.
	BranchingRecursive = terrain.BranchingRecursive
	// BranchingLoops grows irregular reconnecting routes and secondary loops.
	BranchingLoops = terrain.BranchingLoops
)

// ParseBranchingStrategy accepts simple, recursive, or loops.
func ParseBranchingStrategy(name string) (BranchingStrategy, error) {
	return terrain.ParseBranchingStrategy(name)
}
