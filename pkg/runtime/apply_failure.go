package runtime

import "errors"

type ApplyFailureStage string

const (
	ApplyFailureSetup    ApplyFailureStage = "setup"
	ApplyFailureExecute  ApplyFailureStage = "execute"
	ApplyFailureFinalize ApplyFailureStage = "finalize"
)

type ApplyFailure struct {
	Stage ApplyFailureStage
	Cause error
}

func NewApplyFailure(stage ApplyFailureStage, cause error) *ApplyFailure {
	if cause == nil {
		panic("runtime: apply failure cause is required")
	}
	return &ApplyFailure{Stage: stage, Cause: cause}
}

func (e *ApplyFailure) Error() string {
	return e.Cause.Error()
}

func (e *ApplyFailure) Unwrap() error {
	return e.Cause
}

func AsApplyFailure(err error) (*ApplyFailure, bool) {
	var failure *ApplyFailure
	if !errors.As(err, &failure) {
		return nil, false
	}
	return failure, true
}
