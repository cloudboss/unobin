package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type stateGCGolden struct {
	Cases []stateGCCaseGolden `json:"cases"`
}

type stateGCCaseGolden struct {
	Name        string                  `json:"name"`
	Result      *stateGCResult          `json:"result"`
	Error       string                  `json:"error"`
	Diagnostics []diagnostic.Diagnostic `json:"diagnostics"`
	Deletes     []string                `json:"deletes"`
	UnlockCalls int                     `json:"unlock-calls"`
}

func TestStateGCOperationGolden(t *testing.T) {
	result := stateGCGolden{}
	for _, tc := range []struct {
		name       string
		revisions  []string
		current    string
		currentErr error
		deleteErr  map[string]error
		unlockErr  error
	}{
		{
			name: "partial deletion", revisions: []string{"rev-1", "rev-2", "rev-3"},
			current: "rev-3", deleteErr: map[string]error{"rev-2": errors.New("delete failed")},
		},
		{
			name: "first deletion fails", revisions: []string{"rev-1", "rev-2"},
			current: "rev-2", deleteErr: map[string]error{"rev-1": errors.New("delete failed")},
		},
		{name: "current failure", currentErr: errors.New("current failed")},
		{
			name: "unlock after deletion", revisions: []string{"rev-1", "rev-2"},
			current: "rev-2", unlockErr: errors.New("unlock failed"),
		},
		{name: "unlock without deletion", unlockErr: errors.New("unlock failed")},
	} {
		store := &stateGCBackend{
			revisions: tc.revisions, current: tc.current, currentErr: tc.currentErr,
			deleteErr: tc.deleteErr, deletes: []string{}, lock: &stateGCLock{err: tc.unlockErr},
		}
		mutation, err := gcStateMetadata(stateMetadata{Store: store, Stack: "dev"}, 0)
		var document *stateGCResult
		if mutation != nil {
			value := buildStateGCResult(
				Info{FactoryName: "appdeploy"}, mutation.Stack, err == nil,
				mutation.Deleted, mutation.Kept, mutation.Current,
				mutation.FailedRevision, stateErrorDiagnostics(err),
			)
			document = &value
		}
		result.Cases = append(result.Cases, stateGCCaseGolden{
			Name: tc.name, Result: document, Error: runnerErrorString(err),
			Diagnostics: stateErrorDiagnostics(err), Deletes: store.deletes,
			UnlockCalls: store.lock.calls,
		})
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/state-gc-operation.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

type stateMutationGolden struct {
	Cases []stateMutationCaseGolden `json:"cases"`
}

type stateMutationCaseGolden struct {
	Name            string                  `json:"name"`
	Result          any                     `json:"result"`
	ResultNonNull   bool                    `json:"result-non-null"`
	RevisionCurrent bool                    `json:"revision-current"`
	Error           string                  `json:"error"`
	Diagnostics     []diagnostic.Diagnostic `json:"diagnostics"`
	CurrentEntries  []string                `json:"current-entries"`
	UnlockCalls     int                     `json:"unlock-calls"`
}

func TestStateMutationOperationGolden(t *testing.T) {
	result := stateMutationGolden{Cases: []stateMutationCaseGolden{
		moveStateAfterCommittedSetError(t),
		removeStateAfterUnlockError(t),
		removeStateBeforeCommitError(t),
	}}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/state-mutation-operation.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func moveStateAfterCommittedSetError(t *testing.T) stateMutationCaseGolden {
	t.Helper()
	store, starting := newStateMutationStore(t, "action.old")
	wrapped := &stateMutationBackend{
		Backend: store, setCurrentErr: errors.New("set current failed"),
		commitBeforeSetError: true,
	}
	from := mustStateOperationRef(t, "action.old")
	to := mustStateOperationRef(t, "action.new")
	dag := &runtime.DAG{
		Nodes: map[string]*runtime.Node{
			"action.new": {
				Address: "action.new", Kind: runtime.NodeAction, Alias: "core",
				LibraryPath: "example.com/core", Type: "record",
			},
		},
		Edges: map[string][]string{"action.new": {}},
	}
	mutation, err := moveStateMetadata(
		stateMetadata{Store: wrapped, Stack: "dev"}, dag, nil, from, to,
	)
	var document any
	if mutation != nil {
		value, buildErr := buildStateMoveResult(
			Info{FactoryName: "appdeploy"}, mutation.Stack, false,
			mutation.From, mutation.To, mutation.Moved, "revision", stateErrorDiagnostics(err),
		)
		require.NoError(t, buildErr)
		document = value
	}
	return stateMutationGoldenCase(
		t, "move after committed set error", wrapped, starting, document, mutation != nil, err,
	)
}

func removeStateAfterUnlockError(t *testing.T) stateMutationCaseGolden {
	t.Helper()
	store, starting := newStateMutationStore(t, "action.old")
	wrapped := &stateMutationBackend{
		Backend: store, unlockErr: errors.New("unlock failed"),
	}
	mutation, err := removeStateMetadata(
		stateMetadata{Store: wrapped, Stack: "dev"},
		mustStateOperationRef(t, "action.old"),
	)
	var document any
	if mutation != nil {
		value, buildErr := buildStateRemoveResult(
			Info{FactoryName: "appdeploy"}, mutation.Stack, false,
			mutation.Address, "revision", stateErrorDiagnostics(err),
		)
		require.NoError(t, buildErr)
		document = value
	}
	return stateMutationGoldenCase(
		t, "remove after unlock error", wrapped, starting, document, mutation != nil, err,
	)
}

func removeStateBeforeCommitError(t *testing.T) stateMutationCaseGolden {
	t.Helper()
	store, starting := newStateMutationStore(t, "action.old")
	wrapped := &stateMutationBackend{
		Backend: store, setCurrentErr: errors.New("set current failed"),
	}
	mutation, err := removeStateMetadata(
		stateMetadata{Store: wrapped, Stack: "dev"},
		mustStateOperationRef(t, "action.old"),
	)
	return stateMutationGoldenCase(
		t, "remove before commit error", wrapped, starting, nil, mutation != nil, err,
	)
}

func stateMutationGoldenCase(
	t *testing.T,
	name string,
	store *stateMutationBackend,
	starting string,
	document any,
	resultNonNull bool,
	err error,
) stateMutationCaseGolden {
	t.Helper()
	current, currentErr := store.CurrentRev()
	require.NoError(t, currentErr)
	snapshot, currentErr := store.Current()
	require.NoError(t, currentErr)
	entries := make([]string, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		entries = append(entries, entry.Address)
	}
	slices.Sort(entries)
	return stateMutationCaseGolden{
		Name: name, Result: document, ResultNonNull: resultNonNull,
		RevisionCurrent: current != starting,
		Error:           runnerErrorString(err), Diagnostics: stateErrorDiagnostics(err),
		CurrentEntries: entries, UnlockCalls: store.unlockCalls,
	}
}

func newStateMutationStore(t *testing.T, address string) (*local.Store, string) {
	t.Helper()
	store, err := local.NewStore(t.TempDir(), "appdeploy", "dev", encrypters.Noop{})
	require.NoError(t, err)
	snapshot := state.NewSnapshot(
		state.FactoryInfo{Name: "appdeploy", Version: "v1", ContentRevision: "content"},
		"dev",
	)
	snapshot.Entries = []*state.Entry{{
		Address: address, Type: state.EntryAction, Category: "action",
		Binding: &state.Binding{
			Alias: "core", LibraryPath: "example.com/core", Export: "record",
		},
		Inputs: map[string]any{}, Outputs: map[string]any{},
	}}
	revision, err := store.Write(snapshot)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(revision))
	return store, revision
}

func mustStateOperationRef(t *testing.T, value string) runtime.EntryRef {
	t.Helper()
	ref, err := runtime.ParseEntryRef(value)
	require.NoError(t, err)
	return ref
}

type stateMutationBackend struct {
	state.Backend
	setCurrentErr        error
	commitBeforeSetError bool
	unlockErr            error
	unlockCalls          int
}

func (b *stateMutationBackend) SetCurrent(revision string) error {
	if b.setCurrentErr == nil {
		return b.Backend.SetCurrent(revision)
	}
	if b.commitBeforeSetError {
		if err := b.Backend.SetCurrent(revision); err != nil {
			return err
		}
	}
	return b.setCurrentErr
}

func (b *stateMutationBackend) Lock(ctx context.Context) (state.Lock, error) {
	lock, err := b.Backend.Lock(ctx)
	if err != nil {
		return nil, err
	}
	return &stateMutationLock{Lock: lock, backend: b}, nil
}

type stateMutationLock struct {
	state.Lock
	backend *stateMutationBackend
}

func (l *stateMutationLock) Unlock() error {
	l.backend.unlockCalls++
	if err := l.Lock.Unlock(); err != nil {
		return err
	}
	return l.backend.unlockErr
}

type stateGCBackend struct {
	state.Backend
	revisions  []string
	current    string
	currentErr error
	deleteErr  map[string]error
	deletes    []string
	lock       *stateGCLock
}

func (b *stateGCBackend) List() ([]string, error) {
	return append([]string{}, b.revisions...), nil
}

func (b *stateGCBackend) CurrentRev() (string, error) {
	if b.currentErr != nil {
		return "", b.currentErr
	}
	if b.current == "" {
		return "", state.ErrNoCurrent
	}
	return b.current, nil
}

func (b *stateGCBackend) Delete(revision string) error {
	b.deletes = append(b.deletes, revision)
	return b.deleteErr[revision]
}

func (b *stateGCBackend) Lock(context.Context) (state.Lock, error) {
	return b.lock, nil
}

type stateGCLock struct {
	err   error
	calls int
}

func (l *stateGCLock) Unlock() error {
	l.calls++
	return l.err
}
