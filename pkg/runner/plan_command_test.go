package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

type planWriterFailureGolden struct {
	Error          string `json:"error"`
	Reported       bool   `json:"reported"`
	Stdout         string `json:"stdout"`
	ArtifactExists bool   `json:"artifact-exists"`
	ArtifactStack  string `json:"artifact-stack"`
}

func TestPlanWriterFailureGolden(t *testing.T) {
	factory := ubtest.ReadValidFixture(t, "testdata/ub/plan-command", "factory")
	stackSource := ubtest.ReadValidFixture(t, "testdata/ub/plan-command", "stack")
	want, err := os.ReadFile("testdata/plan-writer-failure.json")
	require.NoError(t, err)
	info := testInfo(t, factory)
	stack, err := parseStackSource("dev.ub", []byte(stackSource))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir("artifacts", 0o755))

	writer := &planFailureWriter{}
	command := &cobra.Command{Use: "factory"}
	command.SetOut(writer)
	artifactPath := filepath.Join("artifacts", "dev.ubp")
	err = doPlanWithFormat(
		command, info, stack, "dev.ub", artifactPath, 0, false, false,
		cmdout.FormatJSON, nil,
	)
	sealed, readErr := os.ReadFile(artifactPath)
	artifactExists := readErr == nil
	var artifactStack string
	if readErr == nil {
		resolveEncrypter := func(*runtime.StateRef) (sdkencrypt.Encrypter, error) {
			return encrypters.Noop{}, nil
		}
		plan, openErr := runtime.OpenPlan(sealed, resolveEncrypter)
		if openErr == nil {
			artifactStack = plan.Stack
		}
	}
	view := planWriterFailureGolden{
		Error:          runnerErrorString(err),
		Reported:       cmdout.IsReported(err),
		Stdout:         writer.String(),
		ArtifactExists: artifactExists,
		ArtifactStack:  artifactStack,
	}
	got, err := json.MarshalIndent(view, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	require.Equal(t, string(want), string(got))
}

type planFailureWriter struct {
	bytes.Buffer
}

func (w *planFailureWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer failed")
}
