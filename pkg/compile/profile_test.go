package compile

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestUnknownBuildProfileDoesNotWriteOutput(t *testing.T) {
	root := compileResultRoot(t)
	options := compileResultOptions(root)
	options.BuildProfile = "custom"
	result, err := RunResult(options)
	require.Nil(t, result)
	require.EqualError(t, err, "compile: unknown build profile \"custom\" (want full or local)")
	_, err = os.Stat(filepath.Join(root, "build"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBuildProfilesUseSelectedRegistries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: builds full and local factory profiles")
	}
	t.Setenv("UB_STATE_KEY", "")
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS("testdata/ub/build-profiles/valid")))
	core, err := filepath.Abs("../..")
	require.NoError(t, err)
	options := Options{
		FactoryPath: filepath.Join(root, "factory.ub"), StackName: "profile-check",
		GoVersion: "1.26.2", Version: "v0.0.0", CLIVersion: "dev", Build: true,
		ReplaceUnobin: core, Stdout: io.Discard, Stderr: io.Discard,
		NewResolver: func(project string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(project), nil
		},
	}
	var revisions []string
	profiles := []program.BuildProfile{program.BuildProfileFull, program.BuildProfileLocal}
	for _, profile := range profiles {
		options.BuildProfile = profile
		options.OutDir = filepath.Join(root, string(profile))
		result, err := RunResult(options)
		require.NoError(t, err)
		revisions = append(revisions, result.ContentRevision)
		goList := exec.CommandContext(t.Context(), "go", "list", "-buildvcs=false", "-deps", ".")
		goList.Dir = result.OutputDir
		deps, err := goList.CombinedOutput()
		require.NoError(t, err, "%s", deps)
		if profile == program.BuildProfileLocal {
			for _, excluded := range []string{
				"github.com/aws/aws-sdk-go-v2", "google.golang.org/api",
				"github.com/cloudboss/unobin/pkg/backends\n",
				"github.com/cloudboss/unobin/pkg/encrypters\n",
				"github.com/cloudboss/unobin/pkg/runner\n",
			} {
				require.NotContains(t, string(deps), excluded)
			}
		} else {
			require.Contains(t, string(deps), "github.com/cloudboss/unobin/pkg/runner")
		}
		command := func(args ...string) ([]byte, error) {
			cmd := exec.CommandContext(t.Context(), result.BinaryPath, args...)
			cmd.Dir = root
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if err != nil {
				return append(output, stderr.Bytes()...), err
			}
			return output, nil
		}
		output, err := command("schema", "state", "--format", "json")
		require.NoError(t, err, "%s", output)
		var schema struct {
			Backends   []struct{ Name string }
			Encrypters []struct{ Name string }
		}
		require.NoError(t, json.Unmarshal(output, &schema))
		var backends, encrypters []string
		for _, backend := range schema.Backends {
			backends = append(backends, backend.Name)
		}
		for _, encrypter := range schema.Encrypters {
			encrypters = append(encrypters, encrypter.Name)
		}
		if profile == program.BuildProfileFull {
			require.Equal(t, []string{"gcs", "local", "s3"}, backends)
			require.Equal(t, []string{"env-key", "gcp-kms", "kms", "noop"}, encrypters)
			continue
		}
		require.Equal(t, []string{"local"}, backends)
		require.Equal(t, []string{"env-key", "noop"}, encrypters)
		for _, selection := range []struct{ path, message string }{
			{path: "unsupported-state.ub", message: `no backend named "s3"; available: local`},
			{path: "unsupported-encryption.ub",
				message: `no key-source named "kms"; available: env-key, noop`},
		} {
			output, err := command("plan", "--allow-version-mismatch", "-c", selection.path,
				"-o", "unsupported.ubp")
			require.Error(t, err)
			require.Contains(t, string(output), selection.message)
		}
		_, err = os.Stat(filepath.Join(root, ".unsupported-state"))
		require.ErrorIs(t, err, os.ErrNotExist)
		output, err = command("plan", "--allow-version-mismatch", "-c", "stack.ub", "-o", "plan.ubp")
		require.NoError(t, err, "%s", output)
		output, err = command("apply", "plan.ubp")
		require.NoError(t, err, "%s", output)
		output, err = command("output", "-c", "stack.ub", "value")
		require.NoError(t, err, "%s", output)
		require.Equal(t, "'local profile'", strings.TrimSpace(string(output)))
		output, err = command("state", "pull", "-c", "stack.ub")
		require.NoError(t, err, "%s", output)
		snapshot, err := state.DecodeSnapshot(output)
		require.NoError(t, err)
		require.Equal(t, "local profile", snapshot.Outputs["value"])
	}
	require.NotEqual(t, revisions[0], revisions[1])
}
