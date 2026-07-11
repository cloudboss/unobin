package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type applyLockGolden struct {
	Cases []applyLockCaseGolden `json:"cases"`
}

type applyLockCaseGolden struct {
	Name               string `json:"name"`
	ResultNonNull      bool   `json:"result-non-null"`
	WrittenRevision    bool   `json:"written-revision"`
	Error              string `json:"error"`
	FailureStage       string `json:"failure-stage"`
	UnlockError        bool   `json:"unlock-error"`
	UnderlyingUnlocked bool   `json:"underlying-unlocked"`
}

func TestApplyLockFailureGolden(t *testing.T) {
	result := applyLockGolden{Cases: []applyLockCaseGolden{
		applyUnlockFailure(t),
		applyCurrentRevisionFailure(t),
	}}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/apply-lock.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func applyUnlockFailure(t *testing.T) applyLockCaseGolden {
	t.Helper()
	store := newStateStore(t)
	wrapped := &unlockFailureBackend{Backend: store}
	executor, plan := emptyApplyExecutor(t, wrapped)
	result, err := executor.ApplyPlan(context.Background(), plan)
	var unlockError *StateUnlockError
	failure, _ := AsApplyFailure(err)
	return applyLockCaseGolden{
		Name: "unlock after state write", ResultNonNull: result != nil,
		WrittenRevision: result != nil && result.WrittenRev != "",
		Error:           runtimeErrorString(err), FailureStage: string(failure.Stage),
		UnlockError:        errors.As(err, &unlockError),
		UnderlyingUnlocked: wrapped.unlocked,
	}
}

func applyCurrentRevisionFailure(t *testing.T) applyLockCaseGolden {
	t.Helper()
	store := &currentFailureBackend{Backend: newStateStore(t)}
	executor, plan := emptyApplyExecutor(t, store)
	result, err := executor.ApplyPlan(context.Background(), plan)
	var unlockError *StateUnlockError
	failure, _ := AsApplyFailure(err)
	return applyLockCaseGolden{
		Name: "current revision failure", ResultNonNull: result != nil,
		WrittenRevision: result != nil && result.WrittenRev != "",
		Error:           runtimeErrorString(err), FailureStage: string(failure.Stage),
		UnlockError:        errors.As(err, &unlockError),
		UnderlyingUnlocked: store.unlocked,
	}
}

func emptyApplyExecutor(t *testing.T, store state.Backend) (*Executor, *PlanFile) {
	t.Helper()
	factory := state.FactoryInfo{Name: "test-stack", Version: "v0", ContentRevision: "c0"}
	dag, source := syntaxDAGAndBody(t, refreshFixture(t, "empty"), map[string]*Library{})
	executor := &Executor{
		DAG: dag, SyntaxSource: source, Libraries: map[string]*Library{},
		Store: store, Factory: factory,
	}
	plan := &PlanFile{
		FormatVersion: PlanFormatVersion,
		Factory: FactoryRef{
			Name: factory.Name, Version: factory.Version, ContentRevision: factory.ContentRevision,
		},
		Stack: "prod", Steps: []PlanStep{},
	}
	return executor, plan
}

type currentFailureBackend struct {
	state.Backend
	unlocked bool
}

func (b *currentFailureBackend) CurrentRev() (string, error) {
	return "", errors.New("current failed")
}

func (b *currentFailureBackend) Lock(ctx context.Context) (state.Lock, error) {
	lock, err := b.Backend.Lock(ctx)
	if err != nil {
		return nil, err
	}
	return &trackingStateLock{Lock: lock, unlocked: &b.unlocked}, nil
}

type trackingStateLock struct {
	state.Lock
	unlocked *bool
}

func (l *trackingStateLock) Unlock() error {
	if err := l.Lock.Unlock(); err != nil {
		return err
	}
	*l.unlocked = true
	return nil
}
