package runtime

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

type ruleSettings struct {
	Tier     string `ub:"tier"`
	Replicas int    `ub:"replicas"`
}

type ruleInput struct {
	Name     string            `ub:"name"`
	Region   string            `ub:"region"`
	Labels   map[string]string `ub:"labels"`
	Settings *ruleSettings     `ub:"settings"`
	Zones    []string          `ub:"zones"`
}

type ruleOutput struct {
	ID     string      `ub:"id"`
	ETag   string      `ub:"etag"`
	Status *ruleStatus `ub:"status"`
}

type ruleStatus struct {
	Code string `ub:"code"`
}

type ruleDefinition = ResourceDefinition[ruleInput, *ruleOutput, NoConfig]

func ruleInputFields() (
	InputDescriptor[ruleInput, string],
	InputDescriptor[ruleInput, string],
	InputDescriptor[ruleInput, map[string]string],
	InputDescriptor[ruleInput, *ruleSettings],
	InputDescriptor[ruleInput, string],
	InputDescriptor[ruleInput, int],
) {
	return InputField(func(v *ruleInput) *string { return &v.Name }),
		InputField(func(v *ruleInput) *string { return &v.Region }),
		InputField(func(v *ruleInput) *map[string]string { return &v.Labels }),
		InputField(func(v *ruleInput) **ruleSettings { return &v.Settings }),
		InputField(func(v *ruleInput) *string { return &v.Settings.Tier }),
		InputField(func(v *ruleInput) *int { return &v.Settings.Replicas })
}

func validRuleDefinition() ruleDefinition {
	name, region, labels, _, _, replicas := ruleInputFields()
	etag := OutputField(func(v *ruleOutput) *string { return &v.ETag })
	return ruleDefinition{
		SchemaVersion: 2,
		InputSemantics: InputSemantics[ruleInput]{
			Rules: []InputRule[ruleInput]{
				EqualBy(labels, equalStringMaps),
			},
		},
		Identity: ResourceIdentity[ruleInput, *ruleOutput]{
			Version:       1,
			Scope:         IdentityConfiguration,
			AddressInputs: []AnyInputField[ruleInput]{name},
			StableID: func(_ ruleInput, out *ruleOutput) (string, error) {
				return out.ID, nil
			},
		},
		Replacement: ReplacementRules[ruleInput, *ruleOutput]{
			Inputs: []ReplacementRule[ruleInput]{
				ReplaceWhenChanged(labels),
				ReplaceWhenChanged(region),
				ReplaceWhen(replicas, func(prior, desired int) bool {
					return desired > prior
				}),
			},
			Drift: []DriftRule[*ruleOutput]{
				ReplaceOnDrift(etag, func(recorded, observed string) bool {
					return recorded == observed
				}),
			},
		},
	}
}

func TestResolveResourceDefinition(t *testing.T) {
	got, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	require.Equal(t, 2, got.schemaVersion)
	require.Equal(t, 1, got.identityVersion)
	require.Equal(t, IdentityConfiguration, got.identityScope)
	require.NotNil(t, got.stableID)
	require.Equal(t, []string{"labels"}, inputRulePaths(got.inputRules))
	require.Equal(t, []string{"name"}, inputFieldPaths(got.addressInputs))
	require.Equal(
		t,
		[]string{"labels", "region", "settings.replicas"},
		replacementRulePaths(got.replacementInputs),
	)
	require.Equal(t, []string{"etag"}, driftRulePaths(got.driftRules))
}

func TestInputRuleUsesTypedCollectionEquality(t *testing.T) {
	got, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	rule := got.inputRules[0]

	equal, known, err := rule.equivalent(
		ruleInput{Labels: nil},
		ruleInput{Labels: map[string]string{}},
	)
	require.NoError(t, err)
	require.True(t, known)
	require.True(t, equal)

	equal, known, err = rule.equivalent(
		ruleInput{Labels: map[string]string{"env": "dev"}},
		ruleInput{Labels: map[string]string{"env": "prod"}},
	)
	require.NoError(t, err)
	require.True(t, known)
	require.False(t, equal)
}

func TestInputRuleSkipsInaccessibleNestedField(t *testing.T) {
	_, _, _, _, tier, _ := ruleInputFields()
	calls := 0
	definition := validRuleDefinition()
	definition.InputSemantics.Rules = []InputRule[ruleInput]{
		EqualBy(tier, func(prior, desired string) bool {
			calls++
			return prior == desired
		}),
	}
	got, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	equal, known, err := got.inputRules[0].equivalent(
		ruleInput{},
		ruleInput{Settings: &ruleSettings{Tier: "standard"}},
	)
	require.NoError(t, err)
	require.False(t, known)
	require.False(t, equal)
	require.Zero(t, calls)
}

