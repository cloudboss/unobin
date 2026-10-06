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

	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func BenchmarkCompileFactoryProfile(b *testing.B) {
	profile, err := program.ParseBuildProfile(os.Getenv("UNOBIN_BENCH_PROFILE"))
	require.NoError(b, err)
	for _, count := range []int{10, 500} {
		for _, cold := range []bool{false, true} {
			cache := "warm"
			if cold {
				cache = "cold"
			}
			b.Run(fmt.Sprintf("nodes=%d/cache=%s", count, cache), func(b *testing.B) {
				options := compileBenchmarkOptions(b, count)
				options.Build, options.BuildProfile = true, profile
				outputs := b.TempDir()
				if cold {
					b.Setenv("GOCACHE", filepath.Join(b.TempDir(), "cache"))
				}
				var result *Result
				iteration := 0
				for b.Loop() {
					options.OutDir = filepath.Join(outputs, fmt.Sprintf("output-%d", iteration))
					iteration++
					result, err = RunResult(options)
					require.NoError(b, err)
				}
				require.True(b, result.Built)
				require.NotEmpty(b, result.ContentRevision)
				info, err := os.Stat(result.BinaryPath)
				require.NoError(b, err)
				b.ReportMetric(float64(info.Size()), "binary-bytes")
				b.ReportMetric(float64(count), "nodes")
				goFiles, sourceBytes := 0, 0
				require.NoError(b, filepath.WalkDir(result.OutputDir,
					func(path string, entry fs.DirEntry, err error) error {
						if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
							return err
						}
						body, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						if _, err := parser.ParseFile(token.NewFileSet(), path, body,
							parser.AllErrors); err != nil {
							return err
						}
						goFiles++
						sourceBytes += len(body)
						return nil
					}))
				require.Equal(b, 2, goFiles)
				b.ReportMetric(float64(sourceBytes), "source-bytes")
				goBin, err := toolchain.Ensure(io.Discard)
				require.NoError(b, err)
				command := exec.Command(goBin, "list", "-buildvcs=false", "-deps", ".")
				command.Dir = result.OutputDir
				output, err := command.CombinedOutput()
				require.NoError(b, err, "%s", output)
				packages := strings.Fields(string(output))
				require.Contains(b, packages, "github.com/cloudboss/unobin/pkg/factorycli")
				if profile == program.BuildProfileFull {
					require.Contains(b, packages, "github.com/cloudboss/unobin/pkg/runner")
				} else {
					for _, dependency := range packages {
						require.NotContains(b, dependency, "github.com/aws/aws-sdk-go-v2")
						require.NotContains(b, dependency, "google.golang.org/api")
					}
					require.NotContains(b, packages, "github.com/cloudboss/unobin/pkg/runner")
				}
				b.ReportMetric(float64(len(packages)), "packages")
			})
		}
	}
}
