package parallel

import (
	"runtime"
	"sync/atomic"
	"testing"
)

func TestNestedJobsShareBudgetAndVisitEveryIndex(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)
	for _, processors := range []int{1, 2, 4, 8} {
		runtime.GOMAXPROCS(processors)
		var counts [16][23]atomic.Int32
		For(len(counts), func(i int) {
			join := Start(func() {
				For(len(counts[i]), func(j int) {
					if helpers.Load() > int32(max(0, processors-2)) {
						t.Error("nested jobs exceeded helper budget")
					}
					counts[i][j].Add(1)
				})
			})
			join()
		})
		for i := range counts {
			for j := range counts[i] {
				if counts[i][j].Load() != 1 {
					t.Fatal("nested job skipped or repeated an index")
				}
			}
		}
		if helpers.Load() != 0 {
			t.Fatal("completed jobs leaked a worker permit")
		}
	}
}
