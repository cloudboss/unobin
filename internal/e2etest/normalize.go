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
	stateRevisionLineRE    = regexp.MustCompile(
		`(?m)^([* ] )[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z(?:_[0-9]+)?$`,
	)
)

func normalizeCommandResult(result CommandResult, repoRoot string) CommandResult {
	result.Stdout = normalizeDynamicText(result.Stdout, repoRoot)
	result.Stderr = normalizeDynamicText(result.Stderr, repoRoot)
	return result
}

func normalizeWorkspaceResult(result CommandResult, workspace string) CommandResult {
	result.Stdout = strings.ReplaceAll(result.Stdout, workspace, "<workspace>")
	result.Stderr = strings.ReplaceAll(result.Stderr, workspace, "<workspace>")
	return result
}

func normalizeFileResults(
	results map[string]string,
	repoRoot string,
	workspace string,
) map[string]string {
	out := make(map[string]string, len(results))
	for path, content := range results {
		content = normalizeDynamicText(content, repoRoot)
		content = strings.ReplaceAll(content, workspace, "<workspace>")
		out[path] = content
	}
	return out
}

func normalizeDynamicText(s string, repoRoot string) string {
	s = contentRevisionTextRE.ReplaceAllString(s, "content-revision <revision>")
	s = contentRevisionFieldRE.ReplaceAllString(s, "content-revision: '<revision>'")
	s = eventTimeRE.ReplaceAllString(s, "time: '<time>'")
	s = elapsedRE.ReplaceAllString(s, "elapsed: '<elapsed>'")
	s = stateRevisionLineRE.ReplaceAllString(s, "${1}<revision>")
	if repoRoot != "" {
		s = strings.ReplaceAll(s, repoRoot, "<repo>")
	}
	return s
}
