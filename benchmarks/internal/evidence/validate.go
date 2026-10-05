package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/perf/benchfmt"
)

func Validate(record Record, files map[string][]byte) (Measurements, error) {
	if err := record.Spec.Validate(); err != nil {
		return nil, err
	}
	if record.FormatVersion != FormatVersion || record.Benchstat != BenchstatVersion {
		return nil, fmt.Errorf("unsupported evidence format or comparison-tool version")
	}
	if !fullRevision(record.Revision) || !fullRevision(record.FixtureRevision) || !record.CleanSource {
		return nil, fmt.Errorf("measurements require full revisions and clean measured source")
	}
	if !filepath.IsAbs(record.Repository) || record.Machine.GoVersion == "" ||
		record.Machine.GOOS == "" || record.Machine.GOARCH == "" ||
		record.Machine.CPUModel == "" || record.Machine.LogicalCPUs < 1 {
		return nil, fmt.Errorf("measurements require repository, toolchain, and machine metadata")
	}
	for _, key := range []string{"CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT"} {
		if _, exists := record.Environment[key]; !exists {
			return nil, fmt.Errorf("missing recorded environment setting %s", key)
		}
	}
	if len(record.Fixtures) == 0 || len(record.Dependencies) == 0 {
		return nil, fmt.Errorf("measurements require fixture and dependency digests")
	}
	if !safePath(record.SpecPath) || !fullDigest(record.Fixtures[record.SpecPath]) {
		return nil, fmt.Errorf("measurements require the committed workload declaration")
	}
	for _, inventory := range []map[string]string{record.Fixtures, record.Dependencies} {
		for name, digest := range inventory {
			if !safePath(name) || !fullDigest(digest) {
				return nil, fmt.Errorf("invalid input digest for %q", name)
			}
		}
	}
	for _, fixture := range record.Spec.Fixtures {
		found := false
		for name := range record.Fixtures {
			found = found || name == fixture || strings.HasPrefix(name, fixture+"/")
		}
		if !found {
			return nil, fmt.Errorf("no recorded inputs for fixture %q", fixture)
		}
	}
	if len(record.Tests) != len(record.Spec.Tests) {
		return nil, fmt.Errorf("missing correctness-test runs")
	}
	passed := map[string]bool{}
	var previous time.Time
	for i, run := range record.Tests {
		body, err := validateRun(run, record.Spec.Tests[i], files, previous)
		if err != nil {
			return nil, fmt.Errorf("correctness tests: %w", err)
		}
		previous = run.Finished
		for line := range strings.SplitSeq(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "ok" {
				passed[fields[1]] = true
			}
		}
	}
	for _, testCase := range record.Spec.Cases {
		if !passed[testCase.Package] {
			return nil, fmt.Errorf("no successful correctness tests for %s", testCase.Package)
		}
	}
	warmup, err := validateRun(record.Warmup, record.Spec.BenchmarkArgs(true), files, previous)
	if err != nil {
		return nil, fmt.Errorf("benchmark preparation: %w", err)
	}
	warmRecord := record
	warmRecord.Spec.Kind, warmRecord.Spec.Samples, warmRecord.Spec.Benchtime = "build", 1, "1x"
	warmRecord.Benchmark = record.Warmup
	if _, _, err := readMeasurements(warmRecord, warmup); err != nil {
		return nil, fmt.Errorf("benchmark preparation: %w", err)
	}
	body, err := validateRun(record.Benchmark,
		record.Spec.BenchmarkArgs(false), files, record.Warmup.Finished)
	if err != nil {
		return nil, fmt.Errorf("benchmark: %w", err)
	}
	if !bytes.Contains(body, []byte("\nPASS\n")) {
		return nil, fmt.Errorf("benchmark output does not record success")
	}
	values, elapsed, err := readMeasurements(record, body)
	if err != nil {
		return nil, err
	}
	if record.Benchmark.Finished.Sub(record.Benchmark.Started).Seconds() < elapsed*0.8 {
		return nil, fmt.Errorf("recorded benchmark duration is shorter than its raw measurements")
	}
	return values, nil
}

