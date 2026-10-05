package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func recordDirectory(t *testing.T, record Record, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o644))
	}
	require.NoError(t, SaveRecord(dir, record))
	return dir
}

func fixtureRunner(_ context.Context, dir string, args []string, output string) (Run, error) {
	body := []byte("Synthetic comparison for artifact tests.\n")
	err := os.WriteFile(filepath.Join(dir, output), body, 0o644)
	start := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	return Run{
		Args: args, Directory: ".", Started: start, Finished: start.Add(time.Second),
		Output: output, Digest: digest(body),
	}, err
}

func TestComparisonArtifactsIncludeBothRawRecordsAndDerivedStatistics(t *testing.T) {
	spec := fixtureSpec()
	before, beforeFiles := fixtureRecord(spec, 100, strings.Repeat("b", 40))
	after, afterFiles := fixtureRecord(spec, 80, strings.Repeat("c", 40))
	beforeDir := recordDirectory(t, before, beforeFiles)
	afterDir := recordDirectory(t, after, afterFiles)
	out := filepath.Join(t.TempDir(), "comparison")
	summary, err := WriteComparison(t.Context(), beforeDir, afterDir, out, fixtureRunner)
	require.NoError(t, err)
	checked, err := ValidateComparison(t.Context(), out, fixtureRunner)
	require.NoError(t, err)
	require.Equal(t, summary, checked)
	for _, name := range []string{
		"metadata.json", "before.txt", "after.txt", "comparison.txt", "summary.json",
		"before-test.txt", "after-test.txt", "before-warmup.txt", "after-warmup.txt",
	} {
		require.FileExists(t, filepath.Join(out, name))
	}
	_, err = WriteComparison(t.Context(), beforeDir, afterDir, out, fixtureRunner)
	require.Error(t, err)
}

func TestComparisonValidationRejectsAlteredStatisticsAndToolOutput(t *testing.T) {
	fields := []string{"statistics", "comparison", "unknown metadata field", "missing side"}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			spec := fixtureSpec()
			before, beforeFiles := fixtureRecord(spec, 100, strings.Repeat("b", 40))
			after, afterFiles := fixtureRecord(spec, 80, strings.Repeat("c", 40))
			out := filepath.Join(t.TempDir(), "comparison")
			_, err := WriteComparison(t.Context(), recordDirectory(t, before, beforeFiles),
				recordDirectory(t, after, afterFiles), out, fixtureRunner)
			require.NoError(t, err)
			path := filepath.Join(out, "metadata.json")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			var metadata Comparison
			require.NoError(t, json.Unmarshal(body, &metadata))
			switch field {
			case "statistics":
				path := filepath.Join(out, "summary.json")
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				body = []byte(strings.ReplaceAll(string(body), `"improved"`, `"fabricated"`))
				require.NoError(t, os.WriteFile(path, body, 0o644))
				metadata.SummaryDigest = digest(body)
			case "comparison":
				body := []byte("fabricated comparison")
				require.NoError(t, os.WriteFile(filepath.Join(out, "comparison.txt"), body, 0o644))
				metadata.Comparison.Digest = digest(body)
			case "unknown metadata field":
				body = []byte(strings.Replace(string(body), "{", `{"unknown":true,`, 1))
				require.NoError(t, os.WriteFile(path, body, 0o644))
			case "missing side":
				require.NoError(t, os.Remove(filepath.Join(out, "after.txt")))
			}
			if field == "statistics" || field == "comparison" {
				body, err = json.MarshalIndent(metadata, "", "  ")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o644))
			}
			_, err = ValidateComparison(t.Context(), out, fixtureRunner)
			require.Error(t, err)
		})
	}
}

func TestComparisonToolFailureCannotValidate(t *testing.T) {
	spec := fixtureSpec()
	before, beforeFiles := fixtureRecord(spec, 100, strings.Repeat("b", 40))
	after, afterFiles := fixtureRecord(spec, 80, strings.Repeat("c", 40))
	out := filepath.Join(t.TempDir(), "comparison")
	cause := errors.New("comparison tool failed")
	failure := func(ctx context.Context, dir string, args []string, output string) (Run, error) {
		run, err := fixtureRunner(ctx, dir, args, output)
		run.ExitCode = 1
		return run, errors.Join(cause, err)
	}
	_, err := WriteComparison(t.Context(), recordDirectory(t, before, beforeFiles),
		recordDirectory(t, after, afterFiles), out, failure)
	require.ErrorIs(t, err, cause)
	_, err = ValidateComparison(t.Context(), out, fixtureRunner)
	require.Error(t, err)
}

func TestRecordMetadataCannotOverwriteAnExistingRecord(t *testing.T) {
	record, files := fixtureRecord(fixtureSpec(), 100, strings.Repeat("b", 40))
	dir := recordDirectory(t, record, files)
	_, _, err := LoadRecord(dir)
	require.NoError(t, err)
	require.Error(t, SaveRecord(dir, record))
}
