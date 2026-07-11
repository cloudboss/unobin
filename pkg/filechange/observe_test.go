package filechange

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type observeGolden struct {
	Cases []observeCaseGolden `json:"cases"`
}

type observeCaseGolden struct {
	Name         string   `json:"name"`
	Changes      []Change `json:"changes"`
	Error        string   `json:"error"`
	MutationRuns int      `json:"mutation-runs"`
}

func TestObserveGolden(t *testing.T) {
	result := observeGolden{Cases: []observeCaseGolden{
		observeCreatedFiles(t),
		observeUpdatedAndRemovedFiles(t),
		observeUnchangedAndAbsentFiles(t),
		observePartialFailure(t),
		observeFailureWithoutChanges(t),
	}}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/observe.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func observeCreatedFiles(t *testing.T) observeCaseGolden {
	t.Helper()
	dir := t.TempDir()
	return runObserveCase(t, "created files", dir, []string{"a.txt", "b.txt"}, func() error {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644))
		return nil
	})
}

func observeUpdatedAndRemovedFiles(t *testing.T) observeCaseGolden {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("old"), 0o644))
	return runObserveCase(
		t, "updated and removed files", dir, []string{"b.txt", "a.txt"}, func() error {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("new"), 0o644))
			require.NoError(t, os.Remove(filepath.Join(dir, "b.txt")))
			return nil
		},
	)
}

func observeUnchangedAndAbsentFiles(t *testing.T) observeCaseGolden {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("same"), 0o644))
	return runObserveCase(t, "unchanged and absent files", dir,
		[]string{"missing.txt", "a.txt"}, func() error { return nil })
}

func observePartialFailure(t *testing.T) observeCaseGolden {
	t.Helper()
	dir := t.TempDir()
	return runObserveCase(t, "partial failure", dir, []string{"a.txt"}, func() error {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
		return errors.New("mutation failed")
	})
}

func observeFailureWithoutChanges(t *testing.T) observeCaseGolden {
	t.Helper()
	return runObserveCase(t, "failure without changes", t.TempDir(),
		[]string{"missing.txt"}, func() error { return errors.New("mutation failed") })
}

func runObserveCase(
	t *testing.T,
	name string,
	dir string,
	paths []string,
	mutate func() error,
) observeCaseGolden {
	t.Helper()
	absPaths := make([]string, len(paths))
	for index, path := range paths {
		absPaths[index] = filepath.Join(dir, path)
	}
	runs := 0
	changes, err := Observe(absPaths, func() error {
		runs++
		return mutate()
	})
	for index := range changes {
		changes[index].Path = filepath.Base(changes[index].Path)
	}
	return observeCaseGolden{
		Name: name, Changes: changes, Error: filechangeErrorString(err), MutationRuns: runs,
	}
}

func filechangeErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
