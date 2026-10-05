package evidence

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func collectionRepository(t *testing.T) (string, Spec, string) {
	t.Helper()
	repository := t.TempDir()
	require.NoError(t, os.CopyFS(repository, os.DirFS("testdata/repository")))
	spec := fixtureSpec()
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		command := exec.Command("git", args...)
		command.Dir = repository
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	require.NoError(t, saveJSON(filepath.Join(repository, "workload.json"), spec))
	command := exec.Command("git", "add", "workload.json")
	command.Dir = repository
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	command = exec.Command("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com",
		"commit", "-qm", "Create collection fixture")
	command.Dir = repository
	output, err = command.CombinedOutput()
	require.NoError(t, err, string(output))
	command = exec.Command("git", "rev-parse", "HEAD")
	command.Dir = repository
	output, err = command.Output()
	require.NoError(t, err)
	return repository, spec, strings.TrimSpace(string(output))
}

func TestCollectionRefusesDirtySourceAndChangedFixtures(t *testing.T) {
	problems := []string{"tracked edit", "untracked source", "different fixture revision"}
	for _, problem := range problems {
		t.Run(problem, func(t *testing.T) {
			repository, _, revision := collectionRepository(t)
			options := CollectionOptions{
				Repository: repository, SpecPath: "workload.json",
				OutDir: filepath.Join(repository, "_output", "record"), FixtureRevision: revision,
			}
			path := "pkg/runtime/work.go"
			if problem == "untracked source" {
				path = "pkg/runtime/new.go"
			}
			require.NoError(t, os.WriteFile(filepath.Join(repository, path), []byte("package runtime\n"),
				0o644))
			if problem == "different fixture revision" {
				path = "pkg/runtime/index_benchmark_test.go"
				body, err := os.ReadFile(filepath.Join(repository, path))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(repository, path),
					append(body, []byte("\n// Changed benchmark fixture.\n")...), 0o644))
				command := exec.Command("git", "-c", "user.name=Fixture",
					"-c", "user.email=fixture@example.com", "commit", "-qam", "Change fixture")
				command.Dir = repository
				output, err := command.CombinedOutput()
				require.NoError(t, err, string(output))
			}
			_, err := Collect(t.Context(), options)
			require.Error(t, err)
			require.NoDirExists(t, options.OutDir)
		})
	}
}

func TestAuditRecordChecksCommittedFixtureAndDependencyContent(t *testing.T) {
	repository, spec, revision := collectionRepository(t)
	record, _ := fixtureRecord(spec, 100, revision)
	record.FixtureRevision = revision
	record.SpecPath = "workload.json"
	record.Fixtures = map[string]string{}
	for _, path := range append(slices.Clone(spec.Fixtures), record.SpecPath) {
		body, err := os.ReadFile(filepath.Join(repository, path))
		require.NoError(t, err)
		record.Fixtures[path] = contentDigest(body)
	}
	module, err := os.ReadFile(filepath.Join(repository, "go.mod"))
	require.NoError(t, err)
	record.Dependencies = map[string]string{"go.mod": contentDigest(module)}
	require.NoError(t, AuditRecord(t.Context(), repository, record))
	record.Fixtures[spec.Fixtures[0]] = strings.Repeat("f", 64)
	require.Error(t, AuditRecord(t.Context(), repository, record))
}

func TestAuditRejectsChangedWorkloadDeclarationAndIncompleteFixtures(t *testing.T) {
	for _, change := range []string{"description", "fixtures"} {
		t.Run(change, func(t *testing.T) {
			repository, spec, revision := collectionRepository(t)
			record, _ := fixtureRecord(spec, 100, revision)
			record.FixtureRevision = revision
			record.SpecPath = "workload.json"
			record.Fixtures = map[string]string{}
			for _, path := range append(slices.Clone(spec.Fixtures), record.SpecPath) {
				body, err := os.ReadFile(filepath.Join(repository, path))
				require.NoError(t, err)
				record.Fixtures[path] = contentDigest(body)
			}
			module, err := os.ReadFile(filepath.Join(repository, "go.mod"))
			require.NoError(t, err)
			record.Dependencies = map[string]string{"go.mod": contentDigest(module)}
			if change == "description" {
				record.Spec.Description = "Changed metadata declaration"
			} else {
				delete(record.Fixtures, spec.Fixtures[0])
			}
			require.Error(t, AuditRecord(t.Context(), repository, record))
		})
	}
}

