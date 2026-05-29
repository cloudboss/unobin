package runtime

import "time"

// ApplyStage tags one moment in a step's apply lifecycle.
type ApplyStage string

const (
	// StageStart fires when the scheduler hands the step to a worker.
	StageStart ApplyStage = "start"
	// StageDone fires when the worker reports a successful result.
	StageDone ApplyStage = "done"
	// StageFail fires when the worker returns an error. Apply will
	// halt further dispatch but already-running siblings still emit
	// their own done or fail events.
	StageFail ApplyStage = "fail"
)

// ApplyEvent is one observation the scheduler hands to the optional
// Executor.Events channel during a run. The renderer in the runner
// consumes these to print live per-step progress on stderr or to emit
// one JSON object per event under --json.
type ApplyEvent struct {
	Address string
	Kind    NodeKind

	// Composite marks an event for a composite call site (a boundary).
	// A boundary's Kind is its own resource/data/action kind, so this
	// is what tells a boundary apart from a leaf of that kind.
	Composite bool

	Decision Decision
	Stage    ApplyStage
	Time     time.Time
	Elapsed  time.Duration
	Err      error
}
