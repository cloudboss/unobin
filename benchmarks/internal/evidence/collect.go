package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

type CollectionOptions struct {
	Repository      string
	SpecPath        string
	OutDir          string
	FixtureRevision string
	Capabilities    []string
}

func Collect(ctx context.Context, options CollectionOptions) (Record, error) {
	repository, err := filepath.Abs(options.Repository)
	if err != nil {
		return Record{}, err
	}
	specPath := options.SpecPath
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(repository, specPath)
	}
	rel, err := filepath.Rel(repository, specPath)
	if err != nil || !safePath(filepath.ToSlash(rel)) {
		return Record{}, fmt.Errorf("workload specification must be inside the repository")
	}
	var spec Spec
	if err := readJSON(specPath, &spec); err != nil {
		return Record{}, err
	}
	if err := spec.Validate(); err != nil {
		return Record{}, err
	}
	revision, err := gitText(ctx, repository, "rev-parse", "HEAD")
	if err != nil {
		return Record{}, err
	}
	paths := append(slices.Clone(spec.Fixtures), filepath.ToSlash(rel))
	if err := cleanSource(ctx, repository, paths); err != nil {
		return Record{}, err
	}
	fixtureRevision := options.FixtureRevision
	if fixtureRevision == "" {
		fixtureRevision = revision
	}
	fixtures, err := inputDigests(ctx, repository, revision, paths, true)
	if err != nil {
		return Record{}, err
	}
	prepared, err := inputDigests(ctx, repository, fixtureRevision, paths, false)
	if err != nil {
		return Record{}, err
	}
	if !maps.Equal(fixtures, prepared) {
		return Record{}, fmt.Errorf("benchmark fixtures differ from their preparation revision")
	}
	dependencies, err := dependencyDigests(ctx, repository, revision)
	if err != nil {
		return Record{}, err
	}
	environment, err := goEnvironment(ctx, repository)
	if err != nil {
		return Record{}, err
	}
	cpu, err := cpuModel(ctx)
	if err != nil {
		return Record{}, err
	}
	out := options.OutDir
	if !filepath.IsAbs(out) {
		out = filepath.Join(repository, out)
	}
	if err := createDirectory(out); err != nil {
		return Record{}, err
	}
	record := Record{
		FormatVersion: FormatVersion, Spec: spec, Revision: revision, FixtureRevision: fixtureRevision,
		SpecPath:   filepath.ToSlash(rel),
		Repository: repository, CleanSource: true, Fixtures: fixtures, Dependencies: dependencies,
		Machine: Machine{
			GoVersion: environment["GOVERSION"], GOOS: environment["GOOS"], GOARCH: environment["GOARCH"],
			CPUModel: cpu, LogicalCPUs: runtime.NumCPU(),
		},
		Environment: environment, Benchstat: BenchstatVersion,
		Capabilities: slices.Sorted(slices.Values(options.Capabilities)), Tests: []Run{},
	}
	runner := CommandRunner()
	run := func(args []string, name string) (Run, error) {
		return runner(ctx, repository, args, filepath.Join(out, name))
	}
	finish := func(cause error) (Record, error) {
		if err := cleanSource(ctx, repository, paths); err != nil {
			record.CleanSource = false
			cause = errors.Join(cause, err)
		}
		current, err := gitText(ctx, repository, "rev-parse", "HEAD")
		if err != nil || current != revision {
			record.CleanSource = false
			cause = errors.Join(cause, err, fmt.Errorf("source revision changed during collection"))
		}
		cause = errors.Join(cause, SaveRecord(out, record))
		if cause == nil {
			_, files, err := LoadRecord(out)
			if err != nil {
				return record, err
			}
			_, cause = Validate(record, files)
		}
		return record, cause
	}
	for i, args := range spec.Tests {
		result, err := run(args, fmt.Sprintf("test-%02d.txt", i+1))
		record.Tests = append(record.Tests, result)
		if err != nil {
			return finish(err)
		}
	}
	record.Warmup, err = run(spec.BenchmarkArgs(true), "warmup.txt")
	if err != nil {
		return finish(err)
	}
	record.Benchmark, err = run(spec.BenchmarkArgs(false), "benchmarks.txt")
	return finish(err)
}

func AuditRecord(ctx context.Context, repository string, record Record) error {
	if !safePath(record.SpecPath) {
		return fmt.Errorf("invalid workload declaration path")
	}
	for _, revision := range []string{record.Revision, record.FixtureRevision} {
		if !fullRevision(revision) {
			return fmt.Errorf("evidence requires full commit IDs")
		}
		selected, err := gitText(ctx, repository, "rev-parse", revision+"^{commit}")
		if err != nil {
			return fmt.Errorf("evidence revision %s is unavailable: %w", revision, err)
		}
		if selected != revision {
			return fmt.Errorf("evidence revision %s is not a commit", revision)
		}
	}
	for _, revision := range []string{record.Revision, record.FixtureRevision} {
		paths := append(slices.Clone(record.Spec.Fixtures), record.SpecPath)
		inputs, err := inputDigests(ctx, repository, revision, paths, false)
		if err != nil {
			return err
		}
		if !maps.Equal(inputs, record.Fixtures) {
			return fmt.Errorf("fixture inventory does not match revision %s", revision)
		}
		body, err := gitBytes(ctx, repository, "show", revision+":"+record.SpecPath)
		if err != nil {
			return err
		}
		var declared Spec
		if err := decodeJSON(body, &declared); err != nil {
			return fmt.Errorf("committed workload declaration: %w", err)
		}
		if !reflect.DeepEqual(declared, record.Spec) {
			return fmt.Errorf("workload declaration does not match revision %s", revision)
		}
	}
	for path, digest := range record.Dependencies {
		if !safePath(path) {
			return fmt.Errorf("invalid dependency input path %q", path)
		}
		body, err := gitBytes(ctx, repository, "show", record.Revision+":"+path)
		if err != nil || contentDigest(body) != digest {
			return fmt.Errorf("dependency input %s does not match the recorded revision", path)
		}
	}
	return nil
}

