package infinicave

import (
	"sync/atomic"

	"github.com/razzie/ebiten-infinicave/internal/render"
)

// Request identities belong to the coordinator, not immutable render recipes.
// The epoch separates worlds (including resets); the incarnation separates
// successive requests for the same section, including eviction and reload.
type sectionRequest struct {
	epoch, incarnation uint64
}

type sectionResult struct {
	render.SectionMesh
	request sectionRequest
}

type sectionCancellation struct {
	id      int64
	request sectionRequest
}

var worldEpoch atomic.Uint64

func (w *world) nextSectionRequest() sectionRequest {
	if w.epoch == 0 {
		w.epoch = worldEpoch.Add(1)
	}
	w.incarnation++
	return sectionRequest{epoch: w.epoch, incarnation: w.incarnation}
}

func (w *world) rememberRequest(id int64, request sectionRequest) {
	if w.requests == nil {
		w.requests = make(map[int64]sectionRequest)
	}
	w.requests[id] = request
}

func (w *world) matchesFlight(id int64, request sectionRequest) bool {
	if w.ctx != nil && w.ctx.Err() != nil {
		return false
	}
	if w.inflight != nil {
		flight, active := w.inflight[id]
		return active && flight.request == request
	}
	// Worker-free fixtures can submit unversioned recipes directly. A real
	// request always assigns an epoch and cannot take this path.
	if w.epoch == 0 && request == (sectionRequest{}) {
		return true
	}
	return w.working && w.activeID == id && w.activeRequest == request
}

func (w *world) matchesCollision(id int64, request sectionRequest) bool {
	if w.ctx != nil && w.ctx.Err() != nil {
		return false
	}
	if w.epoch == 0 && request == (sectionRequest{}) {
		return true
	}
	current, exists := w.requests[id]
	return exists && current == request
}

// Both the game goroutine at Close and the dispatcher after joining producers
// can drain channels safely. The latter releases late canceled replies without
// making Close block on CPU work or an application loader.
func (w *world) drainCPUResults() {
	for {
		select {
		case <-w.terrainMeshes:
		default:
			goto results
		}
	}
results:
	for {
		select {
		case <-w.results:
		default:
			goto terrain
		}
	}
terrain:
	for {
		select {
		case <-w.terrain:
		default:
			goto jobs
		}
	}
jobs:
	for {
		select {
		case <-w.jobs:
		default:
			goto canceled
		}
	}
canceled:
	for {
		select {
		case <-w.canceled:
		default:
			return
		}
	}
}
