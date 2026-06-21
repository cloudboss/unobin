package e2etest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverCases(t *testing.T) {
	dir := t.TempDir()
	writeCaseFile(t, dir, "beta", `{
		"name": "beta",
		"factoryPath": "src",
		"libraryPath": "example.com/unobin/e2e/beta",
		"build": true,
		"commands": [
			{
				"name": "validate",
				"args": ["validate", "-c", "stacks/dev.ub"],
				"stdout": "want/validate.stdout",
				"stderr": "want/validate.stderr"
			}
		],
		"files": [
			{ "path": "work/events.ndjson", "want": "want/events.ndjson" }
		],
		"planSummaries": [
			{ "path": "work/plan.ubp", "want": "want/plan-summary.json" }
		],
		"absentFiles": ["work/lock"],
		"stateSummary": "want/state-summary.json",
		"stateSeed": "seed/state.json",
		"stateLocks": ["dev"],
		"deterministic": true
	}`)
	writeCaseFile(t, dir, "alpha", `{
		"name": "alpha",
		"factoryPath": "src",
		"commands": [
			{ "name": "version", "args": ["version"], "stdout": "want/version.stdout" }
		]
	}`)

	cases, err := DiscoverCompiledCases(dir)
	require.NoError(t, err)
	require.Len(t, cases, 2)

	assert.Equal(t, []string{"alpha", "beta"}, []string{cases[0].Name, cases[1].Name})
	assert.Equal(t, filepath.Join(dir, "alpha"), cases[0].Dir)
	assert.Equal(t, "src", cases[0].FactoryPath)
	assert.Equal(t, "version", cases[0].Commands[0].Name)
	assert.Equal(t, []string{"version"}, cases[0].Commands[0].Args)
	assert.Equal(t, "want/version.stdout", cases[0].Commands[0].Stdout)

	beta := cases[1]
	assert.Equal(t, "example.com/unobin/e2e/beta", beta.LibraryPath)
	assert.True(t, beta.Build)
	assert.True(t, beta.Deterministic)
	require.Len(t, beta.Files, 1)
	assert.Equal(t, "work/events.ndjson", beta.Files[0].Path)
	assert.Equal(t, "want/events.ndjson", beta.Files[0].Want)
	require.Len(t, beta.PlanSummaries, 1)
	assert.Equal(t, "work/plan.ubp", beta.PlanSummaries[0].Path)
	assert.Equal(t, "want/plan-summary.json", beta.PlanSummaries[0].Want)
	assert.Equal(t, "want/state-summary.json", beta.StateSummary)
	assert.Equal(t, []string{"work/lock"}, beta.AbsentFiles)
	assert.Equal(t, "seed/state.json", beta.StateSeed)
	assert.Equal(t, []string{"dev"}, beta.StateLocks)
}

func TestDiscoverSourceCases(t *testing.T) {
	dir := t.TempDir()
	writeCaseFile(t, dir, "deps-sync", `{
		"name": "deps-sync",
		"rootPath": "root",
		"executor": "root",
		"build": true,
		"tags": { "github.com/x/lib": ["v1.0.0"] },
		"remotes": [
			{
				"key": "github.com/x/lib@v1.0.0",
				"path": "remotes/lib",
				"commit": "abc123"
			}
		],
		"commands": [
			{ "name": "sync", "args": ["deps", "sync"], "stdout": "want/stdout" }
		],
		"files": [
			{ "path": "root/manifest.ub", "want": "want/manifest.ub" }
		],
		"absentFiles": ["root/services/app/lock.ub"]
	}`)

	cases, err := DiscoverSourceCases(dir)
	require.NoError(t, err)
	require.Len(t, cases, 1)

	got := cases[0]
	assert.Equal(t, "deps-sync", got.Name)
	assert.Equal(t, filepath.Join(dir, "deps-sync"), got.Dir)
	assert.Equal(t, "root", got.RootPath)
	assert.Equal(t, "root", got.Executor)
	assert.True(t, got.Build)
	require.Len(t, got.Remotes, 1)
	assert.Equal(t, "github.com/x/lib@v1.0.0", got.Remotes[0].Key)
	assert.Equal(t, "remotes/lib", got.Remotes[0].Path)
	assert.Equal(t, "abc123", got.Remotes[0].Commit)
	assert.Equal(t, []string{"v1.0.0"}, got.Tags["github.com/x/lib"])
	assert.Equal(t, "sync", got.Commands[0].Name)
	assert.Equal(t, "root/manifest.ub", got.Files[0].Path)
	assert.Equal(t, []string{"root/services/app/lock.ub"}, got.AbsentFiles)
}

func TestDiscoverCasesRejectsBadPaths(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "absolute",
			content: `{
				"name": "bad",
				"factoryPath": "/tmp/src"
			}`,
			want: "factoryPath must be relative",
		},
		{
			name: "parent",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"commands": [
					{ "name": "validate", "stdout": "../want/stdout" }
				]
			}`,
			want: "commands[0].stdout must stay under the case directory",
		},
		{
			name: "empty command name",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"commands": [
					{ "args": ["version"] }
				]
			}`,
			want: "commands[0].name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCaseFile(t, dir, "bad", tt.content)

			_, err := DiscoverCompiledCases(dir)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDiscoverCasesRejectsEmptyDirs(t *testing.T) {
	_, err := DiscoverCompiledCases(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cases")
}

func writeCaseFile(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case.json"), []byte(content), 0o644))
}
