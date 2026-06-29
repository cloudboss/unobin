// Package ubtest runs file-based tests for .ub source fixtures. A test points
// Run at a directory of .ub files and supplies a Driver that compiles one
// fixture through some pipeline stage (parse, validate, reference check, ...).
//
// For each fixture <name>.ub, the diagnostics the driver produces are compared
// against a <name>.ub.err golden, and any textual output against a
// <name>.ub.out golden. A fixture with no .ub.err golden must compile cleanly.
// The -update flag rewrites the goldens from the driver's actual output.
//
// ubtest depends only on the standard library and testify, so every package
// (including the lang front end it tests) can use it without an import cycle.
// The driver closure, which lives in the calling test, does the unobin-specific
// work: parsing the source and reducing an *lang.ErrorList to []string.
package ubtest

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false,
	"rewrite .ub.err and .ub.out golden files from actual test output")

// Driver compiles one fixture and reports the result for golden comparison.
// name is the fixture's path relative to the root, without the .ub extension
// (e.g. "invalid/unclosed-array"); src is the raw .ub bytes. output is compared
// against the <name>.ub.out golden; return "" when the stage produces no
// textual output. diags is the diagnostics the stage produced, in stable order;
// an empty slice means the input was accepted.
type Driver func(name string, src []byte) (output string, diags []string)

// Option configures Run.
type Option func(*config)

type config struct {
	substring  bool
	idempotent bool
	runs       int
}

// Substring matches each line of a .ub.err golden as a substring of the
// produced diagnostics, instead of requiring the whole diagnostics block to
// match exactly. Use it for the parser layer, whose raw messages are verbose
// and version-sensitive; prefer exact matching everywhere else.
func Substring() Option {
	return func(c *config) { c.substring = true }
}

// Idempotent re-runs the driver on its own output and checks the result is
// unchanged. Use it for formatters and renderers, where format(format(x)) must
// equal format(x).
func Idempotent() Option {
	return func(c *config) { c.idempotent = true }
}

// Repeat runs each fixture n times and compares every result. The default is 2.
func Repeat(n int) Option {
	return func(c *config) { c.runs = n }
}

// ReadFixture reads a .ub fixture file and returns it as a string.
func ReadFixture(t testing.TB, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

// ReadValidFixture reads <name>.ub from the valid fixture directory under dir.
func ReadValidFixture(t testing.TB, dir, name string) string {
	t.Helper()
	return ReadFixture(t, filepath.Join(dir, "valid", name+".ub"))
}

// ReadInvalidFixture reads <name>.ub from the invalid fixture directory under dir.
func ReadInvalidFixture(t testing.TB, dir, name string) string {
	t.Helper()
	return ReadFixture(t, filepath.Join(dir, "invalid", name+".ub"))
}

// RequireInvalidFixtureGoldens checks every invalid .ub fixture under dir has
// a matching .ub.err golden.
func RequireInvalidFixtureGoldens(t testing.TB, dir string) {
	t.Helper()
	var missing []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".ub") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if !hasPathSegment(filepath.ToSlash(rel), "invalid") {
			return nil
		}
		if _, err := os.Stat(path + ".err"); err != nil {
			missing = append(missing, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err, "check invalid fixture goldens under %s", dir)
	if len(missing) > 0 {
		t.Fatalf("invalid .ub fixtures need matching .ub.err goldens:\n%s",
			strings.Join(missing, "\n"))
	}
}

func hasPathSegment(path, segment string) bool {
	return strings.Contains("/"+path+"/", "/"+segment+"/")
}

type fixture struct {
	name    string // path relative to the root, without the .ub extension
	src     []byte
	errPath string // <ub path>.err
	outPath string // <ub path>.out
}

// Run discovers every *.ub file under dir (recursively) and runs each through
// drive as a subtest named by its path relative to dir. Each fixture is run
// twice and the two results compared, catching nondeterministic drivers. With
// -update, the goldens are rewritten from actual output instead of asserted.
func Run(t *testing.T, dir string, drive Driver, opts ...Option) {
	t.Helper()
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.runs < 2 {
		cfg.runs = 2
	}
	fixtures, err := discover(dir)
	require.NoError(t, err, "discover %s", dir)
	require.NotEmpty(t, fixtures, "no .ub fixtures under %s", dir)

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			output, diags := drive(fx.name, fx.src)
			for i := range cfg.runs - 1 {
				again, diagsAgain := drive(fx.name, fx.src)
				require.Equal(t, output, again, "driver output changed on run %d", i+2)
				require.Equal(t, diags, diagsAgain, "driver diagnostics changed on run %d", i+2)
			}

			if cfg.idempotent && output != "" {
				reformatted, _ := drive(fx.name, []byte(output))
				require.Equal(t, output, reformatted,
					"driver is not idempotent: output changes when fed back in")
			}

			if *update {
				writeOrRemove(t, fx.errPath, appendNL(formatDiags(diags)))
				writeOrRemove(t, fx.outPath, output)
				return
			}
			checkDiags(t, fx, diags, cfg.substring)
			checkOutput(t, fx, output)
		})
	}
}

func discover(dir string) ([]fixture, error) {
	var fixtures []fixture
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".ub") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fixtures = append(fixtures, fixture{
			name:    strings.TrimSuffix(filepath.ToSlash(rel), ".ub"),
			src:     src,
			errPath: path + ".err",
			outPath: path + ".out",
		})
		return nil
	})
	return fixtures, err
}

func checkDiags(t *testing.T, fx fixture, diags []string, substring bool) {
	t.Helper()
	golden, ok := readGolden(t, fx.errPath)
	block := formatDiags(diags)
	if !ok {
		assert.Empty(t, diags,
			"unexpected diagnostics for %s (run go test -update to record them):\n%s",
			fx.name, block)
		return
	}
	want := strings.TrimRight(golden, "\n")
	if substring {
		for _, line := range nonEmptyLines(want) {
			assert.Contains(t, block, line,
				"diagnostics for %s missing expected substring", fx.name)
		}
		return
	}
	assert.Equal(t, want, strings.TrimRight(block, "\n"),
		"diagnostics for %s differ from %s (run go test -update to refresh)",
		fx.name, filepath.Base(fx.errPath))
}

func checkOutput(t *testing.T, fx fixture, output string) {
	t.Helper()
	golden, ok := readGolden(t, fx.outPath)
	if !ok {
		assert.Empty(t, output,
			"%s produced output but no %s golden exists (run go test -update)",
			fx.name, filepath.Base(fx.outPath))
		return
	}
	assert.Equal(t, golden, output,
		"output for %s differs from %s (run go test -update to refresh)",
		fx.name, filepath.Base(fx.outPath))
}

func readGolden(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	require.NoError(t, err, "read golden %s", path)
	return string(b), true
}

func writeOrRemove(t *testing.T, path, content string) {
	t.Helper()
	if content == "" {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			require.NoError(t, err, "remove %s", path)
		}
		return
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644), "write %s", path)
}

func formatDiags(diags []string) string {
	return strings.Join(diags, "\n")
}

func appendNL(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func nonEmptyLines(s string) []string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
