package runner

import (
	"fmt"
	"io"
	"time"

	"github.com/cloudboss/unobin/pkg/runtime"
)

// consumeApplyEvents reads events until the channel closes and writes
// one line per event to out. Events that represent no observable
// work, such as a composite boundary's output evaluation, a no-op
// resource, or a skipped action, are filtered so the stream shows
// only steps the operator cares about.
func consumeApplyEvents(events <-chan runtime.ApplyEvent, out io.Writer) {
	for ev := range events {
		if isSilentEvent(ev) {
			continue
		}
		writeApplyEventHuman(out, ev)
	}
}

func isSilentEvent(ev runtime.ApplyEvent) bool {
	switch ev.Kind {
	case runtime.NodeOutput, runtime.NodeComposite:
		return true
	}
	switch ev.Decision {
	case runtime.DecisionNoOp, runtime.DecisionSkip:
		return true
	}
	return false
}

func writeApplyEventHuman(out io.Writer, ev runtime.ApplyEvent) {
	ts := ev.Time.Format("15:04:05")
	switch ev.Stage {
	case runtime.StageStart:
		fmt.Fprintf(out, "[%s] %s %s\n", ts, decisionGerund(ev.Decision), ev.Address)
	case runtime.StageDone:
		fmt.Fprintf(out, "[%s] %s %s (%s)\n",
			ts, decisionPast(ev.Decision), ev.Address, formatDuration(ev.Elapsed))
	case runtime.StageFail:
		fmt.Fprintf(out, "[%s] %s failed for %s (%s): %v\n",
			ts, decisionGerund(ev.Decision), ev.Address,
			formatDuration(ev.Elapsed), ev.Err)
	}
}

// decisionGerund returns the present-participle verb for a decision,
// suitable for a "starting" line: creating, updating, replacing,
// destroying, running (for actions), reading (for data sources).
func decisionGerund(d runtime.Decision) string {
	switch d {
	case runtime.DecisionCreate:
		return "creating"
	case runtime.DecisionUpdate:
		return "updating"
	case runtime.DecisionReplace:
		return "replacing"
	case runtime.DecisionDestroy:
		return "destroying"
	case runtime.DecisionRerun:
		return "running"
	case runtime.DecisionRead:
		return "reading"
	}
	return string(d)
}

// decisionPast returns the past-tense verb for a decision.
func decisionPast(d runtime.Decision) string {
	switch d {
	case runtime.DecisionCreate:
		return "created"
	case runtime.DecisionUpdate:
		return "updated"
	case runtime.DecisionReplace:
		return "replaced"
	case runtime.DecisionDestroy:
		return "destroyed"
	case runtime.DecisionRerun:
		return "ran"
	case runtime.DecisionRead:
		return "read"
	}
	return string(d)
}

// formatDuration renders a duration in a short, human form: 350ms for
// sub-second values, 1.2s otherwise.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// renderApplyError prints the structured failure report. The
// underlying module error is printed verbatim so operators see the
// exact text the cloud API returned. Skipped and succeeded counts
// give a quick sense of blast radius without listing every address.
func renderApplyError(out io.Writer, ae *runtime.ApplyError) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Apply failed.")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Failed: %s (%s) after %s\n",
		ae.Address, ae.Decision, formatDuration(ae.Elapsed))
	if ae.Module != "" {
		fmt.Fprintf(out, "  Module: %s\n", ae.Module)
	}
	fmt.Fprintf(out, "  Error:  %v\n", ae.Err)
	fmt.Fprintln(out)
	if ae.SkippedCount > 0 {
		fmt.Fprintf(out,
			"Skipped %d transitive dependent(s); they were not run.\n",
			ae.SkippedCount)
	}
	if ae.SucceededCount > 0 {
		fmt.Fprintf(out,
			"%d step(s) completed before the failure; their state is preserved.\n",
			ae.SucceededCount)
	}
}