func TestCommandRunnerRecordsFailureAndCancellation(t *testing.T) {
	dir := t.TempDir()
	runner := CommandRunner()
	run, err := runner(t.Context(), dir, []string{"go", "test", "./missing"}, "failure.txt")
	require.Error(t, err)
	require.NotZero(t, run.ExitCode)
	require.FileExists(t, filepath.Join(dir, "failure.txt"))
	body, err := os.ReadFile(filepath.Join(dir, "failure.txt"))
	require.NoError(t, err)
	require.Equal(t, contentDigest(body), run.Digest)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runner(ctx, dir, []string{"go", "version"}, "cancelled.txt")
	require.ErrorIs(t, err, context.Canceled)
	_, err = runner(t.Context(), dir, []string{"go", "version"}, "failure.txt")
	require.Error(t, err)
}

func TestCollectRetainsFailedRunsAndDetectsSourceMutation(t *testing.T) {
	repository, _, _ := collectionRepository(t)
	require.NoError(t, os.CopyFS(filepath.Join(repository, "pkg", "runtime"),
		os.DirFS("testdata/mutating-test")))
	command := exec.Command("git", "add", "pkg/runtime/mutation_test.go")
	command.Dir = repository
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	command = exec.Command("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com",
		"commit", "-qam", "Add failing collection fixture")
	command.Dir = repository
	output, err = command.CombinedOutput()
	require.NoError(t, err, string(output))
	out := filepath.Join(repository, "_output", "failed")
	record, err := Collect(t.Context(), CollectionOptions{
		Repository: repository, SpecPath: "workload.json", OutDir: out,
	})
	require.ErrorContains(t, err, "tracked source must be clean")
	require.False(t, record.CleanSource)
	require.Len(t, record.Tests, 1)
	require.NotZero(t, record.Tests[0].ExitCode)
	require.NotEmpty(t, record.Tests[0].Error)
	body, err := os.ReadFile(filepath.Join(out, "test-01.txt"))
	require.NoError(t, err)
	require.Contains(t, string(body), "Intentional collection failure")
	require.Equal(t, contentDigest(body), record.Tests[0].Digest)
	var stored Record
	require.NoError(t, readJSON(filepath.Join(out, "metadata.json"), &stored))
	require.Equal(t, record, stored)
	require.NoFileExists(t, filepath.Join(out, "warmup.txt"))
}

func TestCollectRunsDeclaredCommandsAndProducesAuditableRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("collects ten two-second samples at CPU settings 1,4")
	}
	repository, _, revision := collectionRepository(t)
	record, err := Collect(t.Context(), CollectionOptions{
		Repository: repository, SpecPath: "workload.json",
		OutDir: filepath.Join(repository, "_output", "record"),
	})
	require.NoError(t, err)
	require.Equal(t, revision, record.Revision)
	require.Equal(t, revision, record.FixtureRevision)
	stored, files, err := LoadRecord(filepath.Join(repository, "_output", "record"))
	require.NoError(t, err)
	require.Equal(t, record, stored)
	_, err = Validate(stored, files)
	require.NoError(t, err)
	require.NoError(t, AuditRecord(t.Context(), repository, stored))
}

func TestBenchstatRunnerUsesPinnedTool(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the pinned comparison tool")
	}
	toolDir, err := filepath.Abs("../..")
	require.NoError(t, err)
	runner, err := BenchstatRunner(t.Context(), toolDir)
	require.NoError(t, err)
	before, left := fixtureRecord(fixtureSpec(), 100, strings.Repeat("b", 40))
	after, right := fixtureRecord(fixtureSpec(), 80, strings.Repeat("c", 40))
	out := filepath.Join(t.TempDir(), "comparison")
	_, err = WriteComparison(t.Context(), recordDirectory(t, before, left),
		recordDirectory(t, after, right), out, runner)
	require.NoError(t, err)
	_, err = ValidateComparison(t.Context(), out, runner)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(out, "comparison.txt"))
	require.NoError(t, err)
	require.Contains(t, string(body), "before.txt")
	require.Contains(t, string(body), "after.txt")
	_, err = BenchstatRunner(t.Context(), t.TempDir())
	require.Error(t, err)
	require.False(t, errors.Is(err, context.Canceled))
}
