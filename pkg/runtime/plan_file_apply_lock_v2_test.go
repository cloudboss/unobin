package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyPlanFileV2WithStateLockKeepsLockThroughApply(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	backend := &applyPlanFileV2LockBackend{
		stack:    plan.Stack,
		revision: plan.StateRevision,
	}
	var persisted []*state.SnapshotV2
	resourceApplied := false

	err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2SnapshotCallbacks{
			Load: func(revision string) (*state.SnapshotV2, error) {
				backend.recordLocked(t, "load:"+revision)
				return applyPlanFileV2Snapshot(t), nil
			},
			Persist: func(_ context.Context, snapshot *state.SnapshotV2) error {
				backend.recordLocked(t, "persist")
				persisted = append(persisted, snapshot)
				return nil
			},
		},
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			backend.recordLocked(t, "resource")
			resourceApplied = true
			return nil
		}),
	)
	require.NoError(t, err)
	require.True(t, resourceApplied)
	require.False(t, backend.locked)
	require.Equal(t, []string{
		"lock",
		"current-revision",
		"stack",
		"load:" + plan.StateRevision,
		"persist",
		"resource",
		"persist",
		"unlock",
	}, backend.events)
	require.Len(t, persisted, 2)
	require.Nil(t, persisted[0].Find("resource.old"))
	require.NotNil(t, persisted[0].Find("resource.api"))
}

func TestApplyPlanFileV2WithStateLockInitializesNewSnapshot(t *testing.T) {
	plan := validPlanFileV2(t)
	plan.StateRevision = ""
	plan.StateMoves = []PlannedEntryMove{}
	plan.Steps = []PlanStepV2{}
	plan = finalizeApplyPlanFileV2(t, plan)
	backend := &applyPlanFileV2LockBackend{
		stack:      plan.Stack,
		currentErr: state.ErrNoCurrent,
	}
	loaded := false
	var persisted []*state.SnapshotV2

	err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2SnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				loaded = true
				return nil, errors.New("unexpected snapshot load")
			},
			Persist: func(_ context.Context, snapshot *state.SnapshotV2) error {
				backend.recordLocked(t, "persist")
				persisted = append(persisted, snapshot)
				return nil
			},
		},
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return errors.New("unexpected resource apply")
		}),
	)
	require.NoError(t, err)
	require.False(t, loaded)
	require.Equal(t, []string{
		"lock",
		"current-revision",
		"stack",
		"persist",
		"unlock",
	}, backend.events)
	require.Len(t, persisted, 1)
	require.Equal(t, applyPlanFileV2Factory(plan), persisted[0].Factory)
	require.Equal(t, plan.Stack, persisted[0].Stack)
	require.Empty(t, persisted[0].Entries)
	emptyOutputs, object := persisted[0].Outputs.ObjectFields()
	require.True(t, object)
	require.Empty(t, emptyOutputs)
	require.Empty(t, persisted[0].SensitivePaths)
}

func TestApplyPlanFileV2WithStateLockRejectsRevisionDriftBeforeStatePreparation(
	t *testing.T,
) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	backend := &applyPlanFileV2LockBackend{
		stack:    plan.Stack,
		revision: "state-2",
	}
	loaded := false

	err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2SnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				loaded = true
				return applyPlanFileV2Snapshot(t), nil
			},
			Persist: func(context.Context, *state.SnapshotV2) error { return nil },
		},
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return nil
		}),
	)
	require.ErrorContains(t, err, "state revision changed")
	require.False(t, loaded)
	require.Equal(t, []string{
		"lock",
		"current-revision",
		"stack",
		"unlock",
	}, backend.events)
}

