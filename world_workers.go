package infinicave

import (
	"github.com/razzie/ebiten-infinicave/internal/parallel"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// The root job uses the caller's processor allowance. A second section can
// borrow a helper while all nested generation jobs use the remaining budget.
// Each builder has one owner, even when section results arrive out of order.
func (w *world) runSectionJobs(newBuilder func() *terrain.Builder, build func(*terrain.Builder, sectionJob)) {
	builder := newBuilder()
	var extra *terrain.Builder
	var queued *sectionJob
	var joinExtra func()
	rootBusy := false
	rootDone, extraDone := make(chan struct{}, 1), make(chan struct{}, 1)
	defer func() {
		if rootBusy {
			<-rootDone
		}
		if joinExtra != nil {
			joinExtra()
		}
	}()
	for {
		input := w.jobs
		var available <-chan struct{}
		if queued != nil {
			input = nil
			if w.maxJobs > 1 && joinExtra == nil {
				available = parallel.Available()
			}
		}
		select {
		case <-w.done:
			return
		case <-rootDone:
			rootBusy = false
		case <-extraDone:
			joinExtra()
			joinExtra = nil
		case <-available:
		case job := <-input:
			queued = &job
		}
		if queued == nil {
			continue
		}
		job := *queued
		if !rootBusy {
			rootBusy = true
			go func() { build(builder, job); rootDone <- struct{}{} }()
			queued = nil
		} else if w.maxJobs > 1 && joinExtra == nil {
			if extra == nil {
				extra = newBuilder()
			}
			join, started := parallel.TryStart(func() {
				build(extra, job)
				extraDone <- struct{}{}
			})
			if started {
				joinExtra, queued = join, nil
			}
		}
	}
}
