package runner

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/toolchain"
)

func TestDecideLinkedUnobin(t *testing.T) {
	linked := func(version string, replace *debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{
			Deps: []*debug.Module{
				{Path: "github.com/other/lib", Version: "v3.0.0"},
				{Path: toolchain.UnobinModulePath, Version: version, Replace: replace},
			},
		}
	}
	tests := []struct {
		name       string
		info       *debug.BuildInfo
		expected   string
		wantNotice string
		wantErr    string
	}{
		{
			name:     "linked version matches",
			info:     linked("v0.1.0", nil),
			expected: "v0.1.0",
		},
		{
			name:       "replaced module proceeds with a notice",
			info:       linked("v0.1.0", &debug.Module{Path: "/home/dev/unobin"}),
			expected:   "v0.1.0",
			wantNotice: "/home/dev/unobin",
		},
		{
			name:     "mismatch is refused",
			info:     linked("v0.2.0", nil),
			expected: "v0.1.0",
			wantErr:  "compiled against github.com/cloudboss/unobin v0.1.0 but links v0.2.0",
		},
		{
			name:     "unobin absent from the dependency list checks nothing",
			info:     &debug.BuildInfo{Deps: []*debug.Module{{Path: "github.com/other/lib"}}},
			expected: "v0.1.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notice, err := decideLinkedUnobin(tt.info, tt.expected)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantNotice == "" {
				require.Empty(t, notice)
			} else {
				require.Contains(t, notice, tt.wantNotice)
			}
		})
	}
}

// TestVerifyLinkedUnobinUnstamped proves a binary with no stamped
// expectation, one built outside the CLI, runs without any check.
func TestVerifyLinkedUnobinUnstamped(t *testing.T) {
	prev := readBuildInfo
	t.Cleanup(func() { readBuildInfo = prev })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("an unstamped binary must not read build info")
		return nil, false
	}
	require.NoError(t, verifyLinkedUnobin(""))
}