func TestReplacementRulesUseSemanticEqualityBeforePredicates(t *testing.T) {
	got, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	prior := ruleInput{
		Labels:   map[string]string{},
		Region:   "us-east-1",
		Settings: &ruleSettings{Replicas: 1},
	}
	desired := ruleInput{
		Labels:   nil,
		Region:   "us-west-2",
		Settings: &ruleSettings{Replicas: 2},
	}

	match, known, err := got.replacementInputs[0].matches(prior, desired, true)
	require.NoError(t, err)
	require.True(t, known)
	require.False(t, match)

	match, known, err = got.replacementInputs[1].matches(prior, desired, false)
	require.NoError(t, err)
	require.True(t, known)
	require.True(t, match)

	match, known, err = got.replacementInputs[2].matches(prior, desired, false)
	require.NoError(t, err)
	require.True(t, known)
	require.True(t, match)

	match, known, err = got.replacementInputs[2].matches(desired, prior, false)
	require.NoError(t, err)
	require.True(t, known)
	require.False(t, match)
}

func TestConditionalReplacementSkipsPredicateForEquivalentField(t *testing.T) {
	_, _, _, _, _, replicas := ruleInputFields()
	calls := 0
	definition := validRuleDefinition()
	definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
		ReplaceWhen(replicas, func(_, _ int) bool {
			calls++
			return true
		}),
	}
	got, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	match, known, err := got.replacementInputs[0].matches(
		ruleInput{Settings: &ruleSettings{Replicas: 1}},
		ruleInput{Settings: &ruleSettings{Replicas: 1}},
		true,
	)
	require.NoError(t, err)
	require.True(t, known)
	require.False(t, match)
	require.Zero(t, calls)
}

func TestDriftRuleReplacesOnNonEquivalentOutput(t *testing.T) {
	got, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)

	match, known, err := got.driftRules[0].matches(
		&ruleOutput{ETag: "recorded"},
		&ruleOutput{ETag: "observed"},
	)
	require.NoError(t, err)
	require.True(t, known)
	require.True(t, match)

	match, known, err = got.driftRules[0].matches(
		&ruleOutput{ETag: "same"},
		&ruleOutput{ETag: "same"},
	)
	require.NoError(t, err)
	require.True(t, known)
	require.False(t, match)
}

func TestRuleCallbacksReturnPanicErrors(t *testing.T) {
	_, _, labels, _, _, _ := ruleInputFields()
	definition := validRuleDefinition()
	definition.InputSemantics.Rules = []InputRule[ruleInput]{
		EqualBy(labels, func(_, _ map[string]string) bool {
			panic("bad equality")
		}),
	}
	got, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	_, _, err = got.inputRules[0].equivalent(
		ruleInput{Labels: map[string]string{"env": "dev"}},
		ruleInput{Labels: map[string]string{"env": "prod"}},
	)
	var panicErr *PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "comparing input field labels", panicErr.Op)
}

