package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
)

func pinFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/pin", name)
}

// TestPinFile asserts the canonical form of pinFile's output, since the
// splice result is a formatter draft and reaches disk only through
// WriteCanonical. The already-pinned case asserts raw bytes instead:
// that path leaves the file untouched.
func TestPinFile(t *testing.T) {
	const (
		libraryPath = "github.com/cloudboss/cluster-deploy"
		version     = "v0.3.0"
		revision    = "fedcba"
	)
	tests := []struct {
		name   string
		src    string
		want   string
		action string
	}{
		{
			name:   "no factory block",
			src:    pinFixture(t, "no-factory-src"),
			want:   pinFixture(t, "no-factory-want"),
			action: pinActionAddedFactoryBlock,
		},
		{
			name:   "empty file",
			src:    ``,
			want:   pinFixture(t, "empty-want"),
			action: pinActionAddedFactoryBlock,
		},
		{
			name:   "factory block without pin",
			src:    pinFixture(t, "factory-no-pin-src"),
			want:   pinFixture(t, "factory-no-pin-want"),
			action: pinActionAddedPin,
		},
		{
			name:   "single-line factory block without pin",
			src:    pinFixture(t, "single-line-src"),
			want:   pinFixture(t, "single-line-want"),
			action: pinActionAddedPin,
		},
		{
			name:   "source stack factory block without pin",
			src:    pinFixture(t, "stack-factory-no-pin-src"),
			want:   pinFixture(t, "stack-factory-no-pin-want"),
			action: pinActionAddedPin,
		},
		{
			name:   "pin block missing supported-versions",
			src:    pinFixture(t, "missing-supported-src"),
			want:   pinFixture(t, "missing-supported-want"),
			action: pinActionAddedSupportedVersions,
		},
		{
			name:   "empty pin block",
			src:    pinFixture(t, "empty-pin-src"),
			want:   pinFixture(t, "empty-pin-want"),
			action: pinActionAddedSupportedVersions,
		},
		{
			name:   "empty supported-versions list",
			src:    pinFixture(t, "empty-supported-src"),
			want:   pinFixture(t, "empty-supported-want"),
			action: pinActionAppendedEntry,
		},
		{
			name:   "existing entries without a trailing comma",
			src:    pinFixture(t, "no-trailing-comma-src"),
			want:   pinFixture(t, "no-trailing-comma-want"),
			action: pinActionAppendedEntry,
		},
		{
			name:   "existing entries with a trailing comma",
			src:    pinFixture(t, "trailing-comma-src"),
			want:   pinFixture(t, "trailing-comma-want"),
			action: pinActionAppendedEntry,
		},
		{
			name:   "idempotent when entry already present",
			src:    pinFixture(t, "already-pinned-src"),
			want:   pinFixture(t, "already-pinned-want"),
			action: pinActionAlreadyPinned,
		},
		{
			name:   "inline supported-versions with one entry",
			src:    pinFixture(t, "inline-supported-src"),
			want:   pinFixture(t, "inline-supported-want"),
			action: pinActionAppendedEntry,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := sourceStackWithNoop(tt.src)
			want := sourceStackWithNoop(tt.want)
			got, action, err := pinFile([]byte(src), libraryPath, version, revision)
			require.NoError(t, err)
			assert.Equal(t, tt.action, action)
			if tt.action == pinActionAlreadyPinned {
				assert.Equal(t, src, string(got))
				return
			}
			canonical, err := lang.Canonicalize("stack.ub", got)
			require.NoError(t, err, "pinFile output failed to parse")
			wantCanonical, err := lang.Canonicalize("stack.ub", []byte(want))
			require.NoError(t, err, "pinFile expected output failed to parse")
			assert.Equal(t, string(wantCanonical), string(canonical))
		})
	}
}

func TestPinFileRejectsUnwrappedStack(t *testing.T) {
	src := []byte(pinFixture(t, "reject-unwrapped-stack"))

	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stack file must declare stack")
}

func TestPinFileRejectsLibraryPathMismatch(t *testing.T) {
	src := []byte(pinFixture(t, "reject-library-path-mismatch"))
	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library-path")
}

// TestPinFileRejectsInvalidConfig proves pin refuses to splice into a
// config the validator rejects, instead of stacking a pin block beside
// keys it does not understand.
func TestPinFileRejectsInvalidConfig(t *testing.T) {
	src := []byte(sourceStackWithNoop(pinFixture(t, "reject-invalid-config-body")))
	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid stack factory field")
}

// TestPinWritesCanonicalFile proves the written stack file is reformatted as a
// whole, not just the spliced entry: an operator's odd indentation in an
// untouched block comes out canonical too.
func TestPinWritesCanonicalFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dev.ub")
	require.NoError(t, os.WriteFile(configPath, []byte(pinFixture(t, "write-canonical-input")), 0o644))

	info := Info{
		LibraryPath:     "github.com/cloudboss/cluster-deploy",
		FactoryVersion:  "v0.3.0",
		ContentRevision: "fedcba",
	}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, doPin(cmd, info, configPath, "", ""))

	got, err := os.ReadFile(configPath)
	require.NoError(t, err)
	canonical, err := lang.Canonicalize("stack.ub", got)
	require.NoError(t, err)
	assert.Equal(t, string(canonical), string(got), "pinned stack file should be canonical")
	assert.NotContains(t, string(got), "message:   'hi'", "operator spacing should be normalized")
}

func TestPinFilePreservesTrailingContent(t *testing.T) {
	src := []byte(sourceStackWithNoop(pinFixture(t, "preserves-trailing-src")))
	want := sourceStackWithNoop(pinFixture(t, "preserves-trailing-want"))
	got, action, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.3.0", "fedcba")
	require.NoError(t, err)
	assert.Equal(t, pinActionAppendedEntry, action)
	canonical, err := lang.Canonicalize("stack.ub", got)
	require.NoError(t, err)
	wantCanonical, err := lang.Canonicalize("stack.ub", []byte(want))
	require.NoError(t, err)
	assert.Equal(t, string(wantCanonical), string(canonical))
}
