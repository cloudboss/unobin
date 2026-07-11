package diagnostic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type pathGolden struct {
	Display  []pathCaseGolden `json:"display"`
	Messages []pathCaseGolden `json:"messages"`
}

type pathCaseGolden struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

func TestPathMapperGolden(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	hidden := filepath.Join(root, "cache")
	for _, dir := range []string{
		project,
		real,
		hidden,
		filepath.Join(project, "vendor", "lib", "nested"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.Symlink(real, link))

	absoluteUser := filepath.Join(root, "absolute-user")
	mapper := PathMapper{
		WorkingDir: root,
		ProjectDir: project,
		Mappings: []PathMapping{
			{
				AbsoluteRoot: filepath.Join(project, "vendor", "lib"),
				DisplayRoot:  "vendor/lib",
			},
			{
				AbsoluteRoot: filepath.Join(project, "vendor", "lib", "nested"),
				DisplayRoot:  "special",
			},
			{AbsoluteRoot: link, DisplayRoot: "linked"},
			{
				AbsoluteRoot: filepath.Join(root, "logical.ub"),
				DisplayRoot:  "github.com/example/lib//logical.ub",
			},
			{AbsoluteRoot: absoluteUser, DisplayRoot: absoluteUser},
		},
		HiddenRoots: []string{hidden},
	}

	result := pathGolden{}
	displayCases := []struct {
		name string
		path string
	}{
		{name: "relative cleaned", path: filepath.Join("a", "..", "b.ub")},
		{
			name: "mapped descendant",
			path: filepath.Join(project, "vendor", "lib", "file.ub"),
		},
		{
			name: "longest mapped descendant",
			path: filepath.Join(project, "vendor", "lib", "nested", "file.ub"),
		},
		{name: "logical file", path: filepath.Join(root, "logical.ub")},
		{name: "absolute user path", path: filepath.Join(absoluteUser, "child.ub")},
		{name: "resolved symlink alias", path: filepath.Join(real, "child.ub")},
		{name: "project root", path: project},
		{name: "project fallback", path: filepath.Join(project, "src", "factory.ub")},
		{name: "hidden root", path: filepath.Join(hidden, "deep", "secret.ub")},
		{name: "unrelated absolute", path: filepath.Join(root, "elsewhere", "file.ub")},
		{
			name: "component boundary",
			path: filepath.Join(project, "vendor", "library", "file.ub"),
		},
	}
	for _, tc := range displayCases {
		result.Display = append(result.Display, pathCaseGolden{
			Name:   tc.name,
			Output: normalizeTestRoot(mapper.Display(tc.path), root),
		})
	}

	equalRoot := filepath.Join(root, "equal")
	equalMapper := PathMapper{Mappings: []PathMapping{
		{AbsoluteRoot: equalRoot, DisplayRoot: "first"},
		{AbsoluteRoot: equalRoot, DisplayRoot: "second"},
	}}
	result.Display = append(result.Display, pathCaseGolden{
		Name:   "equal mapping keeps order",
		Output: equalMapper.Display(filepath.Join(equalRoot, "file.ub")),
	})
	relativeMapper := PathMapper{
		WorkingDir: root,
		Mappings: []PathMapping{{
			AbsoluteRoot: "relative-root",
			DisplayRoot:  "requested-root",
		}},
	}
	result.Display = append(result.Display, pathCaseGolden{
		Name: "working directory absolute form",
		Output: relativeMapper.Display(
			filepath.Join(root, "relative-root", "file.ub"),
		),
	})

	messages := []struct {
		name    string
		message string
	}{
		{
			name: "mapped prefixes",
			message: "read " + filepath.Join(project, "vendor", "lib", "file.ub") +
				" and " + filepath.Join(project, "vendor", "lib", "nested", "deep.ub"),
		},
		{
			name:    "resolved symlink prefix",
			message: "open " + filepath.Join(real, "child.ub") + ": denied",
		},
		{
			name:    "project prefix",
			message: "parse " + filepath.Join(project, "src", "factory.ub") + ": failed",
		},
		{
			name:    "hidden prefix",
			message: "tool " + filepath.Join(hidden, "deep", "secret.ub") + ": failed",
		},
		{
			name: "component boundary untouched",
			message: "open " + filepath.Join(project, "vendor", "library", "file.ub") +
				": failed",
		},
	}
	for _, tc := range messages {
		result.Messages = append(result.Messages, pathCaseGolden{
			Name: tc.name,
			Output: normalizeTestRoot(
				mapper.ReplaceKnownPrefixes(tc.message),
				root,
			),
		})
	}

	requireDiagnosticGolden(t, "testdata/path.json", result)
}

func normalizeTestRoot(value, root string) string {
	value = filepath.ToSlash(value)
	return strings.ReplaceAll(value, filepath.ToSlash(root), "<tmp>")
}
