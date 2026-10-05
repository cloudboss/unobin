package evidence

import (
	"fmt"
	"io/fs"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const repositoryModule = "github.com/cloudboss/unobin"

func (spec Spec) Validate() error {
	if spec.WorkPackage == "" || spec.Description == "" || spec.Setup == "" ||
		spec.CachePolicy == "" || len(spec.Parameters) == 0 {
		return fmt.Errorf("workload must declare its identity, parameters, setup, and cache policy")
	}
	switch spec.Kind {
	case "micro":
		duration, err := time.ParseDuration(spec.Benchtime)
		if err != nil || duration < 2*time.Second || spec.Samples < 10 {
			return fmt.Errorf("microbenchmarks require ten samples and a duration of at least two seconds")
		}
		if !slices.Equal(spec.CPUs, []int{1, 4}) {
			return fmt.Errorf("microbenchmarks require CPU settings 1,4")
		}
	case "build", "profile":
		if spec.Benchtime != "1x" || spec.Samples < 5 {
			return fmt.Errorf("full builds require at least five independent 1x samples")
		}
	default:
		return fmt.Errorf("unknown measurement kind %q", spec.Kind)
	}
	if len(spec.CPUs) == 0 || !slices.IsSorted(spec.CPUs) ||
		spec.CPUs[0] < 1 || len(slices.Compact(slices.Clone(spec.CPUs))) != len(spec.CPUs) {
		return fmt.Errorf("CPU settings must be positive, distinct, and sorted")
	}
	if _, err := regexp.Compile(spec.Benchmark); err != nil || spec.Benchmark == "" {
		return fmt.Errorf("invalid benchmark expression %q", spec.Benchmark)
	}
	if len(spec.Fixtures) == 0 || len(spec.Packages) == 0 || len(spec.Cases) == 0 {
		return fmt.Errorf("workload requires fixtures, packages, and expected cases")
	}
	for _, path := range spec.Fixtures {
		if !safePath(path) {
			return fmt.Errorf("invalid fixture path %q", path)
		}
	}
	packages := map[string]bool{}
	for _, pkg := range spec.Packages {
		if !strings.HasPrefix(pkg, "./") || !safePath(strings.TrimPrefix(pkg, "./")) {
			return fmt.Errorf("invalid benchmark package %q", pkg)
		}
		full := repositoryModule + "/" + strings.TrimPrefix(pkg, "./")
		if packages[full] {
			return fmt.Errorf("duplicate benchmark package %q", pkg)
		}
		packages[full] = true
	}
	if spec.PrimaryUnit == "" ||
		spec.PrimaryDirection != "lower" && spec.PrimaryDirection != "higher" {
		return fmt.Errorf("workload must declare a primary unit and improvement direction")
	}
	seen := map[string]bool{}
	for _, testCase := range spec.Cases {
		key := testCase.Package + ":" + testCase.Name
		if !packages[testCase.Package] || !strings.HasPrefix(testCase.Name, "Benchmark") || seen[key] {
			return fmt.Errorf("invalid or duplicate expected case %q", key)
		}
		seen[key] = true
		for _, unit := range []string{"sec/op", "B/op", "allocs/op", spec.PrimaryUnit} {
			if !slices.Contains(testCase.Units, unit) {
				return fmt.Errorf("case %s is missing declared unit %s", key, unit)
			}
		}
		if len(slices.Compact(slices.Sorted(slices.Values(testCase.Units)))) != len(testCase.Units) {
			return fmt.Errorf("case %s has duplicate units", key)
		}
		for unit, value := range testCase.Fixed {
			if !slices.Contains(testCase.Units, unit) || !finite(value) || value < 0 {
				return fmt.Errorf("case %s has an invalid fixed measurement for %s", key, unit)
			}
		}
	}
	if len(spec.Tests) == 0 {
		return fmt.Errorf("workload requires correctness tests")
	}
	for _, args := range spec.Tests {
		if len(args) < 3 || args[0] != "go" || args[1] != "test" {
			return fmt.Errorf("correctness checks must use explicit go test commands")
		}
		for i, arg := range args {
			if strings.HasPrefix(arg, "-bench") || arg == "-run=^$" ||
				arg == "-run" && i+1 < len(args) && args[i+1] == "^$" {
				return fmt.Errorf("correctness tests cannot be benchmark-only commands")
			}
		}
	}
	return nil
}

func (spec Spec) BenchmarkArgs(warmup bool) []string {
	benchtime, samples := spec.Benchtime, spec.Samples
	if warmup {
		benchtime, samples = "1x", 1
	}
	cpus := make([]string, len(spec.CPUs))
	for i, cpu := range spec.CPUs {
		cpus[i] = strconv.Itoa(cpu)
	}
	args := []string{
		"go", "test", "-run", "^$", "-bench", spec.Benchmark, "-benchmem",
		"-benchtime=" + benchtime, "-count=" + strconv.Itoa(samples),
		"-cpu=" + strings.Join(cpus, ","), "-p=1", "-timeout=30m",
	}
	return append(args, spec.Packages...)
}

func safePath(path string) bool {
	return path != "." && fs.ValidPath(path) && !strings.ContainsAny(path, `\:`)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
