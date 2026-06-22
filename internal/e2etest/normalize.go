package e2etest

import (
	"path/filepath"
	"regexp"
	"sort"
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
	localStoreOpenRevRE = regexp.MustCompile(
		`local store: open [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z(?:_[0-9]+)?:`,
	)
	stateRevTextRE = regexp.MustCompile(
		`State rev: [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z(?:_[0-9]+)?`,
	)
	runViewURLRE      = regexp.MustCompile(`Run view: http://127\.0\.0\.1:\d+/[0-9a-f]{32}/`)
	uiEventTimeRE     = regexp.MustCompile(`(?m)^\[[0-9:]+\]`)
	uiEventDurationRE = regexp.MustCompile(`\([0-9]+(?:\.[0-9]+)?(?:ms|s)\)`)
)

func normalizeCommandResult(result CommandResult, repoRoot string) CommandResult {
	result.Stdout = normalizeDynamicText(result.Stdout, repoRoot)
	result.Stderr = normalizeDynamicText(result.Stderr, repoRoot)
	return result
}

func normalizeWorkspaceResult(result CommandResult, workspace string) CommandResult {
	result.Stdout = normalizeWorkspaceText(result.Stdout, workspace)
	result.Stderr = normalizeWorkspaceText(result.Stderr, workspace)
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
		content = normalizeWorkspaceText(content, workspace)
		out[path] = content
	}
	return out
}

func normalizeWorkspaceText(s string, workspace string) string {
	for _, alias := range workspaceAliases(workspace) {
		s = strings.ReplaceAll(s, alias, "<workspace>")
	}
	return s
}

func workspaceAliases(workspace string) []string {
	seen := map[string]bool{}
	var aliases []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		aliases = append(aliases, path)
	}
	add(workspace)
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		add(resolved)
	}
	for _, alias := range append([]string(nil), aliases...) {
		if strings.HasPrefix(alias, "/var/") {
			add("/private" + alias)
		}
		if strings.HasPrefix(alias, "/private/var/") {
			add(strings.TrimPrefix(alias, "/private"))
			add("/private" + alias)
		}
	}
	sort.Slice(aliases, func(i, j int) bool {
		return len(aliases[i]) > len(aliases[j])
	})
	return aliases
}

func normalizeDynamicText(s string, repoRoot string) string {
	s = contentRevisionTextRE.ReplaceAllString(s, "content-revision <revision>")
	s = contentRevisionFieldRE.ReplaceAllString(s, "content-revision: '<revision>'")
	s = eventTimeRE.ReplaceAllString(s, "time: '<time>'")
	s = elapsedRE.ReplaceAllString(s, "elapsed: '<elapsed>'")
	s = stateRevisionLineRE.ReplaceAllString(s, "${1}<revision>")
	s = localStoreOpenRevRE.ReplaceAllString(s, "local store: open <revision>:")
	s = stateRevTextRE.ReplaceAllString(s, "State rev: <revision>")
	s = runViewURLRE.ReplaceAllString(s, "Run view: <run-view>")
	s = uiEventTimeRE.ReplaceAllString(s, "[<time>]")
	s = uiEventDurationRE.ReplaceAllString(s, "(<elapsed>)")
	if repoRoot != "" {
		s = strings.ReplaceAll(s, repoRoot, "<repo>")
	}
	return s
}
