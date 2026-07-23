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
		"emptyDirectories": ["src/assets/empty"],
		"removeAfterCompile": ["src/assets"],
		"assetBundle": { "setCount": 1, "blobCount": 4 },
		"assetCache": { "path": "cache/apply", "referenceCount": 4 },
		"assetIdentity": {
			"asset": "tree",
			"stableEntry": "main.go",
			"changePath": "src/assets/internal/helpers.go",
			"replacementPath": "mutations/helpers.go"
		},
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
		"fileExclusions": [
			{ "path": "work/events.ndjson", "text": ["secret", "/tmp/source"] }
		],
		"planSummaries": [
			{
				"path": "work/plan.ubp",
				"want": "want/plan-summary.json",
				"includeInputs": true
			}
		],
		"planEnvelopes": [
			{ "path": "work/plan.ubp", "want": "want/plan-envelope.json" }
		],
		"stateEnvelopes": [
			{ "stack": "dev", "want": "want/state-envelope.json" }
		],
		"absentFiles": ["work/lock"],
		"stateSummary": "want/state-summary.json",
		"stateSeed": "seed/state.json",
		"extraStateSnapshots": 2,
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
	assert.Equal(t, []string{"src/assets/empty"}, beta.EmptyDirectories)
	assert.Equal(t, []string{"src/assets"}, beta.RemoveAfterCompile)
	require.NotNil(t, beta.AssetBundle)
	assert.Equal(t, 1, beta.AssetBundle.SetCount)
	assert.Equal(t, 4, beta.AssetBundle.BlobCount)
	require.NotNil(t, beta.AssetCache)
	assert.Equal(t, "cache/apply", beta.AssetCache.Path)
	assert.Equal(t, 4, beta.AssetCache.ReferenceCount)
	require.NotNil(t, beta.AssetIdentity)
	assert.Equal(t, "tree", beta.AssetIdentity.Asset)
	assert.Equal(t, "main.go", beta.AssetIdentity.StableEntry)
	assert.Equal(t, "src/assets/internal/helpers.go", beta.AssetIdentity.ChangePath)
	assert.Equal(t, "mutations/helpers.go", beta.AssetIdentity.ReplacementPath)
	require.Len(t, beta.Files, 1)
	assert.Equal(t, "work/events.ndjson", beta.Files[0].Path)
	assert.Equal(t, "want/events.ndjson", beta.Files[0].Want)
	require.Len(t, beta.FileExclusions, 1)
	assert.Equal(t, "work/events.ndjson", beta.FileExclusions[0].Path)
	assert.Equal(t, []string{"secret", "/tmp/source"}, beta.FileExclusions[0].Text)
	require.Len(t, beta.PlanSummaries, 1)
	assert.Equal(t, "work/plan.ubp", beta.PlanSummaries[0].Path)
	assert.Equal(t, "want/plan-summary.json", beta.PlanSummaries[0].Want)
	assert.True(t, beta.PlanSummaries[0].IncludeInputs)
	require.Len(t, beta.PlanEnvelopes, 1)
	assert.Equal(t, "work/plan.ubp", beta.PlanEnvelopes[0].Path)
	assert.Equal(t, "want/plan-envelope.json", beta.PlanEnvelopes[0].Want)
	require.Len(t, beta.StateEnvelopes, 1)
	assert.Equal(t, "dev", beta.StateEnvelopes[0].Stack)
	assert.Equal(t, "want/state-envelope.json", beta.StateEnvelopes[0].Want)
	assert.Equal(t, "want/state-summary.json", beta.StateSummary)
	assert.Equal(t, []string{"work/lock"}, beta.AbsentFiles)
	assert.Equal(t, "seed/state.json", beta.StateSeed)
	assert.Equal(t, 2, beta.ExtraStateSnapshots)
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
			{ "path": "root/project.ub", "want": "want/project.ub" }
		],
		"fileExclusions": [
			{ "path": "root/project.ub", "text": ["secret", "/tmp/source"] }
		],
		"absentFiles": ["root/services/app/project-lock.ub"]
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
	assert.Equal(t, "root/project.ub", got.Files[0].Path)
	require.Len(t, got.FileExclusions, 1)
	assert.Equal(t, "root/project.ub", got.FileExclusions[0].Path)
	assert.Equal(t, []string{"secret", "/tmp/source"}, got.FileExclusions[0].Text)
	assert.Equal(t, []string{"root/services/app/project-lock.ub"}, got.AbsentFiles)
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
		{
			name: "empty exclusion path",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"fileExclusions": [
					{ "text": ["secret"] }
				]
			}`,
			want: "fileExclusions[0].path is required",
		},
		{
			name: "parent exclusion path",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"fileExclusions": [
					{ "path": "../generated.go", "text": ["secret"] }
				]
			}`,
			want: "fileExclusions[0].path must stay under the case directory",
		},
		{
			name: "empty excluded text",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"fileExclusions": [
					{ "path": "generated.go", "text": [""] }
				]
			}`,
			want: "fileExclusions[0].text[0] is required",
		},
		{
			name: "parent empty directory",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"emptyDirectories": ["../empty"]
			}`,
			want: "emptyDirectories[0] must stay under the case directory",
		},
		{
			name: "empty removal path",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"removeAfterCompile": [""]
			}`,
			want: "removeAfterCompile[0] is required",
		},
		{
			name: "case directory removal",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"removeAfterCompile": ["assets/.."]
			}`,
			want: "removeAfterCompile[0] must name a path under the case directory",
		},
		{
			name: "invalid bundle count",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"assetBundle": { "setCount": 0, "blobCount": 1 }
			}`,
			want: "assetBundle.setCount must be positive",
		},
		{
			name: "missing identity entry",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"assetIdentity": {
					"asset": "tree",
					"changePath": "src/helpers.go",
					"replacementPath": "mutations/helpers.go"
				}
			}`,
			want: "assetIdentity.stableEntry is required",
		},
		{
			name: "invalid cache count",
			content: `{
				"name": "bad",
				"factoryPath": "src",
				"assetCache": { "path": "cache", "referenceCount": 0 }
			}`,
			want: "assetCache.referenceCount must be positive",
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
