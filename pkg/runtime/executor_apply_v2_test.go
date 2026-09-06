package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestExecutorApplyPlanV2CreatesNestedResources(t *testing.T) {
	for _, fixture := range []string{"composites", "pending-composite"} {
		t.Run(fixture, func(t *testing.T) {
			executor := newFactoryV2CompositeExecutor(t, fixture)
			executor.Store = newStateStore(t)
			executor.Factory = newPlanEvaluationV2Snapshot(t).Factory
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			result, err := executor.ApplyPlanV2(context.Background(), plan)
			require.NoError(t, err)
			require.Equal(t, "first", result.Outputs["value"])
			require.NotEmpty(t, result.WrittenRev)
			store := executor.Store.(state.SnapshotBackendV2)
			snapshot, err := store.GetV2(result.WrittenRev)
			require.NoError(t, err)
			require.NoError(t, snapshot.Validate())
			require.Len(t, snapshot.Entries, len(plan.Steps)-1)
			next, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			for _, step := range next.Steps {
				if step.Operation.Resource != nil {
					require.NotNil(t, step.Operation.Resource.Prior)
				}
			}
			executor.Destroy = true
			destroy, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			executor.Destroy = false
			result, err = executor.ApplyPlanV2(context.Background(), destroy)
			require.NoError(t, err)
			snapshot, err = store.GetV2(result.WrittenRev)
			require.NoError(t, err)
			require.Empty(t, snapshot.Entries)
			require.Empty(t, result.Outputs)
		})
	}
}

func TestExecutorApplyPlanV2UsesSavedInputsAndConfigurations(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	executor.Store, executor.Factory = newStateStore(t), snapshot.Factory
	executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	executor.Inputs["name"] = "different"
	result, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	stored, err := executor.Store.(state.SnapshotBackendV2).GetV2(result.WrittenRev)
	require.NoError(t, err)
	target := stored.Find("resource.main").Payload.Resource.Target
	fields, _ := target.Inputs.ObjectFields()
	require.Equal(t, StringValue("server"), fields["name"])
	for _, step := range plan.Steps {
		if step.Operation.LibraryConfiguration != nil {
			require.Equal(t,
				*step.Operation.LibraryConfiguration.Result.Record, target.Configuration,
			)
		}
	}
}

type factoryApplyCapture struct {
	readErr error
	mu      sync.Mutex
	calls   []string
	create  func(context.Context, string) error
	read    func(*registeredPlanningOutput) *registeredPlanningOutput
	runs    int
	reads   int
}

func (c *factoryApplyCapture) record(operation, name, endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, operation+":"+name+":"+endpoint)
}

type factoryApplyResource struct {
	Name    string `ub:"name"`
	Size    int    `ub:"size"`
	capture *factoryApplyCapture
}

func (r *factoryApplyResource) Create(
	ctx context.Context, configuration *recordedConfiguration,
) (*registeredPlanningOutput, error) {
	r.capture.record("create", r.Name, configuration.Endpoint)
	if r.capture.create != nil {
		if err := r.capture.create(ctx, r.Name); err != nil {
			return nil, err
		}
	}
	return &registeredPlanningOutput{ID: r.Name, Value: fmt.Sprintf("%s-%d", r.Name, r.Size)}, nil
}

func (r *factoryApplyResource) Read(
	_ context.Context, configuration *recordedConfiguration, prior *registeredPlanningOutput,
) (*registeredPlanningOutput, error) {
	r.capture.record("read", r.Name, configuration.Endpoint)
	if r.capture.readErr != nil {
		return nil, r.capture.readErr
	}
	if r.capture.read != nil {
		return r.capture.read(prior), nil
	}
	return prior, nil
}

func (r *factoryApplyResource) Update(
	_ context.Context, configuration *recordedConfiguration,
	prior Prior[factoryApplyResource, *registeredPlanningOutput],
) (*registeredPlanningOutput, error) {
	r.capture.record("update", r.Name, configuration.Endpoint)
	return &registeredPlanningOutput{
		ID: prior.Outputs.ID, Value: fmt.Sprintf("%s-%d", r.Name, r.Size),
	}, nil
}

func (r *factoryApplyResource) Delete(
	_ context.Context, configuration *recordedConfiguration, _ *registeredPlanningOutput,
) error {
	r.capture.record("delete", r.Name, configuration.Endpoint)
	return nil
}

type factoryApplyLookup struct {
	Name    string `ub:"name"`
	capture *factoryApplyCapture
}

func (r *factoryApplyLookup) Read(context.Context, any) (*factoryV2LookupOutput, error) {
	r.capture.mu.Lock()
	defer r.capture.mu.Unlock()
	r.capture.reads++
	return &factoryV2LookupOutput{Sizes: map[string]int{"": 2, "blue": 1}}, nil
}

