package deps

import (
	"maps"

	"golang.org/x/mod/semver"
)

// Selection performs minimal version selection incrementally. Add records
// that some project requires a dependency at a floor version, and
// Selection keeps the highest floor seen for each dependency -- Go's
// max-of-minimums. A walker feeds it every requirement across the
// dependency graph; the highest floor per dependency is the selected
// version. Floors must be valid semver, which the project reader
// guarantees.
type Selection struct {
	chosen map[Dependency]string
}

// NewSelection returns an empty Selection.
func NewSelection() *Selection {
	return &Selection{chosen: map[Dependency]string{}}
}

// Add records a required floor for dep and reports whether it raised the
// selected version: true the first time dep is seen, or when floor is
// higher than the current selection. A walker uses the result to decide
// whether it must read dep's project at the new version, since a higher
// version may declare different requirements.
func (s *Selection) Add(dep Dependency, floor string) bool {
	current, seen := s.chosen[dep]
	if seen && semver.Compare(floor, current) <= 0 {
		return false
	}
	s.chosen[dep] = floor
	return true
}

// Version returns the selected version for dep, or "" if dep has not been
// added.
func (s *Selection) Version(dep Dependency) string {
	return s.chosen[dep]
}

// Chosen returns a copy of the selected version for every dependency
// added so far.
func (s *Selection) Chosen() map[Dependency]string {
	return maps.Clone(s.chosen)
}
