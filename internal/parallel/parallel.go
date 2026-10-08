// Package parallel shares a goroutine budget across terrain and mesh work.
package parallel

import (
	"runtime"
	"sync"
	"sync/atomic"
)

var helpers atomic.Int32
var released = make(chan struct{}, 1)

// Available signals that an occupied helper has returned its permit. It is a
// hint only: dispatchers must still use TryStart to reserve available capacity.
func Available() <-chan struct{} { return released }

func release() {
	helpers.Add(-1)
	select {
	case released <- struct{}{}:
	default:
	}
}

func acquire() bool {
	// The generation caller uses one processor. Leave capacity for the game
	// loop; nested jobs use this same budget rather than multiplying workers.
	limit := int32(max(0, runtime.GOMAXPROCS(0)-2))
	for {
		n := helpers.Load()
		if n >= limit {
			return false
		}
		if helpers.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// Start runs fn concurrently if capacity is available, otherwise on the caller.
// The returned function joins the task. Nonblocking acquisition lets nested
// work make progress even when every helper is occupied.
func Start(fn func()) func() {
	join, ok := TryStart(fn)
	if !ok {
		fn()
		return func() {}
	}
	return join
}

// TryStart reserves a helper for an optional independent job. On failure it
// leaves the job untouched, so a dispatcher can execute it on its root worker.
func TryStart(fn func()) (join func(), started bool) {
	if !acquire() {
		return nil, false
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer release()
		fn()
	}()
	return func() { <-done }, true
}

// For visits each index exactly once. Each index must own its writes.
func For(n int, fn func(int)) {
	if n <= 0 {
		return
	}
	workers := min(max(1, runtime.GOMAXPROCS(0)-1), n)
	chunk := max(1, n/(workers*8))
	var next atomic.Int64
	run := func() {
		for {
			lo := int(next.Add(int64(chunk))) - chunk
			if lo >= n {
				return
			}
			for i := lo; i < min(lo+chunk, n); i++ {
				fn(i)
			}
		}
	}
	var wg sync.WaitGroup
	for i := 1; i < workers && acquire(); i++ {
		wg.Go(func() {
			defer release()
			run()
		})
	}
	run()
	wg.Wait()
}

// Run executes independent jobs using the same budget as For and Start.
func Run(jobs ...func()) {
	For(len(jobs), func(i int) { jobs[i]() })
}
