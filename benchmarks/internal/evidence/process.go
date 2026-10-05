package evidence

import (
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

func CommandRunner() Runner {
	return func(ctx context.Context, dir string, args []string, output string) (Run, error) {
		return runProcess(ctx, dir, args, args, output)
	}
}

func BenchstatRunner(ctx context.Context, toolDir string) (Runner, error) {
	module, err := readRegular(filepath.Join(toolDir, "go.mod"))
	if err != nil || !strings.Contains(string(module), BenchstatVersion) {
		return nil, fmt.Errorf("comparison-tool module is missing its exact version pin")
	}
	body, err := commandOutput(ctx, toolDir, "go", "tool", "-n", "benchstat")
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(string(body))
	if strings.HasPrefix(path, `"`) {
		path, err = strconv.Unquote(path)
		if err != nil {
			return nil, err
		}
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if info.Path != "golang.org/x/perf/cmd/benchstat" || info.Main.Version != BenchstatVersion {
		return nil, fmt.Errorf("comparison executable does not match its version pin")
	}
	return func(ctx context.Context, dir string, args []string, output string) (Run, error) {
		if !slices.Equal(args, comparisonArgs()) {
			return Run{}, fmt.Errorf("unexpected comparison command")
		}
		actual := append([]string{path}, args[3:]...)
		return runProcess(ctx, dir, actual, args, output)
	}, nil
}

func runProcess(
	ctx context.Context, dir string, actual, recorded []string, output string,
) (Run, error) {
	if len(actual) == 0 {
		return Run{}, fmt.Errorf("empty collection command")
	}
	path := output
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		Args: slices.Clone(recorded), Directory: ".", Started: time.Now().UTC(),
		Output: filepath.Base(path), ExitCode: -1,
	}
	command := exec.CommandContext(ctx, actual[0], actual[1:]...)
	command.Dir, command.Stdout, command.Stderr = dir, file, file
	runErr := command.Run()
	run.Finished = time.Now().UTC()
	if command.ProcessState != nil {
		run.ExitCode = command.ProcessState.ExitCode()
	}
	err = errors.Join(runErr, file.Close(), ctx.Err())
	body, readErr := os.ReadFile(path)
	run.Digest = contentDigest(body)
	err = errors.Join(err, readErr)
	if err != nil {
		run.Error = err.Error()
		return run, fmt.Errorf("collection command %v: %w", recorded, err)
	}
	return run, nil
}

func commandOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w", name, args, errors.Join(err, ctx.Err()))
	}
	return output, nil
}

func gitBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return commandOutput(ctx, dir, "git", args...)
}

func gitText(ctx context.Context, dir string, args ...string) (string, error) {
	body, err := gitBytes(ctx, dir, args...)
	return strings.TrimSpace(string(body)), err
}

func cpuModel(ctx context.Context) (string, error) {
	switch runtime.GOOS {
	case "linux":
		body, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			return "", err
		}
		for line := range strings.SplitSeq(string(body), "\n") {
			key, value, _ := strings.Cut(line, ":")
			if strings.TrimSpace(key) == "model name" {
				return strings.TrimSpace(value), nil
			}
		}
	case "darwin":
		body, err := commandOutput(ctx, "", "sysctl", "-n", "machdep.cpu.brand_string")
		return strings.TrimSpace(string(body)), err
	}
	return "", fmt.Errorf("cannot read the CPU model on %s/%s", runtime.GOOS, runtime.GOARCH)
}
