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

func CheckNoReplacementSentinelInLock(lock *Lock) error {
	if lock == nil {
		return nil
	}
	for id, dep := range lock.Deps {
		if dep != nil && IsReplacementSentinel(dep.Version) {
			return fmt.Errorf(
				"lock: dependency %q: %s is reserved for manifest replacements",
				id, ReplacementSentinel)
		}
	}
	return nil
}

func CheckReplacementSentinels(manifest *Manifest) error {
	if manifest == nil {
		return nil
	}
	for dep, req := range manifest.Requires {
		if !IsReplacementSentinel(req.Version) {
			continue
		}
		if _, ok := manifest.Replace[dep]; ok {
			continue
		}
		return fmt.Errorf(
			"manifest: dependency %s: %s is reserved for manifest replacements; "+
				"add an exact manifest.replace entry",
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
