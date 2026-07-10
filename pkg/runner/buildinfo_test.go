package runner

import (
	"encoding/json"
	"os"
	"runtime/debug"
	"testing"

	"github.com/cloudboss/unobin/pkg/toolchain"
	"github.com/stretchr/testify/require"
)

type buildInfoGolden struct {
	Decisions []buildInfoCaseGolden `json:"decisions"`
	Statuses  []buildInfoCaseGolden `json:"statuses"`
}

type buildInfoCaseGolden struct {
	Name      string `json:"name"`
	Notice    string `json:"notice"`
	Error     string `json:"error"`
	ReadCalls int    `json:"read-calls,omitempty"`
}

func TestLinkedUnobinStatusGolden(t *testing.T) {
	linked := func(version string, replace *debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{Deps: []*debug.Module{
			{Path: "github.com/other/lib", Version: "v3.0.0"},
			{Path: toolchain.UnobinModulePath, Version: version, Replace: replace},
		}}
	}
	decisionCases := []struct {
		name     string
		info     *debug.BuildInfo
		expected string
	}{
		{name: "linked version matches", info: linked("v0.1.0", nil), expected: "v0.1.0"},
		{
			name:     "replaced module proceeds with a notice",
			info:     linked("v0.1.0", &debug.Module{Path: "/home/dev/unobin"}),
			expected: "v0.1.0",
		},
		{name: "mismatch is refused", info: linked("v0.2.0", nil), expected: "v0.1.0"},
		{
			name:     "unobin absent checks nothing",
			info:     &debug.BuildInfo{Deps: []*debug.Module{{Path: "github.com/other/lib"}}},
			expected: "v0.1.0",
		},
	}
	result := buildInfoGolden{}
	for _, tc := range decisionCases {
		notice, err := decideLinkedUnobin(tc.info, tc.expected)
		result.Decisions = append(result.Decisions, buildInfoCaseGolden{
			Name: tc.name, Notice: notice, Error: runnerErrorString(err),
		})
	}

	previous := readBuildInfo
	t.Cleanup(func() { readBuildInfo = previous })
	statusCases := []struct {
		name     string
		expected string
		info     *debug.BuildInfo
		readable bool
	}{
		{name: "unstamped skips build info", readable: true},
		{name: "build info unavailable", expected: "v0.1.0"},
		{
			name:     "replacement status",
			expected: "v0.1.0",
			info:     linked("v0.1.0", &debug.Module{Path: "/home/dev/unobin"}),
			readable: true,
		},
		{
			name:     "mismatch status",
			expected: "v0.1.0",
			info:     linked("v0.2.0", nil),
			readable: true,
		},
	}
	for _, tc := range statusCases {
		readCalls := 0
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			readCalls++
			return tc.info, tc.readable
		}
		notice, err := linkedUnobinStatus(tc.expected)
		result.Statuses = append(result.Statuses, buildInfoCaseGolden{
			Name: tc.name, Notice: notice, Error: runnerErrorString(err), ReadCalls: readCalls,
		})
	}

	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/buildinfo.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func runnerErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
