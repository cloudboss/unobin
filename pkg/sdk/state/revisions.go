package state

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
)

func SortRevisions(revisions []string) []string {
	result := slices.Clone(revisions)
	if result == nil {
		result = []string{}
	}
	slices.SortFunc(result, compareRevision)
	return result
}

type parsedRevision struct {
	time   time.Time
	suffix int
	valid  bool
}

func compareRevision(a, b string) int {
	parsedA := parseRevision(a)
	parsedB := parseRevision(b)
	if parsedA.valid != parsedB.valid {
		if parsedA.valid {
			return -1
		}
		return 1
	}
	if parsedA.valid {
		if byTime := parsedA.time.Compare(parsedB.time); byTime != 0 {
			return byTime
		}
		if bySuffix := cmp.Compare(parsedA.suffix, parsedB.suffix); bySuffix != 0 {
			return bySuffix
		}
	}
	return cmp.Compare(a, b)
}

func parseRevision(revision string) parsedRevision {
	base := revision
	suffix := 0
	if before, after, ok := strings.Cut(revision, "_"); ok {
		value, err := strconv.Atoi(after)
		if err != nil || value < 1 || strings.Contains(before, "_") {
			return parsedRevision{}
		}
		base = before
		suffix = value
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, base)
	if err != nil {
		return parsedRevision{}
	}
	return parsedRevision{time: parsedTime, suffix: suffix, valid: true}
}
