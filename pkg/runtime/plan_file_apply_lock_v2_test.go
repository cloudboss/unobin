package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestApplyPlanFileV2SnapshotsRejectsUnsupportedBackend(t *testing.T) {
	callbacks, err := applyPlanFileV2Snapshots(&applyPlanFileV2OldBackend{})
	require.ErrorContains(t, err, "state store does not support version 2 snapshots")
	require.Equal(t, applyPlanFileV2SnapshotCallbacks{}, callbacks)
}

func TestApplyPlanFileV2WithStateLockKeepsLockThroughApply(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	backend := &applyPlanFileV2LockBackend{
		stack:    plan.Stack,
		revision: plan.StateRevision,
	}
	var persisted []*state.SnapshotV2
	var current []string
	resourceApplied := false
	backend.snapshots = applyPlanFileV2SnapshotCallbacks{
		Load: func(revision string) (*state.SnapshotV2, error) {
			backend.recordLocked(t, "load:"+revision)
			return applyPlanFileV2Snapshot(t), nil
		},
		Write: func(snapshot *state.SnapshotV2) (string, error) {
			backend.recordLocked(t, "write")
			revision := []string{"state-2", "state-3"}[len(persisted)]
			persisted = append(persisted, snapshot)
			return revision, nil
		},
		SetCurrent: func(revision string) error {
			backend.recordLocked(t, "set-current:"+revision)
			current = append(current, revision)
			return nil
		},
	}

	result, err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
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
	require.Len(t, persisted, 2)
	require.NotNil(t, result)
	require.Equal(t, "state-3", result.WrittenRevision)
	require.Equal(t, persisted[1], result.Snapshot)
	require.NotSame(t, persisted[1], result.Snapshot)
	require.True(t, resourceApplied)
	require.False(t, backend.locked)
	require.Equal(t, []string{
		"lock",
		"current-revision",
		"stack",
		"load:" + plan.StateRevision,
		"write",
		"set-current:state-2",
		"resource",
		"write",
		"set-current:state-3",
		"unlock",
	}, backend.events)
	require.Equal(t, []string{"state-2", "state-3"}, current)
	require.Nil(t, persisted[0].Find("resource.old"))
	require.NotNil(t, persisted[0].Find("resource.api"))
	require.NotNil(t, persisted[1].Find("resource.api"))
	require.NoError(t, result.Snapshot.RemoveEntry("resource.api"))
	require.NotNil(t, persisted[1].Find("resource.api"))
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
	backend.snapshots = applyPlanFileV2SnapshotCallbacks{
		Load: func(string) (*state.SnapshotV2, error) {
			loaded = true
			return nil, errors.New("unexpected snapshot load")
		},
		Write: func(snapshot *state.SnapshotV2) (string, error) {
			backend.recordLocked(t, "write")
			persisted = append(persisted, snapshot)
			return "state-1", nil
		},
		SetCurrent: func(revision string) error {
			backend.recordLocked(t, "set-current:"+revision)
			return nil
		},
	}

	result, err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return errors.New("unexpected resource apply")
		}),
	)
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	require.NotNil(t, result)
	require.Equal(t, "state-1", result.WrittenRevision)
	require.Equal(t, persisted[0], result.Snapshot)
	require.NotSame(t, persisted[0], result.Snapshot)
	require.False(t, loaded)
	require.Equal(t, []string{
		"lock",
		"current-revision",
		"stack",
		"write",
		"set-current:state-1",
		"unlock",
	}, backend.events)
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
	backend.snapshots = noopApplyPlanFileV2SnapshotCallbacks(
		func(string) (*state.SnapshotV2, error) {
			loaded = true
			return applyPlanFileV2Snapshot(t), nil
		},
	)

	_, err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
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
			test.backend.snapshots = noopApplyPlanFileV2SnapshotCallbacks(
				func(revision string) (*state.SnapshotV2, error) {
					loaded = true
					return test.load(revision)
				},
			)

			_, err := applyPlanFileV2WithStateLock(
				context.Background(),
				test.backend,
				applyPlanFileV2Factory(plan),
				plan,
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
	backend.snapshots = noopApplyPlanFileV2SnapshotCallbacks(
		func(string) (*state.SnapshotV2, error) { return nil, nil },
	)

	_, err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
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
	backend.snapshots = noopApplyPlanFileV2SnapshotCallbacks(
		func(string) (*state.SnapshotV2, error) {
			loaded = true
			return applyPlanFileV2Snapshot(t), nil
		},
	)

	_, err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
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
	backend.snapshots = noopApplyPlanFileV2SnapshotCallbacks(
		func(string) (*state.SnapshotV2, error) {
			return applyPlanFileV2Snapshot(t), nil
		},
	)

	result, err := applyPlanFileV2WithStateLock(
		context.Background(),
		backend,
		applyPlanFileV2Factory(plan),
		plan,
		applyPlanFileV2Callbacks(func(
			context.Context,
			*applyStateV2,
			PlanStepV2,
		) error {
			return applyErr
		}),
	)
	require.ErrorIs(t, err, applyErr)
	require.Nil(t, result)
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

func TestApplyPlanFileV2WithStateLockStopsAfterSnapshotPersistenceFailure(
	t *testing.T,
) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	expectedErr := errors.New("state unavailable")
	tests := []struct {
		name          string
		writeErr      error
		setCurrentErr error
		wantEvents    []string
	}{
		{
			name:       "write",
			writeErr:   expectedErr,
			wantEvents: []string{"lock", "current-revision", "stack", "load", "write", "unlock"},
		},
		{
			name:          "set current",
			setCurrentErr: expectedErr,
			wantEvents: []string{
				"lock",
				"current-revision",
				"stack",
				"load",
				"write",
				"set-current:state-2",
				"unlock",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &applyPlanFileV2LockBackend{
				stack:    plan.Stack,
				revision: plan.StateRevision,
			}
			resourceApplied := false
			backend.snapshots = applyPlanFileV2SnapshotCallbacks{
				Load: func(string) (*state.SnapshotV2, error) {
					backend.recordLocked(t, "load")
					return applyPlanFileV2Snapshot(t), nil
				},
				Write: func(*state.SnapshotV2) (string, error) {
					backend.recordLocked(t, "write")
					return "state-2", test.writeErr
				},
				SetCurrent: func(revision string) error {
					backend.recordLocked(t, "set-current:"+revision)
					return test.setCurrentErr
				},
			}

			result, err := applyPlanFileV2WithStateLock(
				context.Background(),
				backend,
				applyPlanFileV2Factory(plan),
				plan,
				applyPlanFileV2Callbacks(func(
					context.Context,
					*applyStateV2,
					PlanStepV2,
				) error {
					resourceApplied = true
					return nil
				}),
			)
			require.ErrorIs(t, err, expectedErr)
			require.Nil(t, result)
			require.False(t, resourceApplied)
			require.False(t, backend.locked)
			require.Equal(t, test.wantEvents, backend.events)
		})
	}
}

