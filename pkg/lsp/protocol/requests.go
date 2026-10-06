package protocol

import (
	"context"
	"encoding/json"
	"sync"
)

type requestQueue struct {
	ctx      context.Context
	mu       sync.Mutex
	ready    *sync.Cond
	finished bool
	stopped  bool
	pending  []*pendingRequest
	active   map[ID]map[*pendingRequest]struct{}
}

type pendingRequest struct {
	ctx    context.Context
	cancel context.CancelFunc
	id     *ID
	body   []byte
}

func newRequestQueue(ctx context.Context) *requestQueue {
	q := &requestQueue{ctx: ctx, active: make(map[ID]map[*pendingRequest]struct{})}
	q.ready = sync.NewCond(&q.mu)
	return q
}

func (q *requestQueue) push(body []byte, id *ID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.finished || q.stopped {
		return
	}
	ctx, cancel := context.WithCancel(q.ctx)
	work := &pendingRequest{ctx: ctx, cancel: cancel, id: id, body: body}
	if id != nil {
		if q.active[*id] == nil {
			q.active[*id] = make(map[*pendingRequest]struct{})
		}
		q.active[*id][work] = struct{}{}
	}
	q.pending = append(q.pending, work)
	q.ready.Signal()
}

func (q *requestQueue) cancelRequest(params json.RawMessage) error {
	var cancel struct {
		ID *ID `json:"id"`
	}
	if err := json.Unmarshal(params, &cancel); err != nil {
		return err
	}
	if cancel.ID == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for work := range q.active[*cancel.ID] {
		work.cancel()
	}
	return nil
}

func (q *requestQueue) next() *pendingRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.pending) == 0 && !q.finished && !q.stopped {
		q.ready.Wait()
	}
	if q.stopped || len(q.pending) == 0 {
		return nil
	}
	work := q.pending[0]
	q.pending[0] = nil
	q.pending = q.pending[1:]
	return work
}

func (q *requestQueue) complete(work *pendingRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if work.id != nil {
		delete(q.active[*work.id], work)
		if len(q.active[*work.id]) == 0 {
			delete(q.active, *work.id)
		}
	}
	work.cancel()
}

func (q *requestQueue) finish() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.finished = true
	q.ready.Broadcast()
}

func (q *requestQueue) stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stopped = true
	for _, requests := range q.active {
		for work := range requests {
			work.cancel()
		}
	}
	for _, work := range q.pending {
		work.cancel()
	}
	clear(q.active)
	q.pending = nil
	q.ready.Broadcast()
}
