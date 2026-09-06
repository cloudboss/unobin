package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlanPlanFileV2BuildsFinalizedPlan(t *testing.T) {
	desired := validPlannedActionTarget(t)
	dependencies := []string{"data-source.image"}
	backend := &StateRefV2{
		Name: "local",
		Body: operationObject(t, map[string]EncodedValue{"path": StringValue("state")}),
	}
	moves := []PlannedEntryMove{{From: "resource.old", To: "resource.current"}}
	request := validPlanFileV2PlanningRequest(t)
	request.Backend = backend
	request.StateMoves = moves
	request.Evaluate = func(*planningPassState) ([]planStepV2Request, error) {
		return []planStepV2Request{
			{
				Address:   "action.notify",
				Kind:      NodeAction,
				DependsOn: dependencies,
				Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) {
					return planActionStep(actionPlanningRequest{
						Address:   "action.notify",
						DependsOn: dependencies,
						Desired:   &desired,
					})
				},
			},
		}, nil
	}

	plan, err := planPlanFileV2(context.Background(), request)
	require.NoError(t, err)
	require.NoError(t, plan.Validate())
	require.Equal(t, PlanFormatVersion, plan.FormatVersion)
	require.Equal(t, request.Factory, plan.Factory)
	require.Equal(t, request.Stack, plan.Stack)
	require.Equal(t, request.StateRevision, plan.StateRevision)
	require.Equal(t, request.GeneratedAt, plan.GeneratedAt)
	require.Equal(t, request.Inputs, plan.Inputs)
	require.Equal(t, request.Backend, plan.Backend)
	require.Equal(t, request.Parallelism, plan.Parallelism)
	require.Equal(t, request.Mode, plan.Mode)
	require.Equal(t, request.StateMoves, plan.StateMoves)
	require.Len(t, plan.Steps, 1)
	require.Equal(t, "action.notify", plan.Steps[0].Address)
	require.Equal(t, DecisionRerun, plan.Steps[0].Operation.Action.Decision)
	require.NotEmpty(t, plan.Digest)

	backend.Name = "changed"
	moves[0].To = "resource.changed"
	dependencies[0] = "data-source.changed"
	require.Equal(t, "local", plan.Backend.Name)
	require.Equal(t, "resource.current", plan.StateMoves[0].To)
	require.Equal(t, []string{"data-source.image"}, plan.Steps[0].DependsOn)
}

func TestPlanPlanFileV2RejectsInvalidMetadataBeforeEvaluation(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*planFileV2Request)
		message string
	}{
		{
			name: "factory",
			change: func(request *planFileV2Request) {
				request.Factory.ContentRevision = ""
			},
			message: "factory content revision is required",
		},
		{
			name:    "stack",
			change:  func(request *planFileV2Request) { request.Stack = "" },
			message: "stack is required",
		},
		{
			name: "generated time",
			change: func(request *planFileV2Request) {
				request.GeneratedAt = time.Time{}
			},
			message: "generated time is required",
		},
		{
			name:    "inputs",
			change:  func(request *planFileV2Request) { request.Inputs = StringValue("bad") },
			message: "inputs must be an object",
		},
		{
			name: "backend",
			change: func(request *planFileV2Request) {
				request.Backend = &StateRefV2{
					Body: operationObject(t, map[string]EncodedValue{}),
				}
			},
			message: "state backend name is required",
		},
		{
			name:    "mode",
			change:  func(request *planFileV2Request) { request.Mode = "other" },
			message: "plan mode is invalid",
		},
		{
			name:    "missing moves",
			change:  func(request *planFileV2Request) { request.StateMoves = nil },
			message: "state moves are required",
		},
		{
			name: "invalid move",
			change: func(request *planFileV2Request) {
				request.StateMoves = []PlannedEntryMove{{
					From: "resource.api",
					To:   "resource.api",
				}}
			},
			message: "endpoints must differ",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			request := validPlanFileV2PlanningRequest(t)
			request.Evaluate = func(*planningPassState) ([]planStepV2Request, error) {
				called = true
				return nil, nil
			}
			test.change(&request)

			plan, err := planPlanFileV2(context.Background(), request)
			require.ErrorContains(t, err, test.message)
			require.Equal(t, PlanFileV2{}, plan)
			require.False(t, called)
		})
	}
}

func TestPlanPlanFileV2RejectsInvalidSetupAndPlanningFailure(t *testing.T) {
	request := validPlanFileV2PlanningRequest(t)
	called := false
	request.Evaluate = func(*planningPassState) ([]planStepV2Request, error) {
		called = true
		return nil, nil
	}
	var missingContext context.Context
	plan, err := planPlanFileV2(missingContext, request)
	require.ErrorContains(t, err, "planning context is required")
	require.Equal(t, PlanFileV2{}, plan)
	require.False(t, called)

	request.Evaluate = nil
	plan, err = planPlanFileV2(context.Background(), request)
	require.ErrorContains(t, err, "plan step evaluator is required")
	require.Equal(t, PlanFileV2{}, plan)

	expectedErr := errors.New("evaluation failed")
	request.Evaluate = func(*planningPassState) ([]planStepV2Request, error) {
		return nil, expectedErr
	}
	plan, err = planPlanFileV2(context.Background(), request)
	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, PlanFileV2{}, plan)
}

func validPlanFileV2PlanningRequest(t *testing.T) planFileV2Request {
	t.Helper()
	return planFileV2Request{
		Factory: FactoryRef{
			Name:            "deploy",
			Version:         "v1.0.0",
			ContentRevision: "revision-1",
		},
		Stack:         "production",
		StateRevision: "state-1",
		GeneratedAt:   time.Date(2026, 8, 22, 4, 0, 0, 0, time.UTC),
		Inputs: operationObject(t, map[string]EncodedValue{
			"region": StringValue("east"),
		}),
		Parallelism: 4,
		Mode:        PlanApply,
		StateMoves:  []PlannedEntryMove{},
		Evaluate: func(*planningPassState) ([]planStepV2Request, error) {
			return nil, nil
		},
	}
}