func (r *factoryApplyLookup) Run(context.Context, any) (*factoryV2LookupOutput, error) {
	r.capture.mu.Lock()
	defer r.capture.mu.Unlock()
	r.capture.runs++
	return &factoryV2LookupOutput{Sizes: map[string]int{r.Name: 1}}, nil
}

func newFactoryApplyExecutor(t *testing.T, capture *factoryApplyCapture) *Executor {
	t.Helper()
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/seed", New: func() *Library {
			return &Library{Resources: map[string]ResourceRegistration{
				"plain": MakeResource[plainResource, *plainResourceOutput, any](
					plainResourceDefinition(),
				),
			}}
		}},
		{LibraryPath: "example.com/cloud", New: func() *Library {
			return &Library{
				Configuration: configurationRegistration(1, nil),
				Resources: map[string]ResourceRegistration{
					"server": MakeResourceWith[
						factoryApplyResource, *registeredPlanningOutput, *recordedConfiguration,
					](ResourceDefinition[
						factoryApplyResource, *registeredPlanningOutput, *recordedConfiguration,
					]{
						SchemaVersion: 1,
						Identity: ResourceIdentity[factoryApplyResource, *registeredPlanningOutput]{
							Version: 1, Scope: IdentityConfiguration,
							AddressInputs: []AnyInputField[factoryApplyResource]{
								InputField(func(r *factoryApplyResource) *string {
									return &r.Name
								}),
							},
							StableID: func(_ factoryApplyResource, out *registeredPlanningOutput) (
								string, error,
							) {
								return out.ID, nil
							},
						},
					}, func() *factoryApplyResource {
						return &factoryApplyResource{capture: capture}
					}),
				},
				DataSources: map[string]DataSourceRegistration{
					"lookup": MakeDataSourceWith[factoryApplyLookup, *factoryV2LookupOutput, any](
						func() *factoryApplyLookup { return &factoryApplyLookup{capture: capture} },
					),
				},
				Actions: map[string]ActionRegistration{
					"lookup": MakeActionWith[factoryApplyLookup, *factoryV2LookupOutput, any](
						func() *factoryApplyLookup { return &factoryApplyLookup{capture: capture} },
					),
				},
			}
		}},
	})
	require.NoError(t, err)
	executor := newCatalogPlanningExecutor(t, catalog)
	executor.Libraries["seed"] = catalog.libraries["example.com/seed"]
	source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "targets")
	executor.DAG, executor.SyntaxSource = syntaxDAGAndBody(t, source, executor.Libraries)
	executor.Factory = newPlanEvaluationV2Snapshot(t).Factory
	executor.Store = newStateStore(t)
	executor.Inputs = map[string]any{"name": "main", "size": int64(1)}
	return executor
}

func TestExecutorApplyPlanV2RunsEveryPrimitive(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	for _, test := range []struct {
		name     string
		input    string
		size     int64
		decision Decision
		runs     int
	}{
		{"create", "main", 1, DecisionCreate, 1},
		{"no-op", "main", 1, DecisionNoOp, 1},
		{"update", "main", 2, DecisionUpdate, 2},
		{"replace", "renamed", 2, DecisionReplace, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor.Inputs = map[string]any{"name": test.input, "size": test.size}
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			for _, step := range plan.Steps {
				if step.Address == "resource.main" {
					require.Equal(t, test.decision, step.Operation.Resource.Decision)
				}
			}
			result, err := executor.ApplyPlanV2(context.Background(), plan)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("%s-%d", test.input, test.size), result.Outputs["value"])
			require.Equal(t, test.runs, capture.runs)
			require.Contains(t, result.Actions, "notify")
			require.Contains(t, result.Data, "settings")
			snapshot, err := executor.Store.(state.SnapshotBackendV2).GetV2(result.WrittenRev)
			require.NoError(t, err)
			require.Len(t, snapshot.Entries, 6)
			require.Equal(t, []string{"data-source.settings"},
				snapshot.Find("resource.many['']").Payload.Resource.Target.DependsOn)
		})
	}
}

func TestExecutorApplyPlanV2RunsIndependentProvidersConcurrently(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	capture := &factoryApplyCapture{create: func(ctx context.Context, name string) error {
		if name != "server" {
			return nil
		}
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	executor := newFactoryApplyExecutor(t, capture)
	executor.Parallelism = 1
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	executor.Parallelism = 2
	require.Equal(t, 1, plan.Parallelism)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := executor.ApplyPlanV2(ctx, plan)
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("independent providers did not start concurrently")
		}
	}
	close(release)
	require.NoError(t, <-done)
}

