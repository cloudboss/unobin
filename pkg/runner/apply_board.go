package runner

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// liveRefresh is how often the live board redraws and the plain-output
// heartbeat checks for steps that have been running a while.
const liveRefresh = time.Second

// firstBeat is how long a step runs before the plain-output renderer first
// reports it as still going. Each later reminder for the same step waits
// twice as long as the last, so a slow step grows quieter over time.
const firstBeat = 10 * time.Second

// applyRenderer turns the apply event stream into terminal output. On a
// terminal it keeps a live region at the bottom listing the steps still
// running with their elapsed time, and prints each finished step as a
// permanent line above it. Without a terminal it prints a start and a done
// line per step plus a periodic heartbeat for any step that runs long.
type applyRenderer struct {
	out        io.Writer
	format     Format
	tty        bool
	now        func() time.Time
	running    map[string]*runningStep
	boardLines int
}

// runningStep is the live state the renderer keeps for one in-flight step.
type runningStep struct {
	decision runtime.Decision
	start    time.Time
	nextBeat time.Duration
}

func newApplyRenderer(out io.Writer, format Format) *applyRenderer {
	return &applyRenderer{
		out:     out,
		format:  format,
		tty:     isTerminal(out),
		now:     time.Now,
		running: map[string]*runningStep{},
	}
}

// run consumes events until the channel closes. Machine formats stay
// event-driven, one envelope per event. The text format drives a one-second
// ticker so the live board and the heartbeat keep updating between events.
func (r *applyRenderer) run(events <-chan runtime.ApplyEvent) {
	if r.format != FormatText {
		for ev := range events {
			if isSilentEvent(ev) {
				continue
			}
			_ = writeEnvelope(r.out, r.format, applyEventFrom(ev))
		}
		return
	}
	ticker := time.NewTicker(liveRefresh)
	defer ticker.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				r.clearBoard()
				return
			}
			r.handleEvent(ev)
		case <-ticker.C:
			r.tick()
		}
	}
}

func (r *applyRenderer) handleEvent(ev runtime.ApplyEvent) {
	if isSilentEvent(ev) {
		return
	}
	if r.tty {
		r.handleTTY(ev)
		return
	}
	r.handlePlain(ev)
}

// handleTTY keeps running steps in the live board and prints each finished
// step as a permanent line above it.
func (r *applyRenderer) handleTTY(ev runtime.ApplyEvent) {
	switch ev.Stage {
	case runtime.StageStart:
		r.startRunning(ev)
		r.refresh()
	case runtime.StageDone, runtime.StageFail:
		r.stopRunning(ev.Address)
		r.clearBoard()
		writeApplyEventHuman(r.out, ev)
		r.drawBoard()
	}
}

// handlePlain prints a line for every start, done, and fail, the output a log
// file or pipe expects.
func (r *applyRenderer) handlePlain(ev runtime.ApplyEvent) {
	switch ev.Stage {
	case runtime.StageStart:
		r.startRunning(ev)
	case runtime.StageDone, runtime.StageFail:
		r.stopRunning(ev.Address)
	}
	writeApplyEventHuman(r.out, ev)
}

func (r *applyRenderer) tick() {
	if r.tty {
		r.refresh()
		return
	}
	r.heartbeat()
}

func (r *applyRenderer) startRunning(ev runtime.ApplyEvent) {
	r.running[ev.Address] = &runningStep{
		decision: ev.Decision,
		start:    ev.Time,
		nextBeat: firstBeat,
	}
}

func (r *applyRenderer) stopRunning(addr string) {
	delete(r.running, addr)
}

// heartbeat prints a reminder for each step that has reached its next
// reminder time, then pushes that step's interval past the current elapsed so
// a long pause does not produce a burst of catch-up lines.
func (r *applyRenderer) heartbeat() {
	now := r.now()
	for _, addr := range sortedKeys(r.running) {
		s := r.running[addr]
		elapsed := now.Sub(s.start)
		if elapsed < s.nextBeat {
			continue
		}
		fmt.Fprintf(r.out, "[%s] still %s %s (%s)\n",
			now.Format("15:04:05"), decisionGerund(s.decision), addr, formatDuration(elapsed))
		for s.nextBeat <= elapsed {
			s.nextBeat *= 2
		}
	}
}

// refresh clears the live board and draws it again from the current running
// set, the redraw a ticker or a new running step triggers.
func (r *applyRenderer) refresh() {
	r.clearBoard()
	r.drawBoard()
}

func (r *applyRenderer) drawBoard() {
	lines := renderBoard(r.boardEntries())
	for _, line := range lines {
		fmt.Fprintln(r.out, line)
	}
	r.boardLines = len(lines)
}

// clearBoard erases the live region by moving the cursor up over it and
// clearing to the end of the screen, leaving the cursor where the board began
// so the caller can print there.
func (r *applyRenderer) clearBoard() {
	if r.boardLines == 0 {
		return
	}
	fmt.Fprintf(r.out, "\x1b[%dA\x1b[J", r.boardLines)
	r.boardLines = 0
}

func (r *applyRenderer) boardEntries() []boardEntry {
	now := r.now()
	entries := make([]boardEntry, 0, len(r.running))
	for addr, s := range r.running {
		entries = append(entries, boardEntry{
			address:  addr,
			decision: s.decision,
			elapsed:  now.Sub(s.start),
		})
	}
	return entries
}

// boardEntry is one line's worth of live-board state: a running step's
// address, action, and how long it has been going.
type boardEntry struct {
	address  string
	decision runtime.Decision
	elapsed  time.Duration
}

// renderBoard returns the live-region lines for the steps still running,
// sorted by address, each showing the action verb, the address, and how long
// it has been running. An empty running set yields no lines.
func renderBoard(running []boardEntry) []string {
	if len(running) == 0 {
		return nil
	}
	slices.SortFunc(running, func(a, b boardEntry) int {
		return cmp.Compare(a.address, b.address)
	})
	lines := make([]string, 0, len(running)+2)
	lines = append(lines, "", fmt.Sprintf("Still running (%d):", len(running)))
	for _, e := range running {
		lines = append(lines, fmt.Sprintf("  %s %s (%s)",
			decisionGerund(e.decision), e.address, formatDuration(e.elapsed)))
	}
	return lines
}

func sortedKeys(m map[string]*runningStep) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// isTerminal reports whether out is a character device. A writer that is not
// an *os.File (a buffer in tests, a pipe, a file) is not a terminal, so the
// renderer falls back to plain lines and the cursor moves stay off.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
