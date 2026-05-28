package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runCommand(t *testing.T, a *CommandAction) *CommandActionOutput {
	t.Helper()
	res, err := a.Run(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

func TestCommandSucceeds(t *testing.T) {
	cr := runCommand(t, &CommandAction{Argv: []string{"echo", "hello"}})
	require.Equal(t, "hello\n", cr.Stdout)
	require.Empty(t, cr.Stderr)
	require.Equal(t, 0, cr.ExitCode)
	require.True(t, cr.Duration > 0, "duration should be positive, got %v", cr.Duration)
}

func TestCommandPreservesArgsWithSpaces(t *testing.T) {
	cr := runCommand(t, &CommandAction{Argv: []string{"echo", "two words"}})
	require.Equal(t, "two words\n", cr.Stdout)
}

func TestCommandReportsExitCode(t *testing.T) {
	cr := runCommand(t, &CommandAction{Argv: []string{"false"}})
	require.Equal(t, 1, cr.ExitCode)
}

func TestCommandRequiresArgv(t *testing.T) {
	_, err := (&CommandAction{}).Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "argv is required")
}

func TestCommandArgsArePassedLiterally(t *testing.T) {
	cr := runCommand(t, &CommandAction{
		Argv:        []string{"echo", "$UNOBIN_TEST_KEY"},
		Environment: map[string]string{"UNOBIN_TEST_KEY": "abc123"},
	})
	require.Equal(t, "$UNOBIN_TEST_KEY\n", cr.Stdout)
}

func TestCommandEnvironmentVisibleToChild(t *testing.T) {
	cr := runCommand(t, &CommandAction{
		Argv:        []string{"sh", "-c", "echo \"$UNOBIN_TEST_KEY\""},
		Environment: map[string]string{"UNOBIN_TEST_KEY": "abc123"},
	})
	require.Equal(t, "abc123\n", cr.Stdout)
}

func TestCommandWorkingDir(t *testing.T) {
	dir := t.TempDir()
	cr := runCommand(t, &CommandAction{Argv: []string{"pwd"}, WorkingDir: dir})
	require.True(t, strings.HasPrefix(strings.TrimSpace(cr.Stdout), dir),
		"pwd %q should start with %q", cr.Stdout, dir)
}

func TestCommandContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := (&CommandAction{Argv: []string{"sleep", "5"}}).Run(ctx, nil)
	require.Error(t, err)
}

func TestCommandMissingExecutable(t *testing.T) {
	a := &CommandAction{Argv: []string{"unobin-no-such-binary-xyz"}}
	_, err := a.Run(context.Background(), nil)
	require.Error(t, err)
}

func TestCoreModuleRegistersCommand(t *testing.T) {
	lib := Library()
	require.Equal(t, "core", lib.Name)
	require.Contains(t, lib.Actions, "command")

	at := lib.Actions["command"]
	require.NotNil(t, at)

	a, ok := at.NewReceiver().(*CommandAction)
	require.True(t, ok)
	require.NotNil(t, a)
}
