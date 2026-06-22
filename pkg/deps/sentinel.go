package deps

import (
	"fmt"
	"strconv"
	"strings"
)

const ReplacementSentinel = "v0.0.0-unobin-replaced"

func IsReplacementSentinel(v string) bool {
	return v == ReplacementSentinel
}

func CheckNoReplacementSentinelInProjectLock(projectLock *ProjectLock) error {
	if projectLock == nil {
		return nil
	}
	for id, dep := range projectLock.Deps {
		if dep != nil && IsReplacementSentinel(dep.Version) {
			return fmt.Errorf(
				"project-lock: dependency %q: %s is reserved for project replacements",
				id, ReplacementSentinel)
		}
	}
	return nil
}

func CheckReplacementSentinels(project *Project) error {
	if project == nil {
		return nil
	}
	for dep, req := range project.Requires {
		if !IsReplacementSentinel(req.Version) {
			continue
		}
		if _, ok := project.Replace[dep]; ok {
			continue
		}
		return fmt.Errorf(
			"project: dependency %s: %s is reserved for project replacements; "+
				"add an exact project.replace entry",
			dep, ReplacementSentinel)
	}
	return nil
}

func GoReplacementSentinel(modulePath string) (string, error) {
	major, ok, err := moduleMajor(modulePath)
	if err != nil {
		return "", err
	}
	if !ok || major < 2 {
		return ReplacementSentinel, nil
	}
	return fmt.Sprintf("v%d.0.0-unobin-replaced", major), nil
}

func moduleMajor(modulePath string) (int, bool, error) {
	last := modulePath
	if i := strings.LastIndex(modulePath, "/"); i >= 0 {
		last = modulePath[i+1:]
	}
	if len(last) < 2 || last[0] != 'v' {
		return 0, false, nil
	}
	for _, r := range last[1:] {
		if r < '0' || r > '9' {
			return 0, false, nil
		}
	}
	major, err := strconv.Atoi(last[1:])
	if err != nil {
		return 0, false, fmt.Errorf("module path %q: invalid major suffix %q", modulePath, last)
	}
	return major, true, nil
}
