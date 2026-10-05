package compile

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func BenchmarkCompileFactoryGeneration(b *testing.B) {
	for _, count := range []int{10, 500} {
		b.Run(fmt.Sprintf("nodes=%d", count), func(b *testing.B) {
			benchmarkCompileFactory(b, count, false, false)
		})
	}
}

func BenchmarkCompileFactoryBuild(b *testing.B) {
	for _, count := range []int{10, 500} {
		for _, cold := range []bool{false, true} {
			cache := "warm"
			if cold {
				cache = "cold"
			}
			b.Run(fmt.Sprintf("nodes=%d/cache=%s", count, cache), func(b *testing.B) {
				benchmarkCompileFactory(b, count, true, cold)
			})
		}
	}
}

func benchmarkCompileFactory(b *testing.B, count int, build, cold bool) {
	b.Helper()
	options := compileBenchmarkOptions(b, count)
	options.Build = build
	outputs := b.TempDir()
	if cold {
		b.Setenv("GOCACHE", filepath.Join(b.TempDir(), "cache"))
	}
	var result *Result
	iteration := 0
	for b.Loop() {
		options.OutDir = filepath.Join(outputs, fmt.Sprintf("output-%d", iteration))
		iteration++
		var err error
		result, err = RunResult(options)
		if err != nil {
			b.Fatal(err)
		}
	}
	require.Equal(b, "analysis-workload", result.FactoryName)
	require.Equal(b, build, result.Built)
	sourceBytes := 0
	goFiles := 0
	require.NoError(b, filepath.WalkDir(result.OutputDir,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, err := parser.ParseFile(token.NewFileSet(), path, body, parser.AllErrors); err != nil {
				return err
			}
			sourceBytes += len(body)
			goFiles++
			return nil
		}))
	require.Equal(b, 2, goFiles)
	b.ReportMetric(float64(sourceBytes), "source-bytes")
	b.ReportMetric(float64(count), "nodes")
	if build {
		require.NotEmpty(b, result.ContentRevision)
		info, err := os.Stat(result.BinaryPath)
		require.NoError(b, err)
		require.Greater(b, info.Size(), int64(0))
		b.ReportMetric(float64(info.Size()), "binary-bytes")
		goBin, err := toolchain.Ensure(io.Discard)
		require.NoError(b, err)
		command := exec.Command(goBin, "list", "-buildvcs=false", "-deps", ".")
		command.Dir = result.OutputDir
		output, err := command.CombinedOutput()
		require.NoError(b, err, "%s", output)
		packages := strings.Fields(string(output))
		require.Contains(b, packages, "github.com/cloudboss/unobin/pkg/runtime")
		require.Contains(b, packages, "github.com/cloudboss/unobin/pkg/runner")
		b.ReportMetric(float64(len(packages)), "packages")
	}
}

func compileBenchmarkOptions(b *testing.B, count int) Options {
	b.Helper()
	root := b.TempDir()
	require.NoError(b, os.CopyFS(root, os.DirFS("testdata/ub/compile-scaling/valid")))
	core, err := filepath.Abs("../..")
	require.NoError(b, err)
	return Options{
		FactoryPath: filepath.Join(root, fmt.Sprintf("nodes-%d", count), "factory.ub"),
		StackName:   "analysis-workload", GoVersion: "1.26.2", CLIVersion: "dev",
		ReplaceUnobin: core, Stdout: io.Discard, Stderr: io.Discard,
		NewResolver: func(project string) (resolve.Resolver, error) {
			return resolve.NewLocalResolver(project), nil
		},
	}
}
