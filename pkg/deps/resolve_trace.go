package deps

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
)

// RequirementStep records the source of a floor. An empty Dependency identifies the root.
type RequirementStep struct {
	Dependency     Dependency
	Version        string
	Requires       Dependency
	MinimumVersion string
}

type Resolution struct {
	Selection    map[Dependency]string
	Requirements []RequirementStep
	Chains       map[Dependency][]RequirementStep
}

func ResolveWithTrace(root *Project, fetch Fetcher) (*Resolution, error) {
	selection := NewSelection()
	requirements := []RequirementStep{}
	var queue []Dependency
	enqueue := func(declaring Dependency, version string, project *Project) {
		floors := project.RequireVersions()
		dependencies := slices.SortedFunc(maps.Keys(floors), func(a, b Dependency) int {
			return cmp.Compare(a.String(), b.String())
		})
		for _, dependency := range dependencies {
			floor := floors[dependency]
			requirements = append(requirements, RequirementStep{
				Dependency: declaring, Version: version,
				Requires: dependency, MinimumVersion: floor,
			})
			if selection.Add(dependency, floor) {
				queue = append(queue, dependency)
			}
		}
	}
	enqueue(Dependency{}, "", root)
	fetched := map[Dependency]string{}
	var failure error
	for len(queue) > 0 {
		dependency := queue[0]
		queue = queue[1:]
		version := selection.Version(dependency)
		if fetched[dependency] == version {
			continue
		}
		project, err := fetch.Fetch(dependency, version)
		if err != nil {
			failure = fmt.Errorf("resolve %s@%s: %w", dependency, version, err)
			break
		}
		fetched[dependency] = version
		if project != nil {
			enqueue(dependency, version, project)
		}
	}
	chosen := selection.Chosen()
	return &Resolution{
		Selection: chosen, Requirements: requirements, Chains: requirementChains(chosen, requirements),
	}, failure
}

type requirementNode struct {
	dependency Dependency
	version    string
}

func requirementChains(
	selection map[Dependency]string,
	requirements []RequirementStep,
) map[Dependency][]RequirementStep {
	paths := map[requirementNode][]RequirementStep{{}: {}}
	for changed := true; changed; {
		changed = false
		for _, step := range requirements {
			parent := requirementNode{dependency: step.Dependency, version: step.Version}
			path, reachable := paths[parent]
			if !reachable {
				continue
			}
			target := requirementNode{dependency: step.Requires, version: step.MinimumVersion}
			candidate := append(slices.Clone(path), step)
			current, exists := paths[target]
			if exists && compareRequirementChains(candidate, current) >= 0 {
				continue
			}
			paths[target] = candidate
			changed = true
		}
	}
	chains := make(map[Dependency][]RequirementStep, len(selection))
	for dependency, version := range selection {
		chains[dependency] = slices.Clone(paths[requirementNode{dependency, version}])
	}
	return chains
}

func compareRequirementChains(a, b []RequirementStep) int {
	if order := cmp.Compare(len(a), len(b)); order != 0 {
		return order
	}
	for i := len(a) - 1; i >= 0; i-- {
		for _, pair := range [][2]string{
			{a[i].Dependency.String(), b[i].Dependency.String()},
			{a[i].Version, b[i].Version},
			{a[i].Requires.String(), b[i].Requires.String()},
			{a[i].MinimumVersion, b[i].MinimumVersion},
		} {
			if order := cmp.Compare(pair[0], pair[1]); order != 0 {
				return order
			}
		}
	}
	return 0
}