func TestResourceDefinitionRejectsInvalidRules(t *testing.T) {
	name, region, labels, settings, tier, _ := ruleInputFields()
	etag := OutputField(func(v *ruleOutput) *string { return &v.ETag })
	tests := []struct {
		name    string
		change  func(*ruleDefinition)
		message string
	}{
		{
			name: "schema version",
			change: func(definition *ruleDefinition) {
				definition.SchemaVersion = 0
			},
			message: "schema version must be greater than zero",
		},
		{
			name: "identity version",
			change: func(definition *ruleDefinition) {
				definition.Identity.Version = 0
			},
			message: "identity version must be greater than zero",
		},
		{
			name: "identity scope",
			change: func(definition *ruleDefinition) {
				definition.Identity.Scope = "regional"
			},
			message: "invalid identity scope",
		},
		{
			name: "nil equality callback",
			change: func(definition *ruleDefinition) {
				definition.InputSemantics.Rules = []InputRule[ruleInput]{
					EqualBy(labels, nil),
				}
			},
			message: "equality callback is nil",
		},
		{
			name: "nil replacement callback",
			change: func(definition *ruleDefinition) {
				definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
					ReplaceWhen(region, nil),
				}
			},
			message: "replacement callback is nil",
		},
		{
			name: "nil drift callback",
			change: func(definition *ruleDefinition) {
				definition.Replacement.Drift = []DriftRule[*ruleOutput]{
					ReplaceOnDrift(etag, nil),
				}
			},
			message: "drift equality callback is nil",
		},
		{
			name: "drift without stable ID",
			change: func(definition *ruleDefinition) {
				definition.Identity.StableID = nil
			},
			message: "drift replacement requires a stable ID",
		},
		{
			name: "duplicate address input",
			change: func(definition *ruleDefinition) {
				definition.Identity.AddressInputs = []AnyInputField[ruleInput]{name, name}
			},
			message: `duplicate address input "name"`,
		},
		{
			name: "duplicate equality rule",
			change: func(definition *ruleDefinition) {
				definition.InputSemantics.Rules = []InputRule[ruleInput]{
					EqualBy(labels, equalStringMaps),
					EqualBy(labels, equalStringMaps),
				}
			},
			message: `duplicate equality rule for "labels"`,
		},
		{
			name: "overlapping equality rules",
			change: func(definition *ruleDefinition) {
				definition.InputSemantics.Rules = []InputRule[ruleInput]{
					EqualBy(settings, func(_, _ *ruleSettings) bool { return true }),
					EqualBy(tier, func(_, _ string) bool { return true }),
				}
			},
			message: `equality rules overlap at "settings" and "settings.tier"`,
		},
		{
			name: "duplicate input replacement rule",
			change: func(definition *ruleDefinition) {
				definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
					ReplaceWhenChanged(region),
					ReplaceWhen(region, func(_, _ string) bool { return true }),
				}
			},
			message: `duplicate input replacement rule for "region"`,
		},
		{
			name: "duplicate drift rule",
			change: func(definition *ruleDefinition) {
				definition.Replacement.Drift = []DriftRule[*ruleOutput]{
					ReplaceOnDrift(etag, func(a, b string) bool { return a == b }),
					ReplaceOnDrift(etag, func(a, b string) bool { return a == b }),
				}
			},
			message: `duplicate drift rule for "etag"`,
		},
		{
			name: "equality overlaps address",
			change: func(definition *ruleDefinition) {
				definition.InputSemantics.Rules = []InputRule[ruleInput]{
					EqualBy(name, func(a, b string) bool { return a == b }),
				}
			},
			message: `equality rule "name" overlaps address input "name"`,
		},
		{
			name: "replacement overlaps address",
			change: func(definition *ruleDefinition) {
				definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
					ReplaceWhenChanged(name),
				}
			},
			message: `input replacement rule "name" overlaps address input "name"`,
		},
		{
			name: "equality and replacement overlap",
			change: func(definition *ruleDefinition) {
				definition.InputSemantics.Rules = []InputRule[ruleInput]{
					EqualBy(settings, func(_, _ *ruleSettings) bool { return true }),
				}
				definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
					ReplaceWhenChanged(tier),
				}
			},
			message: `equality rule "settings" overlaps input replacement rule "settings.tier"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := validRuleDefinition()
			tt.change(&definition)
			_, err := resolveResourceDefinition(definition)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestResourceDefinitionAllowsEqualityAndReplacementOnSameField(t *testing.T) {
	_, _, labels, _, _, _ := ruleInputFields()
	definition := validRuleDefinition()
	definition.InputSemantics.Rules = []InputRule[ruleInput]{
		EqualBy(labels, equalStringMaps),
	}
	definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
		ReplaceWhenChanged(labels),
	}

	_, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
}

func TestResourceDefinitionAllowsOverlappingReplacementRules(t *testing.T) {
	_, _, _, settings, tier, _ := ruleInputFields()
	status := OutputField(func(v *ruleOutput) **ruleStatus { return &v.Status })
	statusCode := OutputField(func(v *ruleOutput) *string { return &v.Status.Code })
	definition := validRuleDefinition()
	definition.Replacement.Inputs = []ReplacementRule[ruleInput]{
		ReplaceWhenChanged(settings),
		ReplaceWhenChanged(tier),
	}
	definition.Replacement.Drift = []DriftRule[*ruleOutput]{
		ReplaceOnDrift(status, func(a, b *ruleStatus) bool { return a == b }),
		ReplaceOnDrift(statusCode, func(a, b string) bool { return a == b }),
	}

	_, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
}

func TestResourceDefinitionRejectsInvalidRootTypes(t *testing.T) {
	_, err := resolveResourceDefinition(ResourceDefinition[string, *ruleOutput, NoConfig]{})
	require.ErrorContains(t, err, "input root must be a struct")

	_, err = resolveResourceDefinition(ResourceDefinition[ruleInput, ruleOutput, NoConfig]{})
	require.ErrorContains(t, err, "output root must be a pointer to a struct")
}

func inputRulePaths[In any](rules []resolvedInputRule[In]) []string {
	paths := make([]string, 0, len(rules))
	for _, rule := range rules {
		paths = append(paths, rule.field.path)
	}
	return paths
}

func inputFieldPaths[In any](fields []fieldMetadata[In]) []string {
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		paths = append(paths, field.path)
	}
	return paths
}

func replacementRulePaths[In any](rules []resolvedReplacementRule[In]) []string {
	paths := make([]string, 0, len(rules))
	for _, rule := range rules {
		paths = append(paths, rule.field.path)
	}
	return paths
}

func driftRulePaths[Out any](rules []resolvedDriftRule[Out]) []string {
	paths := make([]string, 0, len(rules))
	for _, rule := range rules {
		paths = append(paths, rule.field.path)
	}
	return paths
}

func equalStringMaps(a, b map[string]string) bool {
	return maps.Equal(a, b)
}