func TestExecutorApplyPlanV2PreservesCompletedStateOnFailure(t *testing.T) {
	capture := &factoryApplyCapture{create: func(_ context.Context, name string) error {
		if name == "main" {
			return fmt.Errorf("provider unavailable")
		}
		return nil
	}}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.ErrorContains(t, err, "provider unavailable")
	require.Zero(t, capture.runs)
	revision, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	snapshot, err := executor.Store.(state.SnapshotBackendV2).GetV2(revision)
	require.NoError(t, err)
	require.NotNil(t, snapshot.Find("data-source.settings"))
	require.Nil(t, snapshot.Find("resource.main"))
	require.Nil(t, snapshot.Find("resource.child"))
}

func TestExecutorApplyPlanV2RejectsChangedRevision(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	before := append([]string(nil), capture.calls...)
	revision, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.ErrorContains(t, err, "state revision changed")
	require.Equal(t, before, capture.calls)
	after, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	require.Equal(t, revision, after)
}

func TestExecutorApplyPlanV2ChecksIdentityBeforeDeleting(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	executor.Destroy = true
	destroy, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	before, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	snapshot, err := executor.Store.(state.SnapshotBackendV2).GetV2(before)
	require.NoError(t, err)
	capture.calls = nil
	capture.read = func(prior *registeredPlanningOutput) *registeredPlanningOutput {
		return &registeredPlanningOutput{ID: "somebody-else", Value: prior.Value}
	}
	_, err = executor.ApplyPlanV2(context.Background(), destroy)
	require.ErrorContains(t, err, "recorded stable ID")
	for _, call := range capture.calls {
		require.NotContains(t, call, "delete:")
	}
	after, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	current, err := executor.Store.(state.SnapshotBackendV2).GetV2(after)
	require.NoError(t, err)
	for _, entry := range snapshot.Entries {
		if entry.Kind == state.StateResource {
			require.Equal(t, &entry, current.Find(entry.Address))
		}
	}
}

func TestExecutorApplyPlanV2ChecksCatalogBeforeSourceMoves(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			executor, snapshot := newStateMoveV2Executor(t)
			source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "root-move")
			_, body := syntaxDAGAndBody(t, source, executor.Libraries)
			executor.SyntaxSource.StateMoves = body.StateMoves
			store := newStateStore(t)
			snapshot.Stack = store.Stack()
			revision, err := store.WriteV2(snapshot)
			require.NoError(t, err)
			require.NoError(t, store.SetCurrent(revision))
			executor.Store, executor.Factory = store, snapshot.Factory
			executor.Inputs = map[string]any{"name": "server", "size": int64(1)}
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			if missing {
				delete(executor.LibraryCatalog.resources.resources, "example.com/cloud")
			}
			result, err := executor.ApplyPlanV2(context.Background(), plan)
			if missing {
				require.ErrorContains(t, err, "prior resource")
				after, err := store.CurrentRev()
				require.NoError(t, err)
				require.Equal(t, revision, after)
				return
			}
			require.NoError(t, err)
			current, err := store.GetV2(result.WrittenRev)
			require.NoError(t, err)
			require.Nil(t, current.Find("resource.old"))
			require.Equal(t, snapshot.Find("resource.old").Payload.Resource.Target.Identity,
				current.Find("resource.main").Payload.Resource.Target.Identity)
		})
	}
}

func TestExecutorApplyPlanV2HandlesOnlyOutputs(t *testing.T) {
	executor := newFactoryV2CompositeExecutor(t, "composites")
	source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "only-output")
	executor.DAG, executor.SyntaxSource = syntaxDAGAndBody(t, source, executor.Libraries)
	executor.Store = newStateStore(t)
	executor.Factory = newPlanEvaluationV2Snapshot(t).Factory
	executor.Inputs = map[string]any{"name": "saved"}
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	result, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"value": "saved"}, result.Outputs)
}

