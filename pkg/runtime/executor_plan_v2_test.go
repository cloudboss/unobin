package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
)

func TestExecutorPlanV2UsesCurrentStateAndSourceMoves(t *testing.T) {
	for _, destroy := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "destroy"}[destroy], func(t *testing.T) {
			executor, snapshot := newStateMoveV2Executor(t)
			source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "root-move")
			_, body := syntaxDAGAndBody(t, source, executor.Libraries)
			executor.SyntaxSource.StateMoves = body.StateMoves
			backend := &planFileV2StateBackend{
				stack: snapshot.Stack, revision: "state-1",
				load: func(string) (*state.SnapshotV2, error) { return snapshot, nil },
			}
			executor.Store, executor.Factory = backend, snapshot.Factory
			executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
			executor.Destroy = destroy
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			require.NoError(t, plan.Validate())
			require.Equal(t, "state-1", plan.StateRevision)
			require.Equal(t, snapshot.Stack, plan.Stack)
			require.Equal(t, snapshot.Factory.Name, plan.Factory.Name)
			require.Equal(t, DefaultParallelism, plan.Parallelism)
			require.Equal(t, []PlannedEntryMove{
				{From: "resource.old", To: "resource.main"},
			}, plan.StateMoves)
			require.Equal(t, operationObject(t, map[string]EncodedValue{
				"name": StringValue("server"), "size": IntegerValue(1),
			}), plan.Inputs)
			var main *PlanStepV2
			for i := range plan.Steps {
				if plan.Steps[i].Address == "resource.main" {
					main = &plan.Steps[i]
				}
			}
			require.NotNil(t, main)
			if destroy {
				require.Equal(t, PlanDestroy, plan.Mode)
				require.Equal(t, DecisionDestroy, main.Operation.Resource.Decision)
				require.Len(t, plan.Steps, 1)
			} else {
				require.Equal(t, PlanApply, plan.Mode)
				require.Equal(t, DecisionNoOp, main.Operation.Resource.Decision)
			}
			require.Equal(t, []string{"current-revision", "stack", "load:state-1"}, backend.events)
			require.NotNil(t, snapshot.Find("resource.old"))
			require.Nil(t, snapshot.Find("resource.main"))
		})
	}
}

func TestExecutorPlanV2UsesLocalSnapshotStore(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	store := newStateStore(t)
	snapshot.Stack = store.Stack()
	revision, err := store.WriteV2(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	executor.Store, executor.Factory = store, snapshot.Factory
	executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	require.Equal(t, revision, plan.StateRevision)
	after, err := store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, revision, after)
	stored, err := store.GetV2(revision)
	require.NoError(t, err)
	require.Equal(t, snapshot.Entries, stored.Entries)
}

func TestExecutorPlanV2InitializesNewState(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	backend := &planFileV2StateBackend{stack: snapshot.Stack, currentErr: state.ErrNoCurrent}
	executor.Store, executor.Factory = backend, snapshot.Factory
	executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	require.Empty(t, plan.StateRevision)
	require.Equal(t, []string{"current-revision", "stack"}, backend.events)
	for _, step := range plan.Steps {
		if step.Operation.Kind == StepResource {
			require.Equal(t, DecisionCreate, step.Operation.Resource.Decision)
		}
	}
}

func TestExecutorPlanV2RejectsObsoleteStoredState(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	store, revision := newObsoleteStateStore(t)
	executor.Store, executor.Factory = store, snapshot.Factory
	executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
	plan, err := executor.PlanV2(context.Background())
	require.ErrorContains(t, err, "obsolete alpha format")
	require.Nil(t, plan)
	after, err := store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, revision, after)
}

func TestExecutorPlanV2RejectsInvalidSetupBeforeLoadingState(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Executor)
		want   string
	}{
		{"missing graph", func(e *Executor) { e.DAG = nil }, "dependency graph is required"},
		{"missing catalog", func(e *Executor) { e.LibraryCatalog = nil }, "catalog is required"},
		{"missing store", func(e *Executor) { e.Store = nil }, "state store is required"},
		{"unsupported store", func(e *Executor) { e.Store = &planFileV2OldBackend{} },
			"does not support version 2 snapshots"},
		{"invalid input", func(e *Executor) { e.Inputs["name"] = make(chan bool) }, "input \"name\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, snapshot := newStateMoveV2Executor(t)
			backend := &planFileV2StateBackend{stack: snapshot.Stack, revision: "state-1"}
			executor.Store, executor.Factory = backend, snapshot.Factory
			executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
			test.change(executor)
			plan, err := executor.PlanV2(context.Background())
			require.ErrorContains(t, err, test.want)
			require.Nil(t, plan)
			require.Empty(t, backend.events)
		})
	}
}

func TestExecutorPlanV2RejectsCanceledContextBeforeStateAccess(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	backend := &planFileV2StateBackend{stack: snapshot.Stack, revision: "state-1"}
	executor.Store, executor.Factory = backend, snapshot.Factory
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err := executor.PlanV2(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, plan)
	require.Empty(t, backend.events)
}

func newObsoleteStateStore(t *testing.T) (*local.Store, string) {
	t.Helper()
	store := newStateStore(t)
	body, err := os.ReadFile("testdata/snapshot-v1.json")
	require.NoError(t, err)
	sealed, err := state.Seal(body, state.PayloadTypeState, encrypters.Noop{})
	require.NoError(t, err)
	revision := "2026-04-30T12:00:00Z"
	path := filepath.Join(
		store.Root, store.Factory, store.Stack(), "snapshots", revision+".json.enc",
	)
	require.NoError(t, os.WriteFile(path, sealed, 0o600))
	require.NoError(t, store.SetCurrent(revision))
	return store, revision
}
