package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime/debug"
	"testing"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/toolchain"
	"github.com/stretchr/testify/require"
)

type versionBoundaryGolden struct {
	Cases []versionBoundaryCaseGolden `json:"cases"`
}

type versionBoundaryCaseGolden struct {
	Name      string   `json:"name"`
	Args      []string `json:"args"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	Error     string   `json:"error"`
	Reported  bool     `json:"reported"`
	ReadCalls int      `json:"read-calls"`
}

func TestVersionFormatBoundaryGolden(t *testing.T) {
	previous := readBuildInfo
	t.Cleanup(func() { readBuildInfo = previous })
	result := versionBoundaryGolden{}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "JSON startup mismatch", args: []string{"version", "--format", "json"}},
		{name: "text startup mismatch", args: []string{"version"}},
		{name: "unsupported format wins", args: []string{"version", "--format", "yaml"}},
	} {
		readCalls := 0
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			readCalls++
			return &debug.BuildInfo{Deps: []*debug.Module{{
				Path: toolchain.UnobinModulePath, Version: "v0.2.0",
			}}}, true
		}
		root := newRootCmd(Info{
			FactoryName: "factory", FactoryVersion: "v1.0.0",
			ContentRevision: "012345abcdef", UnobinVersion: "v0.1.0",
		})
		root.SetArgs(tc.args)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		err := root.Execute()
		cmdout.PrintUnreportedError(root, err)
		result.Cases = append(result.Cases, versionBoundaryCaseGolden{
			Name: tc.name, Args: tc.args,
			Stdout: stdout.String(), Stderr: stderr.String(),
			Error: runnerErrorString(err), Reported: cmdout.IsReported(err), ReadCalls: readCalls,
		})
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/version-format-boundary.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}
