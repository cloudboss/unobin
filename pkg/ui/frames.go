package ui

import "github.com/cloudboss/unobin/pkg/runtime"

// graphFrame is the first frame every client receives: the full step
// graph of the run, in plan order. Steps and their dependency edges
// never change after this; later frames only change step state.
type graphFrame struct {
	Kind    string             `json:"kind"`
	Seq     uint64             `json:"seq"`
	Factory string             `json:"factory"`
	Stack   string             `json:"stack,omitempty"`
	Steps   []runtime.StepNode `json:"steps"`
}

// stepStateEntry is one step's current state inside a snapshot frame.
// Steps still pending are omitted from the snapshot; the graph frame
// already names every step.
type stepStateEntry struct {
	Stage     string `json:"stage"`
	Decision  string `json:"decision"`
	ElapsedMS int64  `json:"elapsed-ms"`
	Err       string `json:"err,omitempty"`
}

// snapshotFrame follows the graph frame on connect so a client that
// joins mid-run (or reconnects) starts from the current state instead
// of replaying history.
type snapshotFrame struct {
	Kind  string                    `json:"kind"`
	Seq   uint64                    `json:"seq"`
	Steps map[string]stepStateEntry `json:"steps"`
}

// applyEventFrame is the live delta: one step changed state. Its
// field meanings match the apply-event envelope `--output json`
// emits, with elapsed in milliseconds rather than formatted text.
type applyEventFrame struct {
	Kind      string `json:"kind"`
	Seq       uint64 `json:"seq"`
	Address   string `json:"address"`
	Decision  string `json:"decision"`
	Stage     string `json:"stage"`
	ElapsedMS int64  `json:"elapsed-ms,omitempty"`
	Err       string `json:"err,omitempty"`
}

// runCompleteFrame ends the stream. NotRun counts steps that never
// started, whether the scheduler halted on a failure or the run was
// interrupted. Message explains a failure that has no failed step,
// such as an interrupt.
type runCompleteFrame struct {
	Kind      string `json:"kind"`
	Seq       uint64 `json:"seq"`
	OK        bool   `json:"ok"`
	Message   string `json:"message,omitempty"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	NotRun    int    `json:"not-run"`
	ElapsedMS int64  `json:"elapsed-ms"`
}
