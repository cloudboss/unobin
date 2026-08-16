package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func validResourceReadRequest(t *testing.T) resourceReadRequest {
	t.Helper()
	return resourceReadRequest{
		Address:       "resource.bucket",
		Binding:       validOperationBinding("bucket"),
		Inputs:        operationObject(t, map[string]EncodedValue{"name": StringValue("logs")}),
		Configuration: validConfigurationRecord(t),
		PriorOutputs: operationObject(t, map[string]EncodedValue{
			"id": StringValue("bucket-1"),
		}),
	}
}

func TestFixedPointPlanningConvergesWithFreshPassState(t *testing.T) {
	passes := 0
	result, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (string, error) {
			passes++
			if state.outputsInvalidated("resource.bucket") {
				return "converged", nil
			}
			require.NoError(t, state.invalidateOutputs("resource.bucket"))
			return "changed", nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, "converged", result)
	require.Equal(t, 2, passes)
}

func TestFixedPointPlanningAccumulatesReplacementReasons(t *testing.T) {
	passes := 0
	result, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) ([]string, error) {
			passes++
			switch passes {
			case 1:
				require.NoError(t, state.requireReplacement(
					"resource.bucket",
					"input:region",
				))
			case 2:
				require.NoError(t, state.requireReplacement(
					"resource.bucket",
					"drift:etag",
				))
			}
			return state.replacementReasons("resource.bucket"), nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 3, passes)
	require.Equal(t, []string{"drift:etag", "input:region"}, result)
}

func TestFixedPointPlanningReusesIdenticalProviderReads(t *testing.T) {
	request := validResourceReadRequest(t)
	want := presentOperationObservation(t)
	reads := 0
	passes := 0
	result, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (ResourceObservation, error) {
			passes++
			observation, err := state.readResource(
				context.Background(),
				request,
				func(context.Context, resourceReadRequest) (ResourceObservation, error) {
					reads++
					return want, nil
				},
			)
			if err != nil {
				return ResourceObservation{}, err
			}
			if passes == 1 {
				if err := state.invalidateOutputs("resource.bucket"); err != nil {
					return ResourceObservation{}, err
				}
			}
			return observation, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, want, result)
	require.Equal(t, 2, passes)
	require.Equal(t, 1, reads)
}

func TestFixedPointPlanningReturnsIndependentCachedObservations(t *testing.T) {
	request := validResourceReadRequest(t)
	want := presentOperationObservation(t)
	passes := 0
	_, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (struct{}, error) {
			passes++
			observation, err := state.readResource(
				context.Background(),
				request,
				func(context.Context, resourceReadRequest) (ResourceObservation, error) {
					return want, nil
				},
			)
			if err != nil {
				return struct{}{}, err
			}
			if passes == 1 {
				*observation.Identity.StableID = "changed"
				return struct{}{}, state.invalidateOutputs("resource.bucket")
			}
			require.Equal(t, "server-1", *observation.Identity.StableID)
			return struct{}{}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, passes)
}

func TestFixedPointPlanningCombinesConcurrentIdenticalReads(t *testing.T) {
	request := validResourceReadRequest(t)
	want := presentOperationObservation(t)
	var reads atomic.Int32
	_, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (struct{}, error) {
			var wg sync.WaitGroup
			errors := make(chan error, 16)
			for range 16 {
				wg.Go(func() {
					observation, err := state.readResource(
						context.Background(),
						request,
						func(context.Context, resourceReadRequest) (
							ResourceObservation,
							error,
						) {
							reads.Add(1)
							return want, nil
						},
					)
					if err == nil && observation.Status != ObservationPresent {
						err = fmt.Errorf("unexpected observation %q", observation.Status)
					}
					errors <- err
				})
			}
			wg.Wait()
			close(errors)
			for err := range errors {
				if err != nil {
					return struct{}{}, err
				}
			}
			return struct{}{}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, int32(1), reads.Load())
}

func TestFixedPointPlanningDoesNotReuseDifferentReadRequests(t *testing.T) {
	first := validResourceReadRequest(t)
	second := first
	second.Inputs = operationObject(t, map[string]EncodedValue{"name": StringValue("archive")})
	reads := 0
	_, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (struct{}, error) {
			read := func(
				context.Context,
				resourceReadRequest,
			) (ResourceObservation, error) {
				reads++
				return presentOperationObservation(t), nil
			}
			if _, err := state.readResource(context.Background(), first, read); err != nil {
				return struct{}{}, err
			}
			if _, err := state.readResource(context.Background(), second, read); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, reads)
}

