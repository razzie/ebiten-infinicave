package infinicave

import (
	"context"
	"errors"

	"github.com/razzie/ebiten-infinicave/internal/parallel"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

const (
	sectionGeometry = iota
	sectionForeground
	sectionBackground
	sectionVegetation
)
const sectionComplete = -1
const sectionQueueLimit = 8

var errSectionYield = errors.New("yield vegetation to foreground")

type phaseResult struct {
	worker  int
	job     sectionJob
	next    *sectionJob
	yielded bool
}
type sectionWorker struct {
	builder *terrain.Builder
	busy    bool
	id      int64
	phase   int
	cancel  context.CancelCauseFunc
	join    func()
}

// Finish the viewport before speculative work. While moving, the next band's
// foreground can precede visible details, but never visible foreground.
func sectionPriority(job sectionJob, viewport Viewport) int {
	if !render.ValidViewport(viewport) {
		return job.phase
	}
	low, high := visibleSections(viewport.Y, viewport.Height)
	visible := job.id >= low && job.id <= high
	if visible && job.phase <= sectionForeground {
		return job.phase
	}
	approaching := viewport.Velocity < 0 && job.id == high+1 || viewport.Velocity > 0 && job.id == low-1
	if approaching && job.phase <= sectionForeground {
		return 2 + job.phase
	}
	if visible {
		return 2 + job.phase
	}
	// Adjacent windows can supply plants to the viewport. Finish them before
	// farther lookahead sections, even if their terrain band is offscreen.
	if job.id == low-1 || job.id == high+1 {
		return 6 + job.phase
	}
	return 10 + job.phase
}

// Builders have one owner. Completed phases rejoin the bounded queue so queued
// foreground work can run before another section's background or vegetation.
func (w *world) runSectionPhases(newBuilder func() *terrain.Builder, advance func(*terrain.Builder, sectionJob, context.Context) *sectionJob) {
	workers := make([]sectionWorker, max(1, w.maxJobs))
	completed := make(chan phaseResult, len(workers))
	queue := make([]sectionJob, 0, sectionQueueLimit)
	viewport := Viewport{}
	defer func() {
		for i := range workers {
			if workers[i].busy {
				workers[i].cancel(context.Canceled)
			}
		}
		for i := range workers {
			if workers[i].busy {
				<-completed
			}
			if workers[i].join != nil {
				workers[i].join()
			}
		}
	}()
	rank := func(job sectionJob) int { return sectionPriority(job, viewport) }
	yieldVegetation := func() {
		for i := range workers {
			worker := &workers[i]
			if !worker.busy || worker.phase != sectionVegetation {
				continue
			}
			for _, job := range queue {
				if job.ctx.Err() == nil && job.phase <= sectionForeground && rank(job) < rank(sectionJob{id: worker.id, phase: worker.phase}) {
					worker.cancel(errSectionYield)
					break
				}
			}
		}
	}
	for {
		// Drain arriving requests before dispatching a lower priority phase.
		for {
			select {
			case job := <-w.jobs:
				queue = append(queue, job)
			case viewport = <-w.priorities:
			default:
				goto dispatch
			}
		}
	dispatch:
		for i := range workers {
			worker := &workers[i]
			if worker.busy || len(queue) == 0 {
				continue
			}
			best := 0
			for j := 1; j < len(queue); j++ {
				if rank(queue[j]) < rank(queue[best]) {
					best = j
				}
			}
			job := queue[best]
			if worker.builder == nil {
				worker.builder = newBuilder()
			}
			phaseCtx, cancel := context.WithCancelCause(job.ctx)
			run := func() {
				next := advance(worker.builder, job, phaseCtx)
				yielded := context.Cause(phaseCtx) == errSectionYield && job.ctx.Err() == nil && !(next != nil && next.phase == sectionComplete)
				completed <- phaseResult{worker: i, job: job, next: next, yielded: yielded}
			}
			if i > 0 {
				join, started := parallel.TryStart(run)
				if !started {
					cancel(context.Canceled)
					continue
				}
				worker.join = join
			} else {
				go run()
			}
			worker.busy, worker.id, worker.phase, worker.cancel = true, job.id, job.phase, cancel
			queue = append(queue[:best], queue[best+1:]...)
		}
		// Use spare capacity first; restarting vegetation is necessary only
		// when higher-priority foreground is still waiting for a worker.
		yieldVegetation()
		var available <-chan struct{}
		if len(queue) > 0 {
			available = parallel.Available()
		}
		select {
		case <-w.done:
			return
		case viewport = <-w.priorities:
		case <-available:
		case job := <-w.jobs:
			queue = append(queue, job)
		case result := <-completed:
			worker := &workers[result.worker]
			worker.cancel(context.Canceled)
			if worker.join != nil {
				worker.join()
				worker.join = nil
			}
			worker.busy = false
			if result.yielded {
				queue = append(queue, result.job)
			} else if result.next != nil && result.next.phase >= 0 {
				queue = append(queue, *result.next)
			}
		}
	}
}
