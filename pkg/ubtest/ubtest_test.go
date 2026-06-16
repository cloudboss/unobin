package ubtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDriver is a unobin-free stand-in used to exercise Run. A line beginning
// with "! " becomes a diagnostic (the text after it); "#" and blank lines are
// ignored; every other line is uppercased into the output.
func fakeDriver(_ string, src []byte) (string, []string) {
	var diags, kept []string
	for line := range strings.SplitSeq(strings.TrimRight(string(src), "\n"), "\n") {
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "! "):
			diags = append(diags, strings.TrimPrefix(line, "! "))
		default:
			kept = append(kept, strings.ToUpper(line))
		}
	}
	var output string
	if len(kept) > 0 {
		output = strings.Join(kept, "\n") + "\n"
	}
	return output, diags
}

func TestRunExactGoldens(t *testing.T) {
	Run(t, "testdata/ub/exact", fakeDriver)
}

func TestRunSubstring(t *testing.T) {
	Run(t, "testdata/ub/substr", fakeDriver, Substring())
}

// fakeDriver uppercases its output, so feeding that output back is stable.
func TestRunIdempotent(t *testing.T) {
	Run(t, "testdata/ub/exact", fakeDriver, Idempotent())
}

func TestUpdateWritesAndRemovesGoldens(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "testdata", "ub")
	writeFixture := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	writeFixture("invalid/boom.ub", "! boom\nkeep me")
	writeFixture("valid/quiet.ub", "# nothing happens\n")

	old := *update
	*update = true
	t.Cleanup(func() { *update = old })
	Run(t, root, fakeDriver)

	errGolden, err := os.ReadFile(filepath.Join(root, "invalid", "boom.ub.err"))
	require.NoError(t, err)
	assert.Equal(t, "boom\n", string(errGolden))
	outGolden, err := os.ReadFile(filepath.Join(root, "invalid", "boom.ub.out"))
	require.NoError(t, err)
	assert.Equal(t, "KEEP ME\n", string(outGolden))

	// A clean fixture records no goldens.
	_, err = os.Stat(filepath.Join(root, "valid", "quiet.ub.err"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(root, "valid", "quiet.ub.out"))
	assert.True(t, os.IsNotExist(err))

	// The recorded goldens now pass an ordinary run.
	*update = false
	Run(t, root, fakeDriver)
}

func TestDiscover(t *testing.T) {
	fixtures, err := discover("testdata/ub/exact")
	require.NoError(t, err)
	var names []string
	for _, fx := range fixtures {
		names = append(names, fx.name)
	}
	assert.Equal(t, []string{
		"invalid/one-error",
		"invalid/two-errors",
		"valid/clean",
		"valid/silent",
	}, names)
}

func TestDiscoverEmpty(t *testing.T) {
	fixtures, err := discover(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, fixtures)
}

func TestFormatDiags(t *testing.T) {
	tests := []struct {
		name  string
		diags []string
		want  string
	}{
		{"none", nil, ""},
		{"one", []string{"a"}, "a"},
		{"many", []string{"a", "b", "c"}, "a\nb\nc"},
		{"hint in diag", []string{"msg\n  hint: x", "b"}, "msg\n  hint: x\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatDiags(tt.diags))
		})
	}
}

func TestNonEmptyLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"blank only", "\n  \n", nil},
		{"trims blanks", "a\n\nb\n", []string{"a", "b"}},
		{"keeps indent", "  a\nb", []string{"  a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nonEmptyLines(tt.in))
		})
	}
}

func TestAppendNL(t *testing.T) {
	assert.Equal(t, "", appendNL(""))
	assert.Equal(t, "x\n", appendNL("x"))
	assert.Equal(t, "x\n", appendNL("x\n"))
}
