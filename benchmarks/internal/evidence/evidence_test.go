package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixtureSpec() Spec {
	return Spec{
		WorkPackage: "F00-validator", Description: "Synthetic validator test data", Kind: "micro",
		Fixtures:   []string{"pkg/runtime/index_benchmark_test.go"},
		Parameters: map[string]string{"nodes": "10"}, Packages: []string{"./pkg/runtime"},
		Benchmark: "^BenchmarkWork$", Samples: 10, Benchtime: "2s", CPUs: []int{1, 4},
		Setup: "Input construction excluded", CachePolicy: "Symmetric warm cache",
		PrimaryUnit: "sec/op", PrimaryDirection: "lower",
		Cases: []Case{{
			Package: "github.com/cloudboss/unobin/pkg/runtime", Name: "BenchmarkWork/n=10",
			Units: []string{"sec/op", "B/op", "allocs/op", "nodes"},
			Fixed: map[string]float64{"nodes": 10},
		}},
		Tests: [][]string{{"go", "test", "./pkg/runtime"}},
	}
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func fixtureRecord(spec Spec, ns float64, revision string) (Record, map[string][]byte) {
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	files := map[string][]byte{"test.txt": []byte("ok github.com/cloudboss/unobin/pkg/runtime\n")}
	outputFor := func(samples, iterations int) []byte {
		var output strings.Builder
		fmt.Fprintln(&output, "goos: linux\ngoarch: amd64")
		fmt.Fprintln(&output, "pkg: github.com/cloudboss/unobin/pkg/runtime\ncpu: Synthetic CPU")
		for _, cpu := range spec.CPUs {
			name := "BenchmarkWork/n=10"
			if cpu != 1 {
				name += fmt.Sprintf("-%d", cpu)
			}
			for range samples {
				fmt.Fprintf(&output, "%s %d %.3f ns/op 8 B/op 1 allocs/op 10 nodes\n",
					name, iterations, ns)
			}
		}
		fmt.Fprintln(&output, "PASS\nok fixture")
		return []byte(output.String())
	}
	iterations := 1
	if spec.Kind == "micro" {
		iterations = 50000000
	}
	files["benchmarks.txt"] = outputFor(spec.Samples, iterations)
	files["warmup.txt"] = outputFor(1, 1)
	run := func(args []string, name string, duration time.Duration) Run {
		started := start
		start = start.Add(duration)
		return Run{
			Args: args, Directory: ".", Started: started, Finished: start,
			Output: name, Digest: digest(files[name]),
		}
	}
	return Record{
		FormatVersion: FormatVersion, Spec: spec, Revision: revision,
		FixtureRevision: strings.Repeat("a", 40), Repository: "/repo", CleanSource: true,
		Fixtures:     map[string]string{spec.Fixtures[0]: strings.Repeat("a", 64)},
		Dependencies: map[string]string{"go.mod": strings.Repeat("b", 64)},
		Machine: Machine{
			GoVersion: "go1.26.2", GOOS: "linux", GOARCH: "amd64",
			CPUModel: "Synthetic CPU", LogicalCPUs: 8,
		},
		Environment: map[string]string{"CGO_ENABLED": "0", "GOFLAGS": "", "GOEXPERIMENT": ""},
		Benchstat:   BenchstatVersion,
		Tests:       []Run{run(spec.Tests[0], "test.txt", time.Second)},
		Warmup:      run(spec.BenchmarkArgs(true), "warmup.txt", time.Minute),
		Benchmark:   run(spec.BenchmarkArgs(false), "benchmarks.txt", 2*time.Minute),
	}, files
}

func TestCompareComputesStatisticsFromRawSamples(t *testing.T) {
	spec := fixtureSpec()
	before, beforeFiles := fixtureRecord(spec, 100, strings.Repeat("b", 40))
	after, afterFiles := fixtureRecord(spec, 80, strings.Repeat("c", 40))
	summary, err := Compare(before, beforeFiles, after, afterFiles)
	require.NoError(t, err)
	require.Equal(t, "improved", summary.Assessment)
	var timeResults []Statistic
	for _, result := range summary.Statistics {
		if result.Unit == "sec/op" {
			timeResults = append(timeResults, result)
			require.InDelta(t, 100e-9, result.Before.Median, 1e-15)
			require.InDelta(t, 80e-9, result.After.Median, 1e-15)
			require.InDelta(t, -20, *result.DeltaPercent, 1e-9)
			require.Equal(t, 10, result.Before.Samples)
			require.Equal(t, 10, result.After.Samples)
			require.True(t, result.Significant)
		}
	}
	require.Equal(t, []int{1, 4}, []int{timeResults[0].CPU, timeResults[1].CPU})
	_, err = json.Marshal(summary)
	require.NoError(t, err)
}

func TestCompareRejectsIncompleteOrDifferentEvidence(t *testing.T) {
	for _, problem := range []string{
		"missing raw", "changed raw", "failed benchmark", "failed test", "missing cases",
		"missing unit", "fewer samples", "changed counts", "changed workload", "changed source",
		"changed toolchain", "changed environment", "changed dependencies", "dirty source",
		"abbreviated revision", "no tests", "no warmup", "incomplete warmup", "short duration",
		"wrong benchmark flags",
		"invalid number", "unexpected case", "wrong target", "wrong comparison tool",
	} {
		t.Run(problem, func(t *testing.T) {
			spec := fixtureSpec()
			before, beforeFiles := fixtureRecord(spec, 100, strings.Repeat("b", 40))
			after, afterFiles := fixtureRecord(spec, 80, strings.Repeat("c", 40))
			rewriteRaw := func(old, replacement string) {
				body := strings.ReplaceAll(string(afterFiles["benchmarks.txt"]), old, replacement)
				afterFiles["benchmarks.txt"] = []byte(body)
				after.Benchmark.Digest = digest([]byte(body))
			}
			switch problem {
			case "missing raw":
				delete(afterFiles, "benchmarks.txt")
			case "changed raw":
				afterFiles["benchmarks.txt"] = []byte("fabricated")
			case "failed benchmark":
				after.Benchmark.ExitCode = 1
			case "failed test":
				after.Tests[0].ExitCode = 1
			case "missing cases":
				rewriteRaw("BenchmarkWork/n=10-4", "missing")
			case "missing unit":
				rewriteRaw(" 8 B/op", "")
			case "fewer samples":
				after.Spec.Samples = 9
			case "changed counts":
				rewriteRaw("10 nodes", "9 nodes")
			case "changed workload":
				after.Spec.Parameters = map[string]string{"nodes": "11"}
			case "changed source":
				after.Fixtures = maps.Clone(after.Fixtures)
				after.Fixtures[spec.Fixtures[0]] = strings.Repeat("c", 64)
			case "changed toolchain":
				after.Machine.GoVersion = "go1.27.0"
			case "changed environment":
				after.Environment["GOFLAGS"] = "-tags=changed"
			case "changed dependencies":
				after.Dependencies["go.mod"] = strings.Repeat("c", 64)
			case "dirty source":
				after.CleanSource = false
			case "abbreviated revision":
				after.Revision = "c123"
			case "no tests":
				after.Tests = nil
			case "no warmup":
				after.Warmup = Run{}
			case "incomplete warmup":
				afterFiles["warmup.txt"] = []byte("PASS\n")
				after.Warmup.Digest = digest(afterFiles["warmup.txt"])
			case "short duration":
				after.Benchmark.Finished = after.Benchmark.Started.Add(time.Second)
			case "wrong benchmark flags":
				after.Benchmark.Args = []string{"go", "test", "-bench", "."}
			case "invalid number":
				rewriteRaw("80.000 ns/op", "NaN ns/op")
			case "unexpected case":
				rewriteRaw("BenchmarkWork/n=10-4", "BenchmarkDifferent-4")
			case "wrong target":
				rewriteRaw("goarch: amd64", "goarch: arm64")
			case "wrong comparison tool":
				after.Benchstat = "unversioned"
			}
			_, err := Compare(before, beforeFiles, after, afterFiles)
			require.Error(t, err)
		})
	}
}

func TestSpecRequiresDeclaredSamplingAndInputs(t *testing.T) {
	for _, problem := range []string{"samples", "duration", "cpus", "fixtures", "cases", "tests"} {
		t.Run(problem, func(t *testing.T) {
			spec := fixtureSpec()
			switch problem {
			case "samples":
				spec.Samples = 9
			case "duration":
				spec.Benchtime = "100ms"
			case "cpus":
				spec.CPUs = []int{1}
			case "fixtures":
				spec.Fixtures = []string{"../outside"}
			case "cases":
				spec.Cases = nil
			case "tests":
				spec.Tests = [][]string{{"go", "test", "-run", "^$", "./pkg/runtime"}}
			}
			require.Error(t, spec.Validate())
		})
	}
	args := fixtureSpec().BenchmarkArgs(false)
	require.Equal(t, []string{
		"go", "test", "-run", "^$", "-bench", "^BenchmarkWork$", "-benchmem",
		"-benchtime=2s", "-count=10", "-cpu=1,4", "-p=1", "-timeout=30m", "./pkg/runtime",
	}, args)
}

func TestFiveBuildSamplesRetainUncertaintyWarnings(t *testing.T) {
	spec := fixtureSpec()
	spec.Kind, spec.Samples, spec.Benchtime = "build", 5, "1x"
	before, beforeFiles := fixtureRecord(spec, 100, strings.Repeat("b", 40))
	after, afterFiles := fixtureRecord(spec, 80, strings.Repeat("c", 40))
	summary, err := Compare(before, beforeFiles, after, afterFiles)
	require.NoError(t, err)
	for _, statistic := range summary.Statistics {
		require.NotEmpty(t, statistic.Warnings)
	}
	_, err = json.Marshal(summary)
	require.NoError(t, err)
	values, err := Validate(before, beforeFiles)
	require.NoError(t, err)
	for _, samples := range values {
		require.Equal(t, slices.Repeat([]float64{samples[0]}, 5), samples)
	}
}
