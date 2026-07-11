package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type applyFailureGolden struct {
	Cases         []applyFailureCaseGolden `json:"cases"`
	NilCausePanic string                   `json:"nil-cause-panic"`
}

type applyFailureCaseGolden struct {
	Name        string `json:"name"`
	Stage       string `json:"stage"`
	Error       string `json:"error"`
	AsFailure   bool   `json:"as-failure"`
	CauseFound  bool   `json:"cause-found"`
	ApplyError  bool   `json:"apply-error"`
	UnlockError bool   `json:"unlock-error"`
	Alias       string `json:"alias"`
	LibraryPath string `json:"library-path"`
}

func TestApplyFailureGolden(t *testing.T) {
	setupCause := errors.New("setup failed")
	stepCause := errors.New("provider failed")
	applyError := &ApplyError{
		Address: "resource.greeting", Decision: DecisionCreate,
		Alias: "local", LibraryPath: "example.com/local", Err: stepCause,
	}
	unlockCause := errors.New("unlock failed")
	unlockFailure := &StateUnlockError{Cause: unlockCause}
	cases := []struct {
		name  string
		cause error
		stage ApplyFailureStage
		find  error
	}{
		{
			name: "wrapped setup", stage: ApplyFailureSetup,
			cause: fmt.Errorf("read plan: %w", setupCause), find: setupCause,
		},
		{name: "step failure", stage: ApplyFailureExecute, cause: applyError, find: stepCause},
		{
			name: "joined finalization", stage: ApplyFailureFinalize,
			cause: errors.Join(errors.New("persist failed"), unlockFailure),
			find:  unlockFailure,
		},
	}
	result := applyFailureGolden{NilCausePanic: applyFailureNilCausePanic()}
	for _, tc := range cases {
		err := fmt.Errorf("outer: %w", NewApplyFailure(tc.stage, tc.cause))
		failure, ok := AsApplyFailure(err)
		var step *ApplyError
		var unlock *StateUnlockError
		entry := applyFailureCaseGolden{
			Name: tc.name, Error: err.Error(), AsFailure: ok,
			CauseFound: errors.Is(err, tc.find), ApplyError: errors.As(err, &step),
			UnlockError: errors.As(err, &unlock),
		}
		if failure != nil {
			entry.Stage = string(failure.Stage)
		}
		if step != nil {
			entry.Alias = step.Alias
			entry.LibraryPath = step.LibraryPath
		}
		result.Cases = append(result.Cases, entry)
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/apply-failure.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func applyFailureNilCausePanic() (message string) {
	defer func() {
		message = fmt.Sprint(recover())
	}()
	_ = NewApplyFailure(ApplyFailureSetup, nil)
	return ""
}
