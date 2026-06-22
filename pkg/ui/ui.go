// Package ui serves a live browser view of an apply run: the plan's
// step graph drawn as cards that change color as steps start, finish,
// or fail. The server binds an ephemeral loopback port, serves one
// embedded page under a random token path so other local users cannot
// guess the URL, and streams step state changes over server-sent
// events. It is an observer only: it consumes the same ApplyEvent
// values the terminal renderer does and never blocks the scheduler.
package ui

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path"
	"sync"
	"time"

	"github.com/cloudboss/unobin/pkg/runtime"
)

//go:embed assets
var assetsFS embed.FS

// subscriberCap is how many frames a client may fall behind before it
// is evicted. An evicted client's EventSource reconnects and starts
// over from a fresh snapshot, so eviction loses nothing.
const subscriberCap = 256

// Config names the run a Server displays.
type Config struct {
	Factory string
	Stack   string
	Graph   []runtime.StepNode

	// now is the clock used for elapsed times; tests inject a fixed
	// one. Nil means time.Now.
	now func() time.Time
}

// stepState is the server's record of one step, updated from observed
// events and replayed to late or reconnecting clients as a snapshot.
type stepState struct {
	stage    string
	decision runtime.Decision
	started  time.Time
	elapsed  time.Duration
	err      string
}

// frame is one marshaled SSE payload. complete marks the
// run-complete frame so the events handler knows when a client has
// seen the end of the run.
type frame struct {
	data     []byte
	complete bool
}

// Server is the live run view: an HTTP server plus the step state
// table it streams from. Create one with Start, feed it with Observe,
// end the stream with Complete, and shut it down with Close.
type Server struct {
	cfg    Config
	now    func() time.Time
	ln     net.Listener
	hsrv   *http.Server
	token  string
	began  time.Time
	subCap int

	mu     sync.Mutex
	steps  map[string]*stepState
	seq    uint64
	subs   map[chan frame]bool
	result *runCompleteFrame

	served     chan struct{}
	servedOnce sync.Once
}

// Start listens on an ephemeral loopback port and begins serving the
// run view for cfg.
func Start(cfg Config) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("run view: %w", err)
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("run view: %w", err)
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	s := &Server{
		cfg:    cfg,
		now:    now,
		ln:     ln,
		token:  hex.EncodeToString(tok),
		began:  now(),
		subCap: subscriberCap,
		steps:  make(map[string]*stepState, len(cfg.Graph)),
		subs:   map[chan frame]bool{},
		served: make(chan struct{}),
	}
	for _, n := range cfg.Graph {
		s.steps[n.Address] = &stepState{decision: n.Decision}
	}
	s.hsrv = &http.Server{Handler: s.routes()}
	go func() { _ = s.hsrv.Serve(ln) }()
	return s, nil
}

// URL returns the address of the page, including the token path.
func (s *Server) URL() string {
	return "http://" + s.ln.Addr().String() + "/" + s.token + "/"
}

// Observe records one apply event and forwards it to every connected
// client. It never blocks on a slow client.
func (s *Server) Observe(ev runtime.ApplyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.steps[ev.Address]
	if st == nil {
		st = &stepState{}
		s.steps[ev.Address] = st
	}
	st.decision = ev.Decision
	var errText string
	switch ev.Stage {
	case runtime.StageStart:
		st.stage = "start"
		st.started = ev.Time
	case runtime.StageDone:
		st.stage = "done"
		st.elapsed = ev.Elapsed
	case runtime.StageFail:
		st.stage = "fail"
		st.elapsed = ev.Elapsed
		if ev.Err != nil {
			errText = ev.Err.Error()
		}
		st.err = errText
	}
	s.seq++
	s.broadcastLocked(frame{data: marshalFrame(applyEventFrame{
		Kind:      "apply-event",
		Seq:       s.seq,
		Address:   ev.Address,
		Decision:  string(ev.Decision),
		Stage:     string(ev.Stage),
		ElapsedMS: ev.Elapsed.Milliseconds(),
		Err:       errText,
	})})
}

