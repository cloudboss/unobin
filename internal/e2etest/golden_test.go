package e2etest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoldenUpdateWritesOutputs(t *testing.T) {
	withUpdate(t, true)
	dir := t.TempDir()
	cmd := Command{
		Name:   "plan",
		Stdout: "want/plan.stdout",
		Stderr: "want/plan.stderr",
	}
	result := CommandResult{
		Stdout: "plan output\n",
		Stderr: "plan notes\n",
	}
	files := []FileCheck{{Path: "work/events.ndjson", Want: "want/events.ndjson"}}

	require.NoError(t, compareCommandGoldens(dir, cmd, result, *update))
	require.NoError(t, compareFileGoldens(dir, files, map[string]string{
		"work/events.ndjson": "event\n",
	}, *update))

	assertFileContent(t, filepath.Join(dir, "want/plan.stdout"), "plan output\n")
	assertFileContent(t, filepath.Join(dir, "want/plan.stderr"), "plan notes\n")
	assertFileContent(t, filepath.Join(dir, "want/events.ndjson"), "event\n")
}

func TestGoldenUpdateRemovesEmptyOutputs(t *testing.T) {
	withUpdate(t, true)
	dir := t.TempDir()
	for _, rel := range []string{
		"want/validate.stdout",
		"want/validate.stderr",
		"want/events.ndjson",
	} {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
	}

	cmd := Command{
		Name:   "validate",
		Stdout: "want/validate.stdout",
		Stderr: "want/validate.stderr",
	}
	files := []FileCheck{{Path: "work/events.ndjson", Want: "want/events.ndjson"}}

	require.NoError(t, compareCommandGoldens(dir, cmd, CommandResult{}, *update))
	require.NoError(t, compareFileGoldens(dir, files, map[string]string{
		"work/events.ndjson": "",
	}, *update))

	assert.NoFileExists(t, filepath.Join(dir, "want/validate.stdout"))
	assert.NoFileExists(t, filepath.Join(dir, "want/validate.stderr"))
	assert.NoFileExists(t, filepath.Join(dir, "want/events.ndjson"))
}

func TestGoldenCompareRequiresWholeContent(t *testing.T) {
	withUpdate(t, false)
	dir := t.TempDir()
	path := filepath.Join(dir, "want/stdout")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("hello\n"), 0o644))

	err := compareTextGolden(path, "hello", *update)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs from golden")
}

func TestCompareCommandResultsCatchesChangedSecondRun(t *testing.T) {
	err := compareCommandResults(
		CommandResult{Stdout: "first\n", ExitCode: 0},
		CommandResult{Stdout: "second\n", ExitCode: 0},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdout is not deterministic")
}

func withUpdate(t *testing.T, value bool) {
	t.Helper()
	old := *update
	*update = value
	t.Cleanup(func() { *update = old })
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, string(got))
}
