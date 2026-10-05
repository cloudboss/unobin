package compile

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/filechange"
)

func buildInputFixture(t *testing.T) (string, string) {
	t.Helper()
	goBin, err := exec.LookPath("go")
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS("testdata/go/build-inputs")))
	return goBin, root
}

func TestBuildInputsTrackLocalImplementations(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	goBin, root := buildInputFixture(t)
	dir := filepath.Join(root, "factory")
	before, err := readBuildInputs(goBin, dir)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{12}$`), before.Revision)
	require.NoError(t, before.verify(goBin, dir))

	for _, path := range []string{
		"factory/main.go", "factory/factory.assets", "library/library.go",
		"helper/helper.go", "core/helper/helper.go", "library/data/message.txt",
		"library/engine/native.c", "library/native/detail.h", "library/go.mod",
	} {
		t.Run(path, func(t *testing.T) {
			path = filepath.Join(root, path)
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			body := append([]byte(nil), original...)
			if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".c") ||
				strings.HasSuffix(path, ".h") || strings.HasSuffix(path, "go.mod") {
				body = append(body, []byte("\n// changed input\n")...)
			} else {
				body = append(body, []byte("changed input")...)
			}
			require.NoError(t, os.WriteFile(path, body, 0o644))
			t.Cleanup(func() { require.NoError(t, os.WriteFile(path, original, 0o644)) })
			after, err := readBuildInputs(goBin, dir)
			require.NoError(t, err)
			require.NotEqual(t, before.Revision, after.Revision)
			require.ErrorContains(t, before.verify(goBin, dir), "changed during compilation")
		})
	}
}

func TestBuildInputsIgnoreCheckoutPathsAndUnlinkedFiles(t *testing.T) {
	goBin, first := buildInputFixture(t)
	_, second := buildInputFixture(t)
	var revisions []string
	for _, root := range []string{first, second} {
		dir := filepath.Join(root, "factory")
		path := filepath.Join(dir, "go.mod")
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, name := range []string{"library", "helper", "core"} {
			body = []byte(strings.ReplaceAll(string(body), "../"+name, filepath.Join(root, name)))
		}
		require.NoError(t, os.WriteFile(path, body, 0o644))
		inputs, err := readBuildInputs(goBin, dir)
		require.NoError(t, err)
		revisions = append(revisions, inputs.Revision)
	}
	require.Equal(t, revisions[0], revisions[1])
	for _, path := range []string{
		"library/ignored.go", "library/library_test.go", "helper/notes.txt",
	} {
		path = filepath.Join(first, path)
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, append(body, []byte("\n// ignored edit\n")...), 0o644))
	}
	inputs, err := readBuildInputs(goBin, filepath.Join(first, "factory"))
	require.NoError(t, err)
	require.Equal(t, revisions[0], inputs.Revision)
}

func TestBuildInputsIncludeTargetContext(t *testing.T) {
	goBin, root := buildInputFixture(t)
	dir := filepath.Join(root, "factory")
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOAMD64", "v1")
	before, err := readBuildInputs(goBin, dir)
	require.NoError(t, err)
	t.Setenv("GOAMD64", "v2")
	after, err := readBuildInputs(goBin, dir)
	require.NoError(t, err)
	require.NotEqual(t, before.Revision, after.Revision)
	require.ErrorContains(t, before.verify(goBin, dir), "changed during compilation")
	_, err = readBuildInputs("unavailable-go", dir)
	require.ErrorIs(t, err, exec.ErrNotFound)
}

func TestBuildInputsReadGoOverlay(t *testing.T) {
	goBin, root := buildInputFixture(t)
	dir := filepath.Join(root, "factory")
	t.Setenv("CGO_ENABLED", "0")
	plain, err := readBuildInputs(goBin, dir)
	require.NoError(t, err)
	original := filepath.Join(root, "helper", "helper.go")
	overlaySource := filepath.Join(root, "overlay.go")
	body, err := os.ReadFile(original)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(overlaySource, body, 0o644))
	encoded, err := json.Marshal(map[string]any{"Replace": map[string]string{original: overlaySource}})
	require.NoError(t, err)
	overlay := filepath.Join(root, "overlay.json")
	require.NoError(t, os.WriteFile(overlay, encoded, 0o644))
	t.Setenv("GOFLAGS", "-overlay="+overlay)
	before, err := readBuildInputs(goBin, dir)
	require.NoError(t, err)
	require.NotEqual(t, plain.Revision, before.Revision)
	require.NoError(t, os.WriteFile(original, []byte("invalid original source"), 0o644))
	require.NoError(t, before.verify(goBin, dir))
	edited := append(body, []byte("\n// overlay edit\n")...)
	require.NoError(t, os.WriteFile(overlaySource, edited, 0o644))
	require.ErrorContains(t, before.verify(goBin, dir), "changed during compilation")
}

func TestPublishCheckedBinaryRejectsChangedSources(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	goBin, root := buildInputFixture(t)
	dir := filepath.Join(root, "factory")
	inputs, err := readBuildInputs(goBin, dir)
	require.NoError(t, err)
	previous := filepath.Join(dir, "factory")
	require.NoError(t, os.WriteFile(previous, []byte("previous binary"), 0o755))
	changes, err := publishCheckedBinary(dir, "factory", func(staged string) error {
		path := filepath.Join(root, "helper", "helper.go")
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		edited := append(body, []byte("\n// edit while building\n")...)
		if err := os.WriteFile(path, edited, 0o644); err != nil {
			return err
		}
		command := exec.Command(goBin, "build", "-buildvcs=false", "-o", staged, ".")
		command.Dir = dir
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		return err
	}, func() error { return inputs.verify(goBin, dir) })
	require.ErrorContains(t, err, "changed during compilation: package:example.com/inputs/helper")
	require.Empty(t, changes)
	body, err := os.ReadFile(previous)
	require.NoError(t, err)
	require.Equal(t, "previous binary", string(body))
}

func TestPublishCheckedBinaryPreservesPreviousBinary(t *testing.T) {
	for _, failure := range []string{"", "build", "verification"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "factory")
			require.NoError(t, os.WriteFile(path, []byte("previous binary"), 0o755))
			cause := errors.New("inputs changed during compilation")
			changes, err := publishCheckedBinary(dir, "factory", func(staged string) error {
				require.NotEqual(t, path, staged)
				require.NoError(t, os.WriteFile(staged, []byte("new binary"), 0o755))
				if failure == "build" {
					return cause
				}
				return nil
			}, func() error {
				if failure == "verification" {
					return cause
				}
				return nil
			})
			body, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			if failure == "" {
				require.NoError(t, err)
				require.Equal(t, "new binary", string(body))
				require.Equal(t, []filechange.Change{{Path: path, Action: filechange.ActionUpdated}}, changes)
			} else {
				require.ErrorIs(t, err, cause)
				require.Empty(t, changes)
				require.Equal(t, "previous binary", string(body))
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			require.Equal(t, []string{"factory"}, names)
		})
	}
}
