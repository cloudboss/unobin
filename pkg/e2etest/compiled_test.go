package e2etest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldPinStack(t *testing.T) {
	pinned := map[string]bool{"stacks/pinned.ub": true}
	tests := []struct {
		name      string
		cmd       Command
		stackPath string
		want      bool
	}{
		{
			name:      "pins first stack command",
			cmd:       Command{Name: "validate", Args: []string{"validate", "-c", "stacks/dev.ub"}},
			stackPath: "stacks/dev.ub",
			want:      true,
		},
		{
			name:      "skips explicit pin command",
			cmd:       Command{Name: "pin", Args: []string{"pin", "-c", "stacks/dev.ub"}},
			stackPath: "stacks/dev.ub",
		},
		{
			name: "skips command opt out",
			cmd: Command{
				Name:    "validate",
				Args:    []string{"validate", "-c", "stacks/dev.ub"},
				SkipPin: true,
			},
			stackPath: "stacks/dev.ub",
		},
		{
			name:      "skips existing stack pin",
			cmd:       Command{Name: "validate", Args: []string{"validate", "-c", "stacks/pinned.ub"}},
			stackPath: "stacks/pinned.ub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldPinStack(tt.cmd, tt.stackPath, pinned))
		})
	}
}