func TestApplyPlanFileV2WithStateLockRejectsInvalidSetupBeforeLock(t *testing.T) {
	plan := applyPlanFileV2Plan(t, "resource.old", "resource.api")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	validBackend := func() *applyPlanFileV2LockBackend {
		backend := &applyPlanFileV2LockBackend{
			stack:    plan.Stack,
			revision: plan.StateRevision,
		}
		backend.snapshots = noopApplyPlanFileV2SnapshotCallbacks(
			func(string) (*state.SnapshotV2, error) {
				return applyPlanFileV2Snapshot(t), nil
			},
		)
		return backend
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
		callbacks applyPlanStepsV2Callbacks
		message   string
	}{
		{
			name:      "missing context",
			backend:   validBackend(),
			callbacks: callbacks,
			message:   "apply context is required",
		},
		{
			name:      "missing backend",
			ctx:       context.Background(),
			callbacks: callbacks,
			message:   "state store is required",
		},
		{
			name:      "unsupported snapshot backend",
			ctx:       context.Background(),
			backend:   &applyPlanFileV2OldBackend{},
			callbacks: callbacks,
			message:   "state store does not support version 2 snapshots",
		},
		{
			name:    "missing step callback",
			ctx:     context.Background(),
			backend: validBackend(),
			callbacks: applyPlanStepsV2Callbacks{
				Output: callbacks.Output,
			},
			message: "resource apply callback is required",
		},
		{
			name:      "canceled context",
			ctx:       canceledContext,
			backend:   validBackend(),
			callbacks: callbacks,
			message:   "context canceled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := applyPlanFileV2WithStateLock(
				test.ctx,
				test.backend,
				applyPlanFileV2Factory(plan),
				plan,
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

func noopApplyPlanFileV2SnapshotCallbacks(
	load func(string) (*state.SnapshotV2, error),
) applyPlanFileV2SnapshotCallbacks {
	return applyPlanFileV2SnapshotCallbacks{
		Load: load,
		Write: func(*state.SnapshotV2) (string, error) {
			return "state-2", nil
		},
		SetCurrent: func(string) error { return nil },
	}
}

type applyPlanFileV2LockBackend struct {
	state.Backend
	snapshots  applyPlanFileV2SnapshotCallbacks
	stack      string
	revision   string
	currentErr error
	lockErr    error
	unlockErr  error
	locked     bool
	events     []string
}

type applyPlanFileV2OldBackend struct {
	state.Backend
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

func (b *applyPlanFileV2LockBackend) GetV2(
	revision string,
) (*state.SnapshotV2, error) {
	return b.snapshots.Load(revision)
}

func (b *applyPlanFileV2LockBackend) WriteV2(
	snapshot *state.SnapshotV2,
) (string, error) {
	return b.snapshots.Write(snapshot)
}

func (b *applyPlanFileV2LockBackend) SetCurrent(revision string) error {
	return b.snapshots.SetCurrent(revision)
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
