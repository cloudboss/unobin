package diagnostic

import (
	"cmp"
	"slices"
)

type LibraryCompatibilityDetails struct {
	Dependency             string                   `json:"dependency,omitempty" ub:"dependency,omitempty"`
	Package                string                   `json:"package,omitempty" ub:"package,omitempty"`
	ModulePath             string                   `json:"module-path,omitempty" ub:"module-path,omitempty"`
	Version                string                   `json:"version,omitempty" ub:"version,omitempty"`
	Commit                 string                   `json:"commit,omitempty" ub:"commit,omitempty"`
	ActualVersion          string                   `json:"actual-version,omitempty" ub:"actual-version,omitempty"`
	ActualRequiredAPI      string                   `json:"actual-required-api,omitempty" ub:"actual-required-api,omitempty"`
	CandidateVersion       string                   `json:"candidate-version,omitempty" ub:"candidate-version,omitempty"`
	Query                  string                   `json:"query,omitempty" ub:"query,omitempty"`
	Floor                  string                   `json:"floor,omitempty" ub:"floor,omitempty"`
	RequiredAPI            string                   `json:"required-api,omitempty" ub:"required-api,omitempty"`
	ImplementedAPIs        []string                 `json:"implemented-apis,omitempty" ub:"implemented-apis,omitempty"`
	UnobinVersion          string                   `json:"unobin-version,omitempty" ub:"unobin-version,omitempty"`
	SuggestedUnobinVersion string                   `json:"suggested-unobin-version,omitempty" ub:"suggested-unobin-version,omitempty"`
	RequiredCoreVersion    string                   `json:"required-core-version,omitempty" ub:"required-core-version,omitempty"`
	MinimumGoVersion       string                   `json:"minimum-go-version,omitempty" ub:"minimum-go-version,omitempty"`
	Replacement            string                   `json:"replacement,omitempty" ub:"replacement,omitempty"`
	ActualReplacement      string                   `json:"actual-replacement,omitempty" ub:"actual-replacement,omitempty"`
	RequirementChain       []LibraryRequirementStep `json:"requirement-chain,omitempty" ub:"requirement-chain,omitempty"`
}

type LibraryRequirementStep struct {
	Dependency     string `json:"dependency" ub:"dependency"`
	Version        string `json:"version,omitempty" ub:"version,omitempty"`
	Requires       string `json:"requires" ub:"requires"`
	MinimumVersion string `json:"minimum-version" ub:"minimum-version"`
}

func compareLibraryCompatibility(a, b *LibraryCompatibilityDetails) int {
	if a == nil || b == nil {
		return comparePresence(a != nil, b != nil, false)
	}
	for _, pair := range [][2]string{
		{a.Dependency, b.Dependency}, {a.Package, b.Package}, {a.ModulePath, b.ModulePath},
		{a.Version, b.Version}, {a.Commit, b.Commit}, {a.ActualVersion, b.ActualVersion},
		{a.ActualRequiredAPI, b.ActualRequiredAPI}, {a.CandidateVersion, b.CandidateVersion},
		{a.Query, b.Query}, {a.Floor, b.Floor}, {a.RequiredAPI, b.RequiredAPI},
		{a.UnobinVersion, b.UnobinVersion}, {a.SuggestedUnobinVersion, b.SuggestedUnobinVersion},
		{a.RequiredCoreVersion, b.RequiredCoreVersion}, {a.MinimumGoVersion, b.MinimumGoVersion},
		{a.Replacement, b.Replacement}, {a.ActualReplacement, b.ActualReplacement},
	} {
		if n := cmp.Compare(pair[0], pair[1]); n != 0 {
			return n
		}
	}
	if n := slices.Compare(a.ImplementedAPIs, b.ImplementedAPIs); n != 0 {
		return n
	}
	return slices.CompareFunc(a.RequirementChain, b.RequirementChain, func(
		a, b LibraryRequirementStep,
	) int {
		for _, pair := range [][2]string{
			{a.Dependency, b.Dependency}, {a.Version, b.Version},
			{a.Requires, b.Requires}, {a.MinimumVersion, b.MinimumVersion},
		} {
			if n := cmp.Compare(pair[0], pair[1]); n != 0 {
				return n
			}
		}
		return 0
	})
}
