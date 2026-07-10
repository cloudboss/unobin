package runner

import (
	"fmt"
	"runtime/debug"

	"github.com/cloudboss/unobin/pkg/toolchain"
)

// readBuildInfo is swapped by tests to exercise the version check
// without a real build.
var readBuildInfo = debug.ReadBuildInfo

// linkedUnobinStatus compares the linked module with the version pinned by the compiler.
func linkedUnobinStatus(expected string) (string, error) {
	if expected == "" {
		return "", nil
	}
	info, ok := readBuildInfo()
	if !ok {
		return "", nil
	}
	return decideLinkedUnobin(info, expected)
}

// decideLinkedUnobin applies the version rule to one build info.
func decideLinkedUnobin(bi *debug.BuildInfo, expected string) (string, error) {
	for _, dep := range bi.Deps {
		if dep.Path != toolchain.UnobinModulePath {
			continue
		}
		if dep.Replace != nil {
			return fmt.Sprintf("%s is replaced; this factory runs %s, not %s",
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