func AuditComparison(ctx context.Context, repository, dir string) error {
	var metadata Comparison
	if err := readJSON(filepath.Join(dir, "metadata.json"), &metadata); err != nil {
		return err
	}
	return errors.Join(AuditRecord(ctx, repository, metadata.Before),
		AuditRecord(ctx, repository, metadata.After))
}

func cleanSource(ctx context.Context, repository string, fixtures []string) error {
	if _, err := gitBytes(ctx, repository, "diff", "--exit-code", "HEAD", "--"); err != nil {
		return fmt.Errorf("tracked source must be clean at the measured revision: %w", err)
	}
	status, err := gitBytes(ctx, repository, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	for name := range bytes.SplitSeq(status, []byte{0}) {
		path := string(name)
		if path == "" {
			continue
		}
		for _, fixture := range fixtures {
			if path == fixture || strings.HasPrefix(path, fixture+"/") {
				return fmt.Errorf("untracked fixture %s changes the measured inputs", path)
			}
		}
		ext := filepath.Ext(path)
		if ext == ".go" || ext == ".ub" || ext == ".mod" || ext == ".sum" {
			for _, prefix := range []string{"pkg/", "internal/", "cmd/", "tests/", "benchmarks/"} {
				if strings.HasPrefix(path, prefix) && !strings.HasPrefix(path, "benchmarks/results/") {
					return fmt.Errorf("untracked source %s changes the measured revision", path)
				}
			}
		}
	}
	return nil
}

func inputDigests(
	ctx context.Context, repository, revision string, roots []string, checkLive bool,
) (map[string]string, error) {
	if !fullRevision(revision) {
		return nil, fmt.Errorf("benchmark preparation requires a full commit ID")
	}
	inputs := map[string]string{}
	for _, root := range roots {
		if !safePath(root) {
			return nil, fmt.Errorf("invalid declared input path %q", root)
		}
		paths, err := gitBytes(ctx, repository,
			"ls-tree", "-r", "--name-only", "-z", revision, "--", root)
		if err != nil || len(paths) == 0 {
			return nil, fmt.Errorf("fixture %s is missing from revision %s", root, revision)
		}
		for name := range bytes.SplitSeq(paths, []byte{0}) {
			if len(name) == 0 {
				continue
			}
			path := string(name)
			body, err := gitBytes(ctx, repository, "show", revision+":"+path)
			if err != nil {
				return nil, err
			}
			inputs[path] = contentDigest(body)
			if checkLive {
				live, err := readRegular(filepath.Join(repository, filepath.FromSlash(path)))
				if err != nil || !bytes.Equal(live, body) {
					return nil, fmt.Errorf("fixture %s differs from the measured revision", path)
				}
			}
		}
	}
	return inputs, nil
}

func dependencyDigests(
	ctx context.Context, repository, revision string,
) (map[string]string, error) {
	inputs := map[string]string{}
	for _, path := range []string{"go.mod", "go.sum"} {
		body, err := readRegular(filepath.Join(repository, path))
		if errors.Is(err, os.ErrNotExist) && path == "go.sum" {
			continue
		}
		if err != nil {
			return nil, err
		}
		committed, err := gitBytes(ctx, repository, "show", revision+":"+path)
		if err != nil || !bytes.Equal(committed, body) {
			return nil, fmt.Errorf("dependency input %s differs from the measured revision", path)
		}
		inputs[path] = contentDigest(body)
	}
	return inputs, nil
}

func goEnvironment(ctx context.Context, repository string) (map[string]string, error) {
	args := []string{
		"env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT",
		"GOAMD64", "GO386", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64",
		"GOWASM", "GOTOOLCHAIN", "GOWORK", "GOCACHE", "GOMODCACHE", "GOROOT", "GOTMPDIR",
	}
	body, err := commandOutput(ctx, repository, "go", args...)
	if err != nil {
		return nil, err
	}
	var environment map[string]string
	if err := json.Unmarshal(body, &environment); err != nil {
		return nil, err
	}
	for _, key := range []string{
		"GODEBUG", "GOMAXPROCS", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS",
		"CGO_LDFLAGS", "UNOBIN_BENCH_PROFILE",
	} {
		environment[key] = os.Getenv(key)
	}
	return environment, nil
}
