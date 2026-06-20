package e2etest

import (
	"regexp"
	"strings"
)

var (
	contentRevisionTextRE  = regexp.MustCompile(`content-revision [0-9a-f]{12}`)
	contentRevisionFieldRE = regexp.MustCompile(`content-revision: '[0-9a-f]{12}'`)
	eventTimeRE            = regexp.MustCompile(`time: '[0-9:]+(?:\.[0-9]+)?'`)
	elapsedRE              = regexp.MustCompile(`elapsed: '[^']+'`)
)

func normalizeCommandResult(result CommandResult, repoRoot string) CommandResult {
	result.Stdout = normalizeDynamicText(result.Stdout, repoRoot)
	result.Stderr = normalizeDynamicText(result.Stderr, repoRoot)
	return result
}

func normalizeDynamicText(s string, repoRoot string) string {
	s = contentRevisionTextRE.ReplaceAllString(s, "content-revision <revision>")
	s = contentRevisionFieldRE.ReplaceAllString(s, "content-revision: '<revision>'")
	s = eventTimeRE.ReplaceAllString(s, "time: '<time>'")
	s = elapsedRE.ReplaceAllString(s, "elapsed: '<elapsed>'")
	return strings.ReplaceAll(s, repoRoot, "<repo>")
}