func TestResourceReadDigestCoversCompleteRequest(t *testing.T) {
	base := validResourceReadRequest(t)
	baseDigest, err := base.digest()
	require.NoError(t, err)
	tests := []struct {
		name   string
		change func(*resourceReadRequest)
	}{
		{
			name: "address",
			change: func(request *resourceReadRequest) {
				request.Address = "resource.archive"
			},
		},
		{
			name: "binding",
			change: func(request *resourceReadRequest) {
				request.Binding.Export = "archive"
			},
		},
		{
			name: "inputs",
			change: func(request *resourceReadRequest) {
				request.Inputs = operationObject(
					t,
					map[string]EncodedValue{"name": StringValue("archive")},
				)
			},
		},
		{
			name: "configuration",
			change: func(request *resourceReadRequest) {
				request.Configuration = planningConfigurationRecord(
					t,
					configurationLibraryPath,
					"https://other.example",
				)
			},
		},
		{
			name: "prior outputs",
			change: func(request *resourceReadRequest) {
				request.PriorOutputs = operationObject(
					t,
					map[string]EncodedValue{"id": StringValue("bucket-2")},
				)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := base
			tt.change(&request)
			digest, err := request.digest()
			require.NoError(t, err)
			require.NotEqual(t, baseDigest, digest)
		})
	}
}

func TestFixedPointPlanningRecordsNotFoundAsAbsent(t *testing.T) {
	observation, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (ResourceObservation, error) {
			return state.readResource(
				context.Background(),
				validResourceReadRequest(t),
				func(context.Context, resourceReadRequest) (ResourceObservation, error) {
					return ResourceObservation{}, ErrNotFound
				},
			)
		},
	)
	require.NoError(t, err)
	require.Equal(t, ResourceObservation{Status: ObservationAbsent}, observation)
}

func TestPlanningPassRecordsWholeResourceInvalidation(t *testing.T) {
	decisions := []struct {
		decision    Decision
		invalidated bool
	}{
		{decision: DecisionCreate, invalidated: true},
		{decision: DecisionUpdate, invalidated: true},
		{decision: DecisionReplace, invalidated: true},
		{decision: DecisionNoOp, invalidated: false},
		{decision: DecisionDestroy, invalidated: false},
	}
	for _, tt := range decisions {
		t.Run(string(tt.decision), func(t *testing.T) {
			_, err := runFixedPointPlanning(
				context.Background(),
				func(state *planningPassState) (struct{}, error) {
					operation := validResourcePlanOperation(t, tt.decision)
					_, err := state.recordResourceOperation("resource.bucket", &operation)
					if err != nil {
						return struct{}{}, err
					}
					require.Equal(
						t,
						tt.invalidated,
						state.outputsInvalidated("resource.bucket"),
					)
					return struct{}{}, nil
				},
			)
			require.NoError(t, err)
		})
	}
}

func TestPlanningPassPreservesPriorReplacementDecision(t *testing.T) {
	passes := 0
	operation, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (*ResourcePlanOperation, error) {
			passes++
			current := validResourcePlanOperation(t, DecisionUpdate)
			if passes == 1 {
				current = validResourcePlanOperation(t, DecisionReplace)
				current.Reasons = []string{"input:region"}
			}
			return state.recordResourceOperation("resource.bucket", &current)
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, passes)
	require.Equal(t, DecisionReplace, operation.Decision)
	require.Equal(t, []string{"input:region"}, operation.Reasons)
}

func TestFixedPointPlanningStopsAfterMaximumPasses(t *testing.T) {
	passes := 0
	_, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (struct{}, error) {
			passes++
			return struct{}{}, state.requireReplacement(
				"resource.bucket",
				fmt.Sprintf("input:field%d", passes),
			)
		},
	)
	require.ErrorContains(t, err, "planning did not converge after 32 passes")
	require.ErrorContains(t, err, "resource.bucket input:field32")
	require.Equal(t, maxPlanningPasses, passes)
}

func TestFixedPointPlanningReturnsEvaluationError(t *testing.T) {
	want := errors.New("evaluation failed")
	_, err := runFixedPointPlanning(
		context.Background(),
		func(*planningPassState) (struct{}, error) {
			return struct{}{}, want
		},
	)
	require.ErrorIs(t, err, want)
}

func TestPlanningFactsRejectInvalidValues(t *testing.T) {
	_, err := runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (struct{}, error) {
			return struct{}{}, state.requireReplacement("resource.bucket", "bad")
		},
	)
	require.ErrorContains(t, err, "replacement reason is invalid")

	_, err = runFixedPointPlanning(
		context.Background(),
		func(state *planningPassState) (struct{}, error) {
			return struct{}{}, state.invalidateOutputs("action.deploy")
		},
	)
	require.ErrorContains(t, err, "address category action does not match resource")
}
