package docs

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/docgen"
	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/spf13/cobra"
)

var (
	apiOut string
	APICmd = &cobra.Command{
		Use:   "api [packages...]",
		Short: "Generate the Go package API reference as Markdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			roots := args
			if len(roots) == 0 {
				roots = []string{"pkg"}
			}
			return runAPI(cmd, roots)
		},
	}
)

func init() {
	APICmd.Flags().StringVarP(&apiOut, "out", "o", "docs/reference",
		"Directory to write the generated Markdown into.")
}

func runAPI(cmd *cobra.Command, roots []string) error {
	modPath, err := modulePath()
	if err != nil {
		return err
	}
	byDir, err := goFilesByDir(roots)
	if err != nil {
		return err
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	var summary strings.Builder
	for _, dir := range dirs {
		target, wrote, err := renderPackage(modPath, dir, byDir[dir])
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		if !wrote {
			continue
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", target)
		rel := filepath.ToSlash(dir)
		fmt.Fprintf(&summary, "* [%s](%s.md)\n", rel, rel)
	}
	if summary.Len() == 0 {
		return nil
	}
	// A SUMMARY.md lets mkdocs-literate-nav expand the api/ directory into
	// one nav entry per package.
	path := filepath.Join(apiOut, "api", "SUMMARY.md")
	if err := ufs.WriteFileAtomic(path, []byte(summary.String()), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", path)
	return nil
}

// modulePath reads the module path from go.mod in the current directory.
func modulePath() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no module directive in go.mod")
}

// goFilesByDir collects the non-test Go files under each root, grouped by
// directory. internal and testdata directories are skipped.
func goFilesByDir(roots []string) (map[string][]string, error) {
	byDir := map[string][]string{}
	for _, root := range roots {
		walk := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "internal" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				dir := filepath.Dir(path)
				byDir[dir] = append(byDir[dir], path)
			}
			return nil
		}
		if err := filepath.WalkDir(root, walk); err != nil {
			return nil, err
		}
	}
	return byDir, nil
}

// renderPackage parses one package's files and writes its API reference.
// It reports the target path and whether anything was written; packages
// named main, and those with no exported API, are skipped.
func renderPackage(modPath, dir string, goFiles []string) (string, bool, error) {
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(goFiles))
	for _, gf := range goFiles {
		f, err := parser.ParseFile(fset, gf, nil, parser.ParseComments)
		if err != nil {
			return "", false, err
		}
		if f.Name.Name == "main" {
			return "", false, nil
		}
		files = append(files, f)
	}
	importPath := modPath + "/" + filepath.ToSlash(dir)
	pkg, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return "", false, err
	}
	if pkg.Doc == "" && len(pkg.Consts) == 0 && len(pkg.Vars) == 0 &&
		len(pkg.Funcs) == 0 && len(pkg.Types) == 0 {
		return "", false, nil
	}
	target := filepath.Join(apiOut, "api", filepath.FromSlash(dir)+".md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", false, err
	}
	content := []byte(docgen.APIReference(pkg, fset))
	if err := ufs.WriteFileAtomic(target, content, 0o644); err != nil {
		return "", false, err
	}
	return target, true, nil
}