func validateRun(
	run Run, args []string, files map[string][]byte, previous time.Time,
) ([]byte, error) {
	if run.ExitCode != 0 || run.Error != "" || !slices.Equal(run.Args, args) || run.Directory != "." {
		return nil, fmt.Errorf("unsuccessful run or mismatched command")
	}
	if run.Started.IsZero() || run.Started.Location() != time.UTC ||
		run.Finished.Location() != time.UTC || !run.Finished.After(run.Started) ||
		run.Started.Before(previous) {
		return nil, fmt.Errorf("missing or inconsistent UTC collection timestamps")
	}
	if !safePath(run.Output) || path.Base(run.Output) != run.Output || !fullDigest(run.Digest) {
		return nil, fmt.Errorf("invalid raw-output path or digest")
	}
	body, exists := files[run.Output]
	sum := sha256.Sum256(body)
	if !exists || hex.EncodeToString(sum[:]) != run.Digest {
		return nil, fmt.Errorf("missing or modified raw output %s", run.Output)
	}
	if regexp.MustCompile(`(?m)^(FAIL\b|--- FAIL:)`).Match(body) {
		return nil, fmt.Errorf("raw output %s records a failure", run.Output)
	}
	return body, nil
}

func readMeasurements(record Record, body []byte) (Measurements, float64, error) {
	expected := map[string]Case{}
	for _, testCase := range record.Spec.Cases {
		for _, cpu := range record.Spec.CPUs {
			name := testCase.Name
			if cpu != 1 {
				name += fmt.Sprintf("-%d", cpu)
			}
			expected[testCase.Package+":"+name] = testCase
		}
	}
	values := Measurements{}
	seen := map[string]int{}
	elapsed := 0.0
	reader := benchfmt.NewReader(bytes.NewReader(body), record.Benchmark.Output)
	for reader.Scan() {
		switch result := reader.Result().(type) {
		case *benchfmt.SyntaxError:
			return nil, 0, result
		case *benchfmt.UnitMetadata:
			return nil, 0, fmt.Errorf("raw unit metadata requires an explicit statistical policy")
		case *benchfmt.Result:
			pkg, name := result.GetConfig("pkg"), "Benchmark"+result.Name.String()
			key := pkg + ":" + name
			testCase, exists := expected[key]
			if !exists || result.Iters < 1 {
				return nil, 0, fmt.Errorf("unexpected benchmark case %s", key)
			}
			if result.GetConfig("goos") != record.Machine.GOOS ||
				result.GetConfig("goarch") != record.Machine.GOARCH ||
				strings.TrimSpace(result.GetConfig("cpu")) != record.Machine.CPUModel {
				return nil, 0, fmt.Errorf("benchmark %s used a different target or CPU", key)
			}
			cpu := 1
			for _, candidate := range record.Spec.CPUs {
				if candidate != 1 && strings.HasSuffix(name, fmt.Sprintf("-%d", candidate)) {
					cpu = candidate
				}
			}
			units := map[string]bool{}
			for _, measurement := range result.Values {
				unit, value := measurement.Unit, measurement.Value
				if !slices.Contains(testCase.Units, unit) || units[unit] ||
					!finite(value) || value < 0 || unit == "sec/op" && value == 0 {
					return nil, 0, fmt.Errorf("invalid or unexpected measurement %s in %s", unit, key)
				}
				units[unit] = true
				if fixed, exists := testCase.Fixed[unit]; exists && value != fixed {
					return nil, 0, fmt.Errorf("workload count %s changed in %s", unit, key)
				}
				values[Key{pkg, testCase.Name, cpu, unit}] = append(
					values[Key{pkg, testCase.Name, cpu, unit}], value)
			}
			if len(units) != len(testCase.Units) {
				return nil, 0, fmt.Errorf("missing measurement units in %s", key)
			}
			seconds, _ := result.Value("sec/op")
			duration := seconds * float64(result.Iters)
			if record.Spec.Kind == "micro" {
				target, _ := time.ParseDuration(record.Spec.Benchtime)
				if duration < target.Seconds()*0.8 {
					return nil, 0, fmt.Errorf("benchmark %s has a shorter sample than declared", key)
				}
			} else if result.Iters != 1 {
				return nil, 0, fmt.Errorf("build sample %s is not an independent 1x run", key)
			}
			elapsed += duration
			seen[key]++
		}
	}
	if err := reader.Err(); err != nil {
		return nil, 0, err
	}
	for _, key := range slices.Sorted(maps.Keys(expected)) {
		if seen[key] != record.Spec.Samples {
			return nil, 0, fmt.Errorf("case %s has %d samples; expected %d",
				key, seen[key], record.Spec.Samples)
		}
	}
	return values, elapsed, nil
}

func fullRevision(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 20 && strings.ToLower(value) == value
}

func fullDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}
