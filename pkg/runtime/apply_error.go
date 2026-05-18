package runtime

import (
	"fmt"
	"time"
)

// ApplyError is the structured failure value runApplySchedule returns
// when a step's CRUD or action call reports an error. The original
// error is available via Unwrap so callers can use errors.Is and
// errors.As; the runner uses the structured fields to print a multi
// line report that names the failing address, its decision and module,
// the elapsed time, and the counts of steps that were skipped or
// completed alongside it.
type ApplyError struct {
	Address        string
	Kind           NodeKind
	Decision       Decision
	Module         string
	Elapsed        time.Duration
	Err            error
	SkippedCount   int
	SucceededCount int
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("%s: %v", e.Address, e.Err)
}

func (e *ApplyError) Unwrap() error {
	return e.Err
}
