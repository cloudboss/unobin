package runtime

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
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
				require.Equal(t, entry.Payload.Resource.Target.Inputs, target.Inputs)
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

func TestRefreshV2WaitsForLock(t *testing.T) {
	executor := newFactoryApplyExecutor(t, &factoryApplyCapture{})
	held, err := executor.Store.Lock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, held.Unlock()) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := executor.RefreshV2(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, result)
}

func TestRefreshV2ReadsResourcesConcurrently(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan string, 4)
	release := make(chan struct{})
	capture.read = func(prior *registeredPlanningOutput) *registeredPlanningOutput {
		started <- prior.ID
		select {
		case <-release:
		case <-ctx.Done():
		}
		return prior
	}
	executor.Parallelism = 2
	done := make(chan error, 1)
	go func() {
		_, err := executor.RefreshV2(ctx)
		done <- err
	}()
	var readIDs []string
	for range 2 {
		select {
		case id := <-started:
			readIDs = append(readIDs, id)
		case <-ctx.Done():
			t.Fatal("refresh did not start two concurrent reads")
		}
	}
	close(release)
	require.NoError(t, <-done)
	for range 2 {
		readIDs = append(readIDs, <-started)
	}
	require.ElementsMatch(t, []string{"main", "main-1", "server", "server"}, readIDs)
}

func TestRefreshV2PersistsMigratedInputsAndOutputs(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	store := executor.Store.(state.SnapshotBackendV2)
	prior, err := store.GetV2(applied.WrittenRev)
	require.NoError(t, err)
	expected := prior.Find("resource.main").Payload.Resource.Target
	for i := range prior.Entries {
		if prior.Entries[i].Kind != state.StateResource {
			continue
		}
		target := &prior.Entries[i].Payload.Resource.Target
		inputs, _ := target.Inputs.ObjectFields()
		inputs["count"] = inputs["size"]
		delete(inputs, "size")
		target.Inputs = operationObject(t, inputs)
		outputs, _ := target.Outputs.ObjectFields()
		outputs["old-value"] = outputs["value"]
		delete(outputs, "value")
		target.Outputs = operationObject(t, outputs)
	}
	revision, err := store.WriteV2(prior)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
	library := executor.LibraryCatalog.libraries["example.com/cloud"]
	definition := ResourceDefinition[
		factoryApplyResource, *registeredPlanningOutput, *recordedConfiguration,
	]{
		SchemaVersion: 2,
		Identity: ResourceIdentity[factoryApplyResource, *registeredPlanningOutput]{
			Version: 1, Scope: IdentityConfiguration,
			AddressInputs: []AnyInputField[factoryApplyResource]{
				InputField(func(r *factoryApplyResource) *string { return &r.Name }),
			},
			StableID: func(_ factoryApplyResource, out *registeredPlanningOutput) (string, error) {
				return out.ID, nil
			},
		},
		Migrate: func(version int, old ResourceMigrationState) (ResourceMigrationState, error) {
			require.Equal(t, 1, version)
			inputs, _ := old.Inputs.ObjectFields()
			inputs["size"] = inputs["count"]
			delete(inputs, "count")
			outputs, _ := old.Outputs.ObjectFields()
			outputs["value"] = outputs["old-value"]
			delete(outputs, "old-value")
			return ResourceMigrationState{
				Inputs: operationObject(t, inputs), Outputs: operationObject(t, outputs),
			}, nil
		},
	}
	library.Resources["server"] = MakeResourceWith(definition, func() *factoryApplyResource {
		return &factoryApplyResource{capture: capture}
	})
	executor.LibraryCatalog, err = NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/cloud", New: func() *Library { return library }},
	})
	require.NoError(t, err)
	result, err := executor.RefreshV2(context.Background())
	require.NoError(t, err)
	current, err := store.GetV2(result.WrittenRev)
	require.NoError(t, err)
	expected.SchemaVersion = 2
	require.Equal(t, expected, current.Find("resource.main").Payload.Resource.Target)
}

func TestRefreshV2UsesRecordedConfigurationAfterSourceChanges(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "pending-configuration")
	executor.DAG, executor.SyntaxSource = syntaxDAGAndBody(t, source, executor.Libraries)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	capture.calls = nil
	executor.DAG, executor.SyntaxSource, executor.Libraries = nil, nil, nil
	executor.Inputs = map[string]any{"configuration": map[string]any{"endpoint": "changed"}}
	_, err = executor.RefreshV2(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"read:server:configured"}, capture.calls)
}

func TestRefreshV2PreservesAbsentInputsDespiteCurrentDefaults(t *testing.T) {
	executor := newFactoryApplyExecutor(t, &factoryApplyCapture{})
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	store := executor.Store.(state.SnapshotBackendV2)
	prior, err := store.GetV2(applied.WrittenRev)
	require.NoError(t, err)
	target := &prior.Find("resource.main").Payload.Resource.Target
	fields, _ := target.Inputs.ObjectFields()
	fields["size"] = AbsentValue()
	target.Inputs = operationObject(t, fields)
	expected := target.Inputs
	revision, err := store.WriteV2(prior)
	require.NoError(t, err)
	require.NoError(t, executor.Store.SetCurrent(revision))
	executor.Libraries["cloud"].Defaults = map[string][]lang.DefaultSpec{
		"resource.server": {{Field: "input.size", Value: "7"}},
	}
	result, err := executor.RefreshV2(context.Background())
	require.NoError(t, err)
	current, err := store.GetV2(result.WrittenRev)
	require.NoError(t, err)
	require.Equal(t, expected, current.Find("resource.main").Payload.Resource.Target.Inputs)
}

func TestRefreshV2UpdatesNestedResourceObservations(t *testing.T) {
	var counters resourceCounters
	executor := newFactoryV2CompositeExecutor(t, "composites",
		resourceModules(&counters)["core"].Resources["thing"])
	executor.Store, executor.Factory = newStateStore(t), newPlanEvaluationV2Snapshot(t).Factory
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	applied, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	store := executor.Store.(state.SnapshotBackendV2)
	prior, err := store.GetV2(applied.WrittenRev)
	require.NoError(t, err)
	var expected []state.StateEntryV2
	for _, entry := range prior.Entries {
		if entry.Kind == state.StateResource {
			target := &entry.Payload.Resource.Target
			fields, _ := target.Outputs.ObjectFields()
			fields["size"] = IntegerValue(42)
			target.Outputs = operationObject(t, fields)
			expected = append(expected, entry)
		}
	}
	require.NotEmpty(t, expected)
	counters.readFn = func(prior *countingResourceOutput) (*countingResourceOutput, error) {
		observed := *prior
		observed.Size = 42
		return &observed, nil
	}
	executor.DAG, executor.Libraries, executor.SyntaxSource = nil, nil, nil
	result, err := executor.RefreshV2(context.Background())
	require.NoError(t, err)
	current, err := store.GetV2(result.WrittenRev)
	require.NoError(t, err)
	require.Equal(t, len(expected), result.Refreshed)
	actual := slices.DeleteFunc(current.Entries, func(entry state.StateEntryV2) bool {
		return entry.Kind != state.StateResource
	})
	require.Equal(t, expected, actual)
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