// Complete ends the stream: it computes the run totals from the
// observed events and broadcasts the run-complete frame. message
// explains a failure that has no failed step, such as an interrupt;
// it is ignored when ok is true.
func (s *Server) Complete(ok bool, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result != nil {
		return
	}
	if ok {
		message = ""
	}
	var done, failed int
	for _, st := range s.steps {
		switch st.stage {
		case "done":
			done++
		case "fail":
			failed++
		}
	}
	s.seq++
	s.result = &runCompleteFrame{
		Kind:      "run-complete",
		Seq:       s.seq,
		OK:        ok,
		Message:   message,
		Succeeded: done,
		Failed:    failed,
		NotRun:    len(s.steps) - done - failed,
		ElapsedMS: s.now().Sub(s.began).Milliseconds(),
	}
	s.broadcastLocked(frame{data: marshalFrame(*s.result), complete: true})
}

// WaitServed blocks until some client has received the run-complete
// frame, or d has passed, and reports which happened. It returns
// false immediately when Complete has not been called.
func (s *Server) WaitServed(d time.Duration) bool {
	s.mu.Lock()
	completed := s.result != nil
	s.mu.Unlock()
	if !completed {
		return false
	}
	select {
	case <-s.served:
		return true
	case <-time.After(d):
		return false
	}
}

// Close shuts the server down, closing any open event streams.
func (s *Server) Close() {
	_ = s.hsrv.Close()
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	prefix := "/" + s.token
	mux.HandleFunc("GET "+prefix+"/{$}", s.handleIndex)
	mux.HandleFunc("GET "+prefix+"/assets/{name}", s.handleAsset)
	mux.HandleFunc("GET "+prefix+"/events", s.handleEvents)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	data, err := assetsFS.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var contentType string
	switch path.Ext(name) {
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "text/javascript; charset=utf-8"
	case ".html":
		contentType = "text/html; charset=utf-8"
	case ".svg":
		contentType = "image/svg+xml"
	default:
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := s.subscribe()
	defer s.unsubscribe(ch)
	rc := http.NewResponseController(w)
	for {
		select {
		case fr, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", fr.data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			if fr.complete {
				s.markServed()
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// subscribe registers a new client channel preloaded with the graph
// frame, the current snapshot, and the run-complete frame when the
// run has already ended.
func (s *Server) subscribe() chan frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan frame, s.subCap)
	ch <- frame{data: marshalFrame(graphFrame{
		Kind:    "graph",
		Seq:     s.seq,
		Factory: s.cfg.Factory,
		Stack:   s.cfg.Stack,
		Steps:   s.cfg.Graph,
	})}
	ch <- frame{data: marshalFrame(s.snapshotLocked())}
	if s.result != nil {
		ch <- frame{data: marshalFrame(*s.result), complete: true}
	}
	s.subs[ch] = true
	return ch
}

func (s *Server) unsubscribe(ch chan frame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, ch)
}

// broadcastLocked queues fr for every subscriber. A subscriber whose
// buffer is full is closed and removed; its client reconnects and
// catches up from a snapshot instead of stalling the run.
func (s *Server) broadcastLocked(fr frame) {
	for ch := range s.subs {
		select {
		case ch <- fr:
		default:
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func (s *Server) snapshotLocked() snapshotFrame {
	entries := make(map[string]stepStateEntry, len(s.steps))
	for addr, st := range s.steps {
		if st.stage == "" {
			continue
		}
		entry := stepStateEntry{
			Stage:    st.stage,
			Decision: string(st.decision),
			Err:      st.err,
		}
		if st.stage == "start" {
			entry.ElapsedMS = s.now().Sub(st.started).Milliseconds()
		} else {
			entry.ElapsedMS = st.elapsed.Milliseconds()
		}
		entries[addr] = entry
	}
	return snapshotFrame{Kind: "snapshot", Seq: s.seq, Steps: entries}
}

func (s *Server) markServed() {
	s.servedOnce.Do(func() { close(s.served) })
}

func marshalFrame(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"kind":"error"}`)
	}
	return b
}