func TestApplyPlanFileV2WithStateLockReleasesAfterSetupFailures(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	expectedErr := errors.New("state unavailable")
	tests := []struct {
		name    string
		backend *applyPlanFileV2LockBackend
		load    func(string) (*state.SnapshotV2, error)
		message string
		loaded  bool
	}{
		{
			name: "current revision",
			backend: &applyPlanFileV2LockBackend{
				stack:      plan.Stack,
				currentErr: expectedErr,
			},
			load: func(string) (*state.SnapshotV2, error) {
				return applyPlanFileV2Snapshot(t), nil
			},
			message: "current revision",
		},
		{
			name: "snapshot load",
			backend: &applyPlanFileV2LockBackend{
				stack:    plan.Stack,
				revision: plan.StateRevision,
			},
			load: func(string) (*state.SnapshotV2, error) {
				return nil, expectedErr
			},
			message: "load version 2 snapshot",
			loaded:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := false
			load := func(revision string) (*state.SnapshotV2, error) {
				loaded = true
				return test.load(revision)
			}

			err := applyPlanFileV2WithStateLock(
				context.Background(),
				test.backend,
				applyPlanFileV2Factory(plan),
				plan,
				applyPlanFileV2SnapshotCallbacks{
					Load:    load,
					Persist: func(context.Context, *state.SnapshotV2) error { return nil },
				},
				applyPlanFileV2Callbacks(func(
					context.Context,
					*applyStateV2,
					PlanStepV2,
				) error {
					return nil
				}),
			)
			require.ErrorContains(t, err, test.message)
			require.ErrorIs(t, err, expectedErr)
			require.Equal(t, test.loaded, loaded)
			require.False(t, test.backend.locked)
			require.Equal(t, "unlock", test.backend.events[len(test.backend.events)-1])
		})
	}
}

func TestApplyPlanFileV2WithStateLockRejectsNilLoadedSnapshot(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	backend := &applyPlanFileV2LockBackend{
		stack:    plan.Stack,
		revision: plan.StateRevision,
	}

	err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2SnapshotCallbacks{
			Load:    func(string) (*state.SnapshotV2, error) { return nil, nil },
			Persist: func(context.Context, *state.SnapshotV2) error { return nil },
		},
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return nil
		}),
	)
	require.ErrorContains(t, err, "loader returned nil snapshot")
	require.False(t, backend.locked)
	require.Equal(t, "unlock", backend.events[len(backend.events)-1])
}

func TestApplyPlanFileV2WithStateLockStopsWhenLockFails(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	expectedErr := errors.New("lock unavailable")
	backend := &applyPlanFileV2LockBackend{
		stack:    plan.Stack,
		revision: plan.StateRevision,
		lockErr:  expectedErr,
	}
	loaded := false

	err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2SnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				loaded = true
				return applyPlanFileV2Snapshot(t), nil
			},
			Persist: func(context.Context, *state.SnapshotV2) error { return nil },
		},
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return nil
		}),
	)
	require.ErrorIs(t, err, expectedErr)
	require.ErrorContains(t, err, "acquire lock")
	require.False(t, loaded)
	require.False(t, backend.locked)
	require.Equal(t, []string{"lock"}, backend.events)
}

func TestApplyPlanFileV2WithStateLockJoinsApplyAndUnlockFailures(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	applyErr := errors.New("resource failed")
	unlockErr := errors.New("unlock failed")
	backend := &applyPlanFileV2LockBackend{
		stack:     plan.Stack,
		revision:  plan.StateRevision,
		unlockErr: unlockErr,
	}

	err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2SnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				return applyPlanFileV2Snapshot(t), nil
			},
			Persist: func(context.Context, *state.SnapshotV2) error { return nil },
		},
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return applyErr
		}),
	)
	require.ErrorIs(t, err, applyErr)
	var stateUnlockErr *StateUnlockError
	require.ErrorAs(t, err, &stateUnlockErr)
	require.ErrorIs(t, stateUnlockErr.Cause, unlockErr)
	require.Equal(t, []string{
		"lock",
		"current-revision",
		"stack",
		"unlock",
	}, backend.events)
}

