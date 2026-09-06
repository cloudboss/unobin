package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestRefreshV2UsesRecordedBindingsAndPreservesOtherState(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "observed", true: "absent"}[missing], func(t *testing.T) {
			capture := &factoryApplyCapture{}
			executor := newFactoryApplyExecutor(t, capture)
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			applied, err := executor.ApplyPlanV2(context.Background(), plan)
			require.NoError(t, err)
			store := executor.Store.(state.SnapshotBackendV2)
			prior, err := store.GetV2(applied.WrittenRev)
			require.NoError(t, err)
			capture.calls = nil
			capture.read = func(prior *registeredPlanningOutput) *registeredPlanningOutput {
				return &registeredPlanningOutput{ID: prior.ID, Value: "observed"}
			}
			if missing {
				capture.readErr = ErrNotFound
			}
			executor.DAG, executor.Libraries, executor.SyntaxSource = nil, nil, nil
			result, err := executor.RefreshV2(context.Background())
			require.NoError(t, err)
			current, err := store.GetV2(result.WrittenRev)
			require.NoError(t, err)
			require.NotEqual(t, applied.WrittenRev, result.WrittenRev)
			require.Equal(t, prior.Outputs, current.Outputs)
			require.Equal(t, prior.SensitivePaths, current.SensitivePaths)
			require.Len(t, capture.calls, 4)
			for _, entry := range prior.Entries {
				if entry.Kind != state.StateResource {
					require.Equal(t, &entry, current.Find(entry.Address))
					continue
				}
				if missing {
					require.Nil(t, current.Find(entry.Address))
					continue
				}
				target := current.Find(entry.Address).Payload.Resource.Target
				require.Equal(t, entry.Payload.Resource.Target.Identity, target.Identity)
				require.Equal(t, entry.Payload.Resource.Target.Configuration, target.Configuration)
				fields, _ := target.Outputs.ObjectFields()
				require.Equal(t, StringValue("observed"), fields["value"])
			}
			if missing {
				require.Equal(t, 4, result.Dropped)
				require.Zero(t, result.Refreshed)
			} else {
				require.Equal(t, 4, result.Refreshed)
				require.Zero(t, result.Dropped)
			}
		})
	}
}

func TestRefreshV2RejectsIdentityChangesWithoutWritingState(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	capture.read = func(prior *registeredPlanningOutput) *registeredPlanningOutput {
		return &registeredPlanningOutput{ID: "different", Value: prior.Value}
	}
	result, err := executor.RefreshV2(context.Background())
	require.ErrorContains(t, err, "recorded stable ID")
	require.Nil(t, result)
	current, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, applied.WrittenRev, current)
}

func TestRefreshV2ChecksAllBindingsBeforeProviderReads(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	capture.calls = nil
	delete(executor.LibraryCatalog.resources.resources, "example.com/cloud")
	result, err := executor.RefreshV2(context.Background())
	require.ErrorContains(t, err, "prior resource")
	require.Nil(t, result)
	require.Empty(t, capture.calls)
	current, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, applied.WrittenRev, current)
}

type refreshV2UnlockBackend struct {
	*unlockFailureBackend
	state.SnapshotBackendV2
}

func TestRefreshV2ReturnsCommittedResultWhenUnlockFails(t *testing.T) {
	executor := newFactoryApplyExecutor(t, &factoryApplyCapture{})
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	backend := &refreshV2UnlockBackend{
		unlockFailureBackend: &unlockFailureBackend{Backend: executor.Store},
		SnapshotBackendV2:    executor.Store.(state.SnapshotBackendV2),
	}
	executor.Store = backend
	result, err := executor.RefreshV2(context.Background())
	var unlock *StateUnlockError
	require.ErrorAs(t, err, &unlock)
	require.True(t, backend.unlocked)
	require.NotNil(t, result)
	require.Equal(t, 4, result.Refreshed)
	require.NotEqual(t, applied.WrittenRev, result.WrittenRev)
	current, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, current, result.WrittenRev)
}

func TestRefreshV2LeavesEmptyStoresUnchanged(t *testing.T) {
	executor := newFactoryApplyExecutor(t, &factoryApplyCapture{})
	result, err := executor.RefreshV2(context.Background())
	require.NoError(t, err)
	require.Equal(t, &RefreshResult{}, result)
	_, err = executor.Store.CurrentRev()
	require.ErrorIs(t, err, state.ErrNoCurrent)
}

func TestRefreshV2RejectsObsoleteSnapshots(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	prior := state.NewSnapshot(executor.Factory, executor.Store.Stack())
	revision, err := executor.Store.Write(prior)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
	result, err := executor.RefreshV2(context.Background())
	require.ErrorContains(t, err, "obsolete alpha format")
	require.Nil(t, result)
	require.Empty(t, capture.calls)
	current, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, revision, current)
}
