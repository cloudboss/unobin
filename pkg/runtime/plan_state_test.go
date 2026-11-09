package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type advancingStateBackend struct {
	state.Backend
	afterRead func()
}

func (b *advancingStateBackend) Current() (*state.Snapshot, error) {
	snapshot, err := b.Backend.Current()
	b.advance()
	return snapshot, err
}

func (b *advancingStateBackend) Get(revision string) (*state.Snapshot, error) {
	snapshot, err := b.Backend.Get(revision)
	b.advance()
	return snapshot, err
}

func (b *advancingStateBackend) advance() {
	if hook := b.afterRead; hook != nil {
		b.afterRead = nil
		hook()
	}
}

func TestPlanUsesSnapshotRevision(t *testing.T) {
	store := newStateStore(t)
	seedIncrementalState(t, store)
	revision, err := store.CurrentRev()
	require.NoError(t, err)
	entry := &state.Entry{
		Address: "resource.concurrent", Category: "resource",
		Binding: &state.Binding{Alias: "core", Export: "thing"},
		Inputs:  map[string]any{"name": "concurrent"},
		Outputs: map[string]any{"id": "created-concurrently"},
	}
	storeWithAdvance := &advancingStateBackend{Backend: store}
	storeWithAdvance.afterRead = func() { seedIncrementalState(t, store, entry) }
	executor, _ := emptyApplyExecutor(t, storeWithAdvance)
	plan, err := executor.Plan(t.Context())
	require.NoError(t, err)
	assert.Equal(t, revision, plan.StateRev)
	assert.Empty(t, plan.Steps)

	encoded, err := EncodePlan(plan)
	require.NoError(t, err)
	saved, err := DecodePlan(encoded)
	require.NoError(t, err)
	_, err = executor.ApplyPlan(t.Context(), saved)
	require.ErrorContains(t, err, "state-rev drift")
	current, err := store.Current()
	require.NoError(t, err)
	assert.Equal(t, []*state.Entry{entry}, current.Entries)
}

type planReadErrorBackend struct {
	state.Backend
	revisionErr error
	snapshotErr error
}

func (b *planReadErrorBackend) CurrentRev() (string, error) {
	return "revision", b.revisionErr
}

func (b *planReadErrorBackend) Get(string) (*state.Snapshot, error) {
	return nil, b.snapshotErr
}

func TestPlanRejectsStateReadErrors(t *testing.T) {
	failure := errors.New("state read failed")
	for _, tc := range []struct {
		name        string
		revisionErr error
		snapshotErr error
	}{
		{name: "revision", revisionErr: failure},
		{name: "snapshot", snapshotErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &planReadErrorBackend{
				Backend:     newStateStore(t),
				revisionErr: tc.revisionErr, snapshotErr: tc.snapshotErr,
			}
			executor, _ := emptyApplyExecutor(t, store)
			plan, err := executor.Plan(context.Background())
			assert.ErrorIs(t, err, failure)
			assert.Nil(t, plan)
		})
	}
}