func TestApplyPlanFileV2WithStateLockRejectsInvalidSetupBeforeLock(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	validBackend := func() *applyPlanFileV2LockBackend {
		return &applyPlanFileV2LockBackend{
			stack:    plan.Stack,
			revision: plan.StateRevision,
		}
	}
	validSnapshots := func() applyPlanFileV2SnapshotCallbacks {
		return applyPlanFileV2SnapshotCallbacks{
			Load: func(string) (*state.SnapshotV2, error) {
				return applyPlanFileV2Snapshot(t), nil
			},
			Persist: func(context.Context, *state.SnapshotV2) error { return nil },
		}
	}
	callbacks := applyPlanFileV2Callbacks(func(
		context.Context,
		*applyStateV2,
		PlanStepV2,
	) error {
		return nil
	})
	tests := []struct {
		name      string
		ctx       context.Context
		backend   state.Backend
		snapshots applyPlanFileV2SnapshotCallbacks
		callbacks applyPlanStepsV2Callbacks
		message   string
	}{
		{
			name:      "missing context",
			backend:   validBackend(),
			snapshots: validSnapshots(),
			callbacks: callbacks,
			message:   "apply context is required",
		},
		{
			name:      "missing backend",
			ctx:       context.Background(),
			snapshots: validSnapshots(),
			callbacks: callbacks,
			message:   "state store is required",
		},
		{
			name:    "missing snapshot loader",
			ctx:     context.Background(),
			backend: validBackend(),
			snapshots: applyPlanFileV2SnapshotCallbacks{
				Persist: func(context.Context, *state.SnapshotV2) error { return nil },
			},
			callbacks: callbacks,
			message:   "version 2 snapshot loader is required",
		},
		{
			name:    "missing snapshot persistence",
			ctx:     context.Background(),
			backend: validBackend(),
			snapshots: applyPlanFileV2SnapshotCallbacks{
				Load: func(string) (*state.SnapshotV2, error) {
					return applyPlanFileV2Snapshot(t), nil
				},
			},
			callbacks: callbacks,
			message:   "version 2 snapshot persistence callback is required",
		},
		{
			name:      "missing step callback",
			ctx:       context.Background(),
			backend:   validBackend(),
			snapshots: validSnapshots(),
			callbacks: applyPlanStepsV2Callbacks{
				Output: callbacks.Output,
			},
			message: "resource apply callback is required",
		},
		{
			name:      "canceled context",
			ctx:       canceledContext,
			backend:   validBackend(),
			snapshots: validSnapshots(),
			callbacks: callbacks,
			message:   "context canceled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := applyPlanFileV2WithStateLock(
				test.ctx,
				test.backend,
				applyPlanFileV2Factory(plan),
				plan,
				test.snapshots,
				test.callbacks,
			)
			require.ErrorContains(t, err, test.message)
			if backend, ok := test.backend.(*applyPlanFileV2LockBackend); ok {
				require.Empty(t, backend.events)
			}
		})
	}
}

func applyPlanFileV2Factory(plan PlanFileV2) state.FactoryInfo {
	return state.FactoryInfo{
		Name:            plan.Factory.Name,
		Version:         plan.Factory.Version,
		ContentRevision: plan.Factory.ContentRevision,
	}
}

type applyPlanFileV2LockBackend struct {
	state.Backend
	stack      string
	revision   string
	currentErr error
	lockErr    error
	unlockErr  error
	locked     bool
	events     []string
}

func (b *applyPlanFileV2LockBackend) Stack() string {
	b.events = append(b.events, "stack")
	return b.stack
}

func (b *applyPlanFileV2LockBackend) CurrentRev() (string, error) {
	if !b.locked {
		return "", errors.New("current revision read without lock")
	}
	b.events = append(b.events, "current-revision")
	return b.revision, b.currentErr
}

func (b *applyPlanFileV2LockBackend) Lock(context.Context) (state.Lock, error) {
	b.events = append(b.events, "lock")
	if b.lockErr != nil {
		return nil, b.lockErr
	}
	b.locked = true
	return &applyPlanFileV2Lock{backend: b}, nil
}

func (b *applyPlanFileV2LockBackend) recordLocked(t *testing.T, event string) {
	t.Helper()
	require.True(t, b.locked)
	b.events = append(b.events, event)
}

type applyPlanFileV2Lock struct {
	backend *applyPlanFileV2LockBackend
}

func (l *applyPlanFileV2Lock) Unlock() error {
	l.backend.events = append(l.backend.events, "unlock")
	l.backend.locked = false
	return l.backend.unlockErr
}
