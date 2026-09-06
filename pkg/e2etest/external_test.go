package e2etest

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExternalLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: builds an external test module and factory")
	}
	dir := copyCaseToWorkspace(t, "testdata/ub/external/valid")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return string(output)
	}
	run("mod", "edit", "-replace=github.com/cloudboss/unobin="+e2eRepoRoot(t))
	short := run("test", "-mod=mod", "-trimpath", "-short", "-v", "-count=1", ".")
	require.Contains(t, short, "--- SKIP: TestLibrary/external")
	require.NotContains(t, short, "compile start")
	full := run("test", "-mod=mod", "-trimpath", "-v", "-count=1", ".")
	require.Contains(t, full, "--- PASS: TestLibrary/external")
}
