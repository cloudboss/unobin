package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestPlanPlanFileV2FromStateUsesExactCurrentSnapshot(t *testing.T) {
	request := validPlanFileV2StateRequest(t)
	request.StateMoves = []PlannedEntryMove{{
		From: "resource.old",
		To:   "resource.current",
	}}
	backend := &planFileV2StateBackend{
		stack:    "production",
		revision: "state-1",
	}
	loaded := applyPlanFileV2Snapshot(t)
	var evaluatedSnapshots []*state.SnapshotV2
	request.Evaluate = func(
		snapshot *state.SnapshotV2,
		pass *planningPassState,
	) ([]planStepV2Request, error) {
		evaluatedSnapshots = append(evaluatedSnapshots, snapshot)
		require.NoError(t, snapshot.Validate())
		require.Nil(t, snapshot.Find("resource.old"))
		require.NotNil(t, snapshot.Find("resource.current"))
		if len(evaluatedSnapshots) == 1 {
			snapshot.Entries[0].Address = "resource.changed"
		}
		require.NoError(t, pass.invalidateOutputs("resource.trigger"))
		return []planStepV2Request{}, nil
	}

	plan, err := planPlanFileV2FromState(
		context.Background(),
		backend,
		request,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(revision string) (*state.SnapshotV2, error) {
				backend.events = append(backend.events, "load:"+revision)
				return loaded, nil
			},
		},
	)
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	require.Equal(t, request.Factory.Name, plan.Factory.Name)
	require.Equal(t, request.Factory.Version, plan.Factory.Version)
	require.Equal(t, request.Factory.ContentRevision, plan.Factory.ContentRevision)
	require.Equal(t, backend.stack, plan.Stack)
	require.Equal(t, backend.revision, plan.StateRevision)
	require.Equal(t, request.StateMoves, plan.StateMoves)
	require.NotNil(t, plan.Steps)
	require.Empty(t, plan.Steps)
	require.Len(t, evaluatedSnapshots, 2)
	require.NotSame(t, loaded, evaluatedSnapshots[0])
	require.NotSame(t, evaluatedSnapshots[0], evaluatedSnapshots[1])
	require.NotNil(t, loaded.Find("resource.old"))
	require.Nil(t, loaded.Find("resource.current"))
	require.Equal(t, []string{
		"current-revision",
		"stack",
		"load:state-1",
	}, backend.events)
}

func TestPlanPlanFileV2FromStateInitializesNewSnapshot(t *testing.T) {
	request := validPlanFileV2StateRequest(t)
	backend := &planFileV2StateBackend{
		stack:      "preview",
		currentErr: state.ErrNoCurrent,
	}
	loaded := false
	request.Evaluate = func(
		snapshot *state.SnapshotV2,
		_ *planningPassState,
	) ([]planStepV2Request, error) {
		require.NoError(t, snapshot.Validate())
		require.Equal(t, request.Factory, snapshot.Factory)
		require.Equal(t, backend.stack, snapshot.Stack)
		require.NotNil(t, snapshot.Entries)
		require.Empty(t, snapshot.Entries)
		return []planStepV2Request{}, nil
	}

	plan, err := planPlanFileV2FromState(
		context.Background(),
		backend,
		request,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				loaded = true
				return nil, errors.New("unexpected snapshot load")
			},
		},
	)
	require.NoError(t, err)
	require.False(t, loaded)
	require.Equal(t, "", plan.StateRevision)
	require.Equal(t, backend.stack, plan.Stack)
	require.Equal(t, []string{"current-revision", "stack"}, backend.events)
}

func TestPlanPlanFileV2FromStateRejectsInvalidSetupBeforeStateAccess(t *testing.T) {
	request := validPlanFileV2StateRequest(t)
	validSnapshots := planFileV2PlanningSnapshotCallbacks{
		Load: func(string) (*state.SnapshotV2, error) {
			return nil, errors.New("unexpected snapshot load")
		},
	}

	var missingContext context.Context
	backend := &planFileV2StateBackend{stack: "production", revision: "state-1"}
	plan, err := planPlanFileV2FromState(
		missingContext,
		backend,
		request,
		validSnapshots,
	)
	require.ErrorContains(t, err, "planning context is required")
	require.Equal(t, PlanFileV2{}, plan)
	require.Empty(t, backend.events)

	plan, err = planPlanFileV2FromState(
		context.Background(),
		nil,
		request,
		validSnapshots,
	)
	require.ErrorContains(t, err, "state store is required")
	require.Equal(t, PlanFileV2{}, plan)

	backend = &planFileV2StateBackend{stack: "production", revision: "state-1"}
	plan, err = planPlanFileV2FromState(
		context.Background(),
		backend,
		request,
		planFileV2PlanningSnapshotCallbacks{},
	)
	require.ErrorContains(t, err, "version 2 snapshot loader is required")
	require.Equal(t, PlanFileV2{}, plan)
	require.Empty(t, backend.events)

	backend = &planFileV2StateBackend{stack: "production", revision: "state-1"}
	request.Evaluate = nil
	plan, err = planPlanFileV2FromState(
		context.Background(),
		backend,
		request,
		validSnapshots,
	)
	require.ErrorContains(t, err, "plan step evaluator is required")
	require.Equal(t, PlanFileV2{}, plan)
	require.Empty(t, backend.events)

	backend = &planFileV2StateBackend{stack: "production", revision: "state-1"}
	request = validPlanFileV2StateRequest(t)
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err = planPlanFileV2FromState(
		canceledContext,
		backend,
		request,
		validSnapshots,
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, PlanFileV2{}, plan)
	require.Empty(t, backend.events)
}

