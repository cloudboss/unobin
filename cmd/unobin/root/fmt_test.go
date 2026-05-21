package root

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func runFmtCommand(t *testing.T, stdin io.Reader, args ...string) (string, error) {
	t.Helper()
	resetFlags(FmtCmd)
	root := &cobra.Command{
		Use:          "unobin",
		SilenceUsage: true,
	}
	root.AddCommand(FmtCmd)
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	if stdin != nil {
		root.SetIn(stdin)
	}
	root.SetArgs(append([]string{"fmt"}, args...))
	err := root.Execute()
	return out.String(), err
}

func writeUBFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

const canonicalSource = `name: 'demo'
items: [
  1,
  2,
]
`

const messySource = `name:'demo'
items:[1, 2]
`

func TestFmtCanonicalFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := writeUBFile(t, dir, "stack.ub", canonicalSource)

	got, err := runFmtCommand(t, nil, path)
	require.NoError(t, err)
	require.Equal(t, canonicalSource, got)
}

func TestFmtMessyFileEmitsCanonicalToStdout(t *testing.T) {
	dir := t.TempDir()
	path := writeUBFile(t, dir, "stack.ub", messySource)

	got, err := runFmtCommand(t, nil, path)
	require.NoError(t, err)
	require.Equal(t, canonicalSource, got)

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, messySource, string(onDisk), "default mode must not modify the file")
}

func TestFmtWriteModeReformatsFileInPlace(t *testing.T) {
	dir := t.TempDir()
	path := writeUBFile(t, dir, "stack.ub", messySource)

	got, err := runFmtCommand(t, nil, "-w", path)
	require.NoError(t, err)
	require.Empty(t, got, "write mode produces no stdout output")

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, canonicalSource, string(onDisk))
}

func TestFmtListModeReportsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	clean := writeUBFile(t, dir, "clean.ub", canonicalSource)
	dirty := writeUBFile(t, dir, "dirty.ub", messySource)

	got, err := runFmtCommand(t, nil, "-l", clean, dirty)
	require.NoError(t, err)
	require.Equal(t, dirty+"\n", got, "list mode prints only the dirty file")

	onDisk, err := os.ReadFile(dirty)
	require.NoError(t, err)
	require.Equal(t, messySource, string(onDisk), "list mode must not modify any file")
}

func TestFmtDirectoryWalksRecursively(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	a := writeUBFile(t, dir, "a.ub", messySource)
	b := writeUBFile(t, filepath.Join(dir, "sub"), "b.ub", messySource)
	skip := writeUBFile(t, dir, "skip.txt", messySource)

	got, err := runFmtCommand(t, nil, "-l", dir)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	require.ElementsMatch(t, []string{a, b}, lines)

	onDisk, err := os.ReadFile(skip)
	require.NoError(t, err)
	require.Equal(t, messySource, string(onDisk), "non-ub files must be ignored")
}

func TestFmtReadsStdinAndWritesStdout(t *testing.T) {
	got, err := runFmtCommand(t, strings.NewReader(messySource))
	require.NoError(t, err)
	require.Equal(t, canonicalSource, got)
}

func TestFmtRejectsUnparseableSource(t *testing.T) {
	dir := t.TempDir()
	path := writeUBFile(t, dir, "broken.ub", "not a valid: ub : ::\n")

	_, err := runFmtCommand(t, nil, path)
	require.Error(t, err)
}
