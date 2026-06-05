package runner

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/cloudboss/unobin/pkg/toolchain"
)

// readBuildInfo is swapped by tests to exercise the version check
// without a real build.
var readBuildInfo = debug.ReadBuildInfo

// verifyLinkedUnobin compares the unobin module this binary links
// against the version the compiling CLI pinned into it, refusing to
// run when they differ: the compile-time checks were made by one
// runtime and the binary would execute another. A replaced module is
// the development escape and proceeds with a notice; a binary with no
// stamped expectation (built outside the CLI) checks nothing.
func verifyLinkedUnobin(expected string) error {
	if expected == "" {
		return nil
	}
	info, ok := readBuildInfo()
	if !ok {
		return nil
	}
	notice, err := decideLinkedUnobin(info, expected)
	if err != nil {
		return err
	}
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
	return nil
}

// decideLinkedUnobin applies the version rule to one build info.
func decideLinkedUnobin(bi *debug.BuildInfo, expected string) (string, error) {
	for _, dep := range bi.Deps {
		if dep.Path != toolchain.UnobinModulePath {
			continue
		}
		if dep.Replace != nil {
			return fmt.Sprintf("notice: %s is replaced; this factory runs %s, not %s",
				toolchain.UnobinModulePath, dep.Replace.Path, expected), nil
		}
		if dep.Version != expected {
			return "", fmt.Errorf(
				"this factory was compiled against %s %s but links %s;"+
					" rebuild it with the matching unobin",
				toolchain.UnobinModulePath, expected, dep.Version)
		}
		return "", nil
	}
	return "", nil
}
