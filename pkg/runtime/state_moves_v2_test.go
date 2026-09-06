package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func newStateMoveV2Executor(t *testing.T) (*Executor, *state.SnapshotV2) {
	t.Helper()
	capture := &registeredPlanningCapture{}
	library := newCatalogPlanningLibrary(capture, "current")
	catalog, err := NewLibraryCatalog([]LibraryRegistration{
		{LibraryPath: "example.com/cloud", New: func() *Library { return library }},
	})
	require.NoError(t, err)
	executor := newCatalogPlanningExecutor(t, catalog)
	prior := planEvaluationV2ResourceStepRequest(t, capture).Prior
	snapshot := newPlanEvaluationV2Snapshot(t)
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "resource.old", Kind: state.StateResource,
		Payload: state.StatePayload{
			Kind: state.StateResource, Resource: &state.ResourceStatePayload{Target: *prior},
		},
	})
	return executor, snapshot
}

func TestApplyEntryMovesV2PreservesRecordedTargets(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	action := operationActionState(t)
	action.DependsOn = []string{"resource.old"}
	addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
		Address: "action.consumer", Kind: state.StateAction,
		Payload: state.StatePayload{Kind: state.StateAction, Action: &action},
	})
	original, err := snapshot.Clone()
	require.NoError(t, err)
	moved, results, err := ApplyEntryMovesV2(snapshot, executor.DAG, executor.Libraries,
		[]EntryMoveSpec{
			{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.main"}},
		},
		EntryMoveStrict,
	)
	require.NoError(t, err)
	require.Equal(t, []EntryMoveResult{
		{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.main"}},
	}, results)
	require.Nil(t, moved.Find("resource.old"))
	require.Equal(t, original.Find("resource.old").Payload, moved.Find("resource.main").Payload)
	require.Equal(t, []string{"resource.main"}, moved.Find("action.consumer").Payload.Action.DependsOn)
	require.Equal(t, original, snapshot)
}

func TestApplyEntryMovesV2HandlesRepeatedAndChainedDeclarations(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	specs := []EntryMoveSpec{
		{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.middle"}},
		{From: EntryRef{Address: "resource.middle"}, To: EntryRef{Address: "resource.main"}},
	}
	moved, results, err := ApplyEntryMovesV2(
		snapshot, executor.DAG, executor.Libraries, specs, EntryMoveIdempotent,
	)
	require.NoError(t, err)
	require.Equal(t, []EntryMoveResult{
		{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.main"}},
	}, results)
	again, results, err := ApplyEntryMovesV2(
		moved, executor.DAG, executor.Libraries, specs, EntryMoveIdempotent,
	)
	require.NoError(t, err)
	require.Empty(t, results)
	require.Equal(t, moved, again)
	require.NotSame(t, moved, again)
}

func TestApplyEntryMovesV2RejectsInvalidMoves(t *testing.T) {
	for _, test := range []struct {
		name   string
		specs  []EntryMoveSpec
		change func(*Executor, *state.SnapshotV2)
		want   string
	}{
		{name: "occupied destination", specs: []EntryMoveSpec{
			{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.main"}},
		}, change: func(_ *Executor, snapshot *state.SnapshotV2) {
			entry := *snapshot.Find("resource.old")
			entry.Address = "resource.main"
			addPlanEvaluationV2Entry(t, snapshot, entry)
		}, want: "destination already exists"},
		{name: "wrong implementation", specs: []EntryMoveSpec{
			{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.main"}},
		}, change: func(e *Executor, _ *state.SnapshotV2) {
			e.DAG.Nodes["resource.main"].Type = "different"
		}, want: "implementation differs"},
		{name: "wrong category", specs: []EntryMoveSpec{
			{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "action.main"}},
		}, change: func(e *Executor, _ *state.SnapshotV2) {
			e.DAG.Nodes["action.main"] = &Node{
				Address: "action.main", Kind: NodeAction, Alias: "cloud", Type: "server",
			}
		}, want: "resource entry cannot move to action"},
		{name: "absent source", specs: []EntryMoveSpec{
			{From: EntryRef{Address: "resource.absent"}, To: EntryRef{Address: "resource.main"}},
		}, want: "no entry at resource.absent"},
		{name: "cycle", specs: []EntryMoveSpec{
			{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.main"}},
			{From: EntryRef{Address: "resource.main"}, To: EntryRef{Address: "resource.old"}},
		}, want: "cycle"},
		{name: "missing destination", specs: []EntryMoveSpec{
			{From: EntryRef{Address: "resource.old"}, To: EntryRef{Address: "resource.absent"}},
		}, want: "destination is not in this factory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, snapshot := newStateMoveV2Executor(t)
			if test.change != nil {
				test.change(executor, snapshot)
			}
			original, err := snapshot.Clone()
			require.NoError(t, err)
			moved, results, err := ApplyEntryMovesV2(
				snapshot, executor.DAG, executor.Libraries, test.specs, EntryMoveStrict,
			)
			require.ErrorContains(t, err, test.want)
			require.Nil(t, moved)
			require.Empty(t, results)
			require.Equal(t, original, snapshot)
		})
	}
}

func TestPlanSourceEntryMovesV2PreservesResourceOperation(t *testing.T) {
	executor, snapshot := newStateMoveV2Executor(t)
	source := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "root-move")
	_, body := syntaxDAGAndBody(t, source, executor.Libraries)
	executor.SyntaxSource.StateMoves = body.StateMoves
	moved, moves, err := executor.planSourceEntryMovesV2(snapshot)
	require.NoError(t, err)
	require.Equal(t, []PlannedEntryMove{{From: "resource.old", To: "resource.main"}}, moves)
	steps, err := executor.planFactoryStepsV2(context.Background(),
		operationObject(t, map[string]EncodedValue{
			"name": StringValue("server"), "size": IntegerValue(1),
		}),
		moved,
	)
	require.NoError(t, err)
	require.Contains(t, factoryV2StepAddresses(steps), "resource.main")
	for _, step := range steps {
		require.NotEqual(t, "resource.old", step.Address)
		if step.Address == "resource.main" {
			require.Equal(t, DecisionNoOp, step.Operation.Resource.Decision)
		}
	}
	again, moves, err := executor.planSourceEntryMovesV2(moved)
	require.NoError(t, err)
	require.Empty(t, moves)
	require.Equal(t, moved, again)
	require.NotNil(t, snapshot.Find("resource.old"))
}

func TestSourceEntryMoveSpecsV2ExpandsMovedNestedComposites(t *testing.T) {
	executor := newFactoryV2CompositeExecutor(t, "composites")
	root := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "composite-move")
	_, rootBody := syntaxDAGAndBody(t, root, executor.Libraries)
	executor.SyntaxSource.StateMoves = rootBody.StateMoves
	inner := ubtest.ReadValidFixture(t, "testdata/ub/plan-factory-v2", "inner-move")
	_, innerBody := syntaxDAGAndBody(t, inner, nil)
	innerNode := executor.DAG.Nodes["resource.apps/resource.boxes"]
	innerNode.CompositeSyntaxBody.StateMoves = innerBody.StateMoves
	snapshot := newPlanEvaluationV2Snapshot(t)
	for _, item := range []struct{ address, path string }{
		{"resource.old-app", "example.com/app"},
		{"resource.old-app/resource.boxes['']", "example.com/inner"},
		{"resource.apps['z']", "example.com/app"},
		{"resource.apps['z']/resource.boxes['']", "example.com/inner"},
	} {
		composite := operationCompositeState(t, NodeResource)
		composite.Binding = Binding{LibraryPath: item.path, Export: "box"}
		addPlanEvaluationV2Entry(t, snapshot, state.StateEntryV2{
			Address: item.address, Kind: state.StateComposite,
			Payload: state.StatePayload{Kind: state.StateComposite, Composite: &composite},
		})
	}
	specs, err := executor.sourceEntryMoveSpecsV2(snapshot)
	require.NoError(t, err)
	require.ElementsMatch(t, []EntryMoveSpec{
		{From: EntryRef{Address: "resource.old-app"}, To: EntryRef{Address: "resource.apps['a/b']"}},
		{From: EntryRef{Address: "resource.old-app/resource.boxes['']/resource.old"},
			To: EntryRef{Address: "resource.apps['a/b']/resource.boxes['']/resource.leaf"}},
		{From: EntryRef{Address: "resource.apps['z']/resource.boxes['']/resource.old"},
			To: EntryRef{Address: "resource.apps['z']/resource.boxes['']/resource.leaf"}},
	}, specs)
	moved, moves, err := executor.planSourceEntryMovesV2(snapshot)
	require.NoError(t, err)
	require.Equal(t, []PlannedEntryMove{
		{From: "resource.old-app", To: "resource.apps['a/b']"},
		{From: "resource.old-app/resource.boxes['']", To: "resource.apps['a/b']/resource.boxes['']"},
	}, moves)
	require.Equal(t, []string{
		"resource.apps['a/b']", "resource.apps['a/b']/resource.boxes['']",
		"resource.apps['z']", "resource.apps['z']/resource.boxes['']",
	}, stateMoveEntryAddresses(moved))
}
