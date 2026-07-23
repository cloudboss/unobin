package diagnostic

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type PathMapping struct {
	AbsoluteRoot string
	DisplayRoot  string
}

type PathMapper struct {
	WorkingDir  string
	ProjectDir  string
	Mappings    []PathMapping
	HiddenRoots []string
}

func (m PathMapper) Display(path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path))
	}
	clean := filepath.Clean(path)
	if match, ok := bestMapping(clean, m.WorkingDir, m.Mappings); ok {
		return appendDisplayPath(match.display, match.relative)
	}
	if relative, ok := bestRelative(clean, pathAliases(m.ProjectDir, m.WorkingDir)); ok {
		if relative == "" {
			return "."
		}
		return filepath.ToSlash(relative)
	}
	for _, hidden := range m.HiddenRoots {
		if _, ok := bestRelative(clean, pathAliases(hidden, m.WorkingDir)); ok {
			return filepath.ToSlash(filepath.Base(clean))
		}
	}
	return filepath.ToSlash(filepath.Base(clean))
}

func (m PathMapper) ReplaceKnownPrefixes(message string) string {
	candidates := m.replacementCandidates()
	if len(candidates) == 0 || message == "" {
		return message
	}
	var out strings.Builder
	out.Grow(len(message))
	for i := 0; i < len(message); {
		matched := false
		for _, candidate := range candidates {
			if !strings.HasPrefix(message[i:], candidate.root) {
				continue
			}
			end := i + len(candidate.root)
			if !messageBoundaryBefore(message, i) || !messageBoundaryAfter(message, end) {
				continue
			}
			display := filepath.ToSlash(candidate.display)
			if display == "." && end < len(message) && os.IsPathSeparator(message[end]) {
				end++
				display = ""
			}
			out.WriteString(display)
			i = end
			matched = true
			break
		}
		if matched {
			continue
		}
		out.WriteByte(message[i])
		i++
	}
	return out.String()
}

type mappingMatch struct {
	display  string
	relative string
	rootLen  int
	order    int
}

func bestMapping(path, workingDir string, mappings []PathMapping) (mappingMatch, bool) {
	best := mappingMatch{}
	found := false
	for order, mapping := range mappings {
		for _, root := range pathAliases(mapping.AbsoluteRoot, workingDir) {
			relative, ok := relativeToRoot(path, root)
			if !ok {
				continue
			}
			candidate := mappingMatch{
				display: mapping.DisplayRoot, relative: relative,
				rootLen: len(root), order: order,
			}
			if !found || candidate.rootLen > best.rootLen ||
				(candidate.rootLen == best.rootLen && candidate.order < best.order) {
				best = candidate
				found = true
			}
		}
	}
	return best, found
}

func bestRelative(path string, roots []string) (string, bool) {
	best := ""
	bestLen := -1
	for _, root := range roots {
		relative, ok := relativeToRoot(path, root)
		if ok && len(root) > bestLen {
			best = relative
			bestLen = len(root)
		}
	}
	return best, bestLen >= 0
}

func relativeToRoot(path, root string) (string, bool) {
	if path == "" || root == "" {
		return "", false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if relative == "." {
		return "", true
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

func pathAliases(path, workingDir string) []string {
	if path == "" {
		return nil
	}
	clean := path
	if !filepath.IsAbs(clean) {
		if workingDir != "" {
			clean = filepath.Join(workingDir, clean)
		} else if absolute, err := filepath.Abs(clean); err == nil {
			clean = absolute
		}
	}
	clean = filepath.Clean(clean)
	aliases := []string{clean}
	if target, err := os.Readlink(clean); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(clean), target)
		}
		target = filepath.Clean(target)
		if !slices.Contains(aliases, target) {
			aliases = append(aliases, target)
		}
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != clean {
		resolved = filepath.Clean(resolved)
		if !slices.Contains(aliases, resolved) {
			aliases = append(aliases, resolved)
		}
	}
	return aliases
}

func appendDisplayPath(root, relative string) string {
	root = filepath.ToSlash(root)
	relative = filepath.ToSlash(relative)
	if relative == "" {
		if root == "" {
			return "."
		}
		return root
	}
	if root == "" || root == "." {
		return relative
	}
	return strings.TrimSuffix(root, "/") + "/" + relative
}

type replacementCandidate struct {
	root    string
	display string
	order   int
}

func (m PathMapper) replacementCandidates() []replacementCandidate {
	var candidates []replacementCandidate
	order := 0
	add := func(root, display string) {
		for _, alias := range pathAliases(root, m.WorkingDir) {
			candidates = append(candidates, replacementCandidate{
				root: alias, display: display, order: order,
			})
		}
		order++
	}
	for _, mapping := range m.Mappings {
		add(mapping.AbsoluteRoot, mapping.DisplayRoot)
	}
	if m.ProjectDir != "" {
		add(m.ProjectDir, ".")
	}
	for _, hidden := range m.HiddenRoots {
		add(hidden, filepath.Base(filepath.Clean(hidden)))
	}
	candidates = deduplicateCandidates(candidates)
	slices.SortStableFunc(candidates, func(a, b replacementCandidate) int {
		if n := cmp.Compare(len(b.root), len(a.root)); n != 0 {
			return n
		}
		return cmp.Compare(a.order, b.order)
	})
	return candidates
}

func deduplicateCandidates(in []replacementCandidate) []replacementCandidate {
	out := make([]replacementCandidate, 0, len(in))
	seen := map[string]bool{}
	for _, candidate := range in {
		key := candidate.root + "\x00" + candidate.display
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

func messageBoundaryBefore(message string, index int) bool {
	return index == 0 || !pathByte(message[index-1])
}

func messageBoundaryAfter(message string, index int) bool {
	if index == len(message) {
		return true
	}
	if os.IsPathSeparator(message[index]) {
		return true
	}
	return !pathByte(message[index])
}

func pathByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		strings.ContainsRune("_-.~@/\\", rune(value))
}