func TestExecutorApplyPlanV2ResolvesDeferredConfigurations(t *testing.T) {
	for _, fixture := range []string{"pending-configuration", "pending-configuration-local"} {
		t.Run(fixture, func(t *testing.T) {
			capture := &factoryApplyCapture{}
			executor := newFactoryApplyExecutor(t, capture)
			source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", fixture)
			executor.DAG, executor.SyntaxSource = syntaxDAGAndBody(t, source, executor.Libraries)
			plan, err := executor.PlanV2(context.Background())
			require.NoError(t, err)
			require.Zero(t, capture.reads)
			for _, step := range plan.Steps {
				if step.Operation.LibraryConfiguration != nil {
					require.Equal(t, PlannedConfigurationPending,
						step.Operation.LibraryConfiguration.Result.Kind)
				}
			}
			result, err := executor.ApplyPlanV2(context.Background(), plan)
			require.NoError(t, err)
			require.Equal(t, "server-1", result.Outputs["value"])
			require.Equal(t, 1, capture.reads)
			require.Equal(t, 1, capture.runs)
			require.Contains(t, capture.calls, "create:server:configured")
			snapshot, err := executor.Store.(state.SnapshotBackendV2).GetV2(result.WrittenRev)
			require.NoError(t, err)
			require.Equal(t, []string{"/value"}, snapshot.SensitivePaths)
			require.Equal(t, []string{"resource.seed"},
				snapshot.Find("resource.main").Payload.Resource.Target.DependsOn)
			target := snapshot.Find("resource.main").Payload.Resource.Target
			fields, _ := target.Configuration.Value.ObjectFields()
			require.Equal(t, StringValue("configured"), fields["endpoint"])

		})
	}
}

func TestExecutorApplyPlanV2DrainsAndReportsCompletedWork(t *testing.T) {
	drain := make(chan struct{})
	capture := &factoryApplyCapture{create: func(ctx context.Context, name string) error {
		if name == "main" {
			close(drain)
		}
		return ctx.Err()
	}}
	executor := newFactoryApplyExecutor(t, capture)
	executor.Parallelism = 1
	executor.Drain = drain
	events := make(chan ApplyEvent, 20)
	executor.Events = events
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(context.Background(), plan)
	require.ErrorIs(t, err, ErrInterrupted)
	failure, ok := AsApplyFailure(err)
	require.True(t, ok)
	require.Equal(t, ApplyFailureExecute, failure.Stage)
	revision, err := executor.Store.CurrentRev()
	require.NoError(t, err)
	snapshot, err := executor.Store.(state.SnapshotBackendV2).GetV2(revision)
	require.NoError(t, err)
	require.NotNil(t, snapshot.Find("resource.main"))
	require.Nil(t, snapshot.Find("resource.child"))
	require.Zero(t, capture.runs)
	close(events)
	stages := map[string][]ApplyStage{}
	for event := range events {
		stages[event.Address] = append(stages[event.Address], event.Stage)
	}
	require.Equal(t, []ApplyStage{StageStart, StageDone}, stages["resource.main"])
	require.NotContains(t, stages, "resource.child")
}

func TestExecutorApplyPlanV2ReportsProviderTimeout(t *testing.T) {
	capture := &factoryApplyCapture{create: func(ctx context.Context, name string) error {
		if name == "main" {
			<-ctx.Done()
		}
		return ctx.Err()
	}}
	executor := newFactoryApplyExecutor(t, capture)
	executor.DAG.Nodes["resource.main"].Timeout = 5 * time.Millisecond
	executor.Parallelism = 1
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	plan, err := executor.PlanV2(ctx)
	require.NoError(t, err)
	_, err = executor.ApplyPlanV2(ctx, plan)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	failure, ok := AsApplyFailure(err)
	require.True(t, ok)
	require.Equal(t, ApplyFailureExecute, failure.Stage)
	var stepError *ApplyError
	require.ErrorAs(t, err, &stepError)
	require.Equal(t, "resource.main", stepError.Address)
	require.Equal(t, "example.com/cloud", stepError.LibraryPath)
	require.Equal(t, "cloud", stepError.Alias)
	require.Equal(t, DecisionCreate, stepError.Decision)
	require.Less(t, stepError.Elapsed, time.Second)
}

func TestExecutorPlanAndApplyV2UseConfigurationInput(t *testing.T) {
	capture := &factoryApplyCapture{}
	executor := newFactoryApplyExecutor(t, capture)
	source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "configuration-input")
	executor.DAG, executor.SyntaxSource = syntaxDAGAndBody(t, source, executor.Libraries)
	value, _, err := decodeConcreteValue(testConfigurationValue(t), "configuration")
	require.NoError(t, err)
	executor.Inputs = map[string]any{"configuration": value}
	plan, err := executor.PlanV2(context.Background())
	require.NoError(t, err)
	for _, step := range plan.Steps {
		if op := step.Operation.LibraryConfiguration; op != nil {
			require.Contains(t, op.Result.Record.SensitivePaths, "")
		}
	}
	executor.Inputs = nil
	result, err := executor.ApplyPlanV2(context.Background(), plan)
	require.NoError(t, err)
	require.Equal(t, "server-1", result.Outputs["value"])
	require.Contains(t, capture.calls, "create:server:https://api.example")
}