func TestPlanPlanFileV2FromStateStopsAfterCurrentRevisionFailure(t *testing.T) {
	request := validPlanFileV2StateRequest(t)
	expectedErr := errors.New("current snapshot unavailable")
	backend := &planFileV2StateBackend{
		stack:      "production",
		currentErr: expectedErr,
	}
	loaded := false
	evaluated := false
	request.Evaluate = func(
		*state.SnapshotV2,
		*planningPassState,
	) ([]planStepV2Request, error) {
		evaluated = true
		return nil, nil
	}

	plan, err := planPlanFileV2FromState(
		context.Background(),
		backend,
		request,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				loaded = true
				return nil, nil
			},
		},
	)
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, PlanFileV2{}, plan)
	require.False(t, loaded)
	require.False(t, evaluated)
	require.Equal(t, []string{"current-revision"}, backend.events)
}

func TestPlanPlanFileV2FromStateRejectsMetadataBeforeSnapshotLoad(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*planFileV2StateRequest)
		message string
	}{
		{
			name: "factory",
			change: func(request *planFileV2StateRequest) {
				request.Factory.ContentRevision = ""
			},
			message: "factory content revision is required",
		},
		{
			name: "state moves",
			change: func(request *planFileV2StateRequest) {
				request.StateMoves = nil
			},
			message: "state moves are required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validPlanFileV2StateRequest(t)
			test.change(&request)
			backend := &planFileV2StateBackend{
				stack:    "production",
				revision: "state-1",
			}
			loaded := false
			evaluated := false
			request.Evaluate = func(
				*state.SnapshotV2,
				*planningPassState,
			) ([]planStepV2Request, error) {
				evaluated = true
				return nil, nil
			}

			plan, err := planPlanFileV2FromState(
				context.Background(),
				backend,
				request,
				planFileV2PlanningSnapshotCallbacks{
					Load: func(string) (*state.SnapshotV2, error) {
						loaded = true
						return nil, nil
					},
				},
			)
			require.ErrorContains(t, err, test.message)
			require.Equal(t, PlanFileV2{}, plan)
			require.False(t, loaded)
			require.False(t, evaluated)
			require.Equal(t, []string{
				"current-revision",
				"stack",
			}, backend.events)
		})
	}
}

func TestPlanPlanFileV2FromStateRejectsInvalidMoveBeforeEvaluation(t *testing.T) {
	request := validPlanFileV2StateRequest(t)
	request.StateMoves = []PlannedEntryMove{{
		From: "resource.old",
		To:   "data-source.current",
	}}
	backend := &planFileV2StateBackend{
		stack:    "production",
		revision: "state-1",
	}
	loaded := applyPlanFileV2Snapshot(t)
	evaluated := false
	request.Evaluate = func(
		*state.SnapshotV2,
		*planningPassState,
	) ([]planStepV2Request, error) {
		evaluated = true
		return nil, nil
	}

	plan, err := planPlanFileV2FromState(
		context.Background(),
		backend,
		request,
		planFileV2PlanningSnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) { return loaded, nil },
		},
	)
	require.ErrorContains(t, err, "address category data-source does not match resource")
	require.Equal(t, PlanFileV2{}, plan)
	require.False(t, evaluated)
	require.NotNil(t, loaded.Find("resource.old"))
	require.Nil(t, loaded.Find("data-source.current"))
}

func validPlanFileV2StateRequest(t *testing.T) planFileV2StateRequest {
	t.Helper()
	return planFileV2StateRequest{
		Factory: state.FactoryInfo{
			Name:            "deploy",
			Version:         "v1.0.0",
			ContentRevision: "revision-1",
		},
		GeneratedAt: time.Date(2026, 8, 22, 7, 0, 0, 0, time.UTC),
		Inputs: operationObject(t, map[string]EncodedValue{
			"region": StringValue("east"),
		}),
		Parallelism: 4,
		Mode:        PlanApply,
		StateMoves:  []PlannedEntryMove{},
		Evaluate: func(
			*state.SnapshotV2,
			*planningPassState,
		) ([]planStepV2Request, error) {
			return []planStepV2Request{}, nil
		},
	}
}

type planFileV2StateBackend struct {
	state.Backend
	stack      string
	revision   string
	currentErr error
	events     []string
}

func (b *planFileV2StateBackend) Stack() string {
	b.events = append(b.events, "stack")
	return b.stack
}

func (b *planFileV2StateBackend) CurrentRev() (string, error) {
	b.events = append(b.events, "current-revision")
	return b.revision, b.currentErr
}

func (b *planFileV2StateBackend) Lock(context.Context) (state.Lock, error) {
	b.events = append(b.events, "lock")
	return nil, errors.New("planning must not acquire the state lock")
}
