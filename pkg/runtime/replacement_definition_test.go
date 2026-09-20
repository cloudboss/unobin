package runtime

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type replacementTestNetwork struct {
	Subnet string `ub:"subnet"`
}

type replacementTestInput struct {
	Name     string                  `ub:"name"`
	Capacity int64                   `ub:"capacity"`
	Labels   map[string]string       `ub:"labels"`
	Network  *replacementTestNetwork `ub:"network"`
}

type replacementTestConfig struct {
	Region string `ub:"region"`
	Token  string `ub:"token,sensitive"`
}

type replacementTestStatus struct {
	Generation int64 `ub:"generation"`
}

type replacementTestOutput struct {
	ID       string                 `ub:"id"`
	Status   *replacementTestStatus `ub:"status"`
	Checksum string                 `ub:"checksum"`
}

func TestResourceDefinitionClassifiesReplacementReasons(t *testing.T) {
	name := InputField(func(v *replacementTestInput) *string { return &v.Name })
	capacity := InputField(func(v *replacementTestInput) *int64 { return &v.Capacity })
	labels := InputField(func(v *replacementTestInput) *map[string]string {
		return &v.Labels
	})
	region := ConfigurationField(func(v *replacementTestConfig) *string {
		return &v.Region
	})
	generation := OutputField(func(v *replacementTestOutput) *int64 {
		return &v.Status.Generation
	})
	definition := ResourceDefinition[
		replacementTestInput,
		*replacementTestOutput,
		*replacementTestConfig,
	]{
		SchemaVersion: 1,
		Equality: []InputEqualityRule[replacementTestInput]{
			EqualBy(name, strings.EqualFold),
			EqualBy(labels, maps.Equal),
		},
		Replace: Replacement[
			replacementTestInput,
			*replacementTestOutput,
			*replacementTestConfig,
		]{
			Fields: []AnyInputField[replacementTestInput]{name},
			Rules: []ReplacementRule[replacementTestInput]{
				ReplaceWhen(capacity, func(prior, desired int64) bool {
					return desired < prior
				}),
			},
			ConfigurationFields: []AnyConfigurationField[*replacementTestConfig]{
				region,
			},
			Drift: []DriftRule[*replacementTestOutput]{
				ReplaceOnDrift(generation, func(recorded, observed int64) bool {
					return recorded != observed
				}),
			},
		},
		StableID: func(_ replacementTestInput, out *replacementTestOutput) (string, error) {
			return out.ID, nil
		},
	}

	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	baseInput := replacementTestInput{
		Name:     "example",
		Capacity: 10,
		Labels:   map[string]string{"owner": "runtime"},
	}
	baseConfig := &replacementTestConfig{Region: "east", Token: "old"}
	baseOutput := &replacementTestOutput{
		ID:       "object-1",
		Status:   &replacementTestStatus{Generation: 3},
		Checksum: "recorded",
	}

	tests := []struct {
		name           string
		desiredInput   replacementTestInput
		desiredConfig  *replacementTestConfig
		observedOutput *replacementTestOutput
		want           []string
	}{
		{
			name:           "unchanged",
			desiredInput:   baseInput,
			desiredConfig:  baseConfig,
			observedOutput: baseOutput,
		},
		{
			name: "semantic equality suppresses replacement",
			desiredInput: replacementTestInput{
				Name:     "EXAMPLE",
				Capacity: 10,
				Labels:   map[string]string{"owner": "runtime"},
			},
			desiredConfig:  baseConfig,
			observedOutput: baseOutput,
		},
		{
			name: "unconditional input",
			desiredInput: replacementTestInput{
				Name:     "different",
				Capacity: 10,
				Labels:   baseInput.Labels,
			},
			desiredConfig:  baseConfig,
			observedOutput: baseOutput,
			want:           []string{"name"},
		},
		{
			name: "conditional input chooses update",
			desiredInput: replacementTestInput{
				Name:     baseInput.Name,
				Capacity: 11,
				Labels:   baseInput.Labels,
			},
			desiredConfig:  baseConfig,
			observedOutput: baseOutput,
		},
		{
			name: "conditional input chooses replacement",
			desiredInput: replacementTestInput{
				Name:     baseInput.Name,
				Capacity: 9,
				Labels:   baseInput.Labels,
			},
			desiredConfig:  baseConfig,
			observedOutput: baseOutput,
			want:           []string{"capacity"},
		},
		{
			name:          "unselected credentials",
			desiredInput:  baseInput,
			desiredConfig: &replacementTestConfig{Region: "east", Token: "new"},
			observedOutput: &replacementTestOutput{
				ID:       "object-1",
				Status:   &replacementTestStatus{Generation: 3},
				Checksum: "changed",
			},
		},
		{
			name:           "selected configuration",
			desiredInput:   baseInput,
			desiredConfig:  &replacementTestConfig{Region: "west", Token: "new"},
			observedOutput: baseOutput,
			want:           []string{"region"},
		},
		{
			name:          "selected drift",
			desiredInput:  baseInput,
			desiredConfig: baseConfig,
			observedOutput: &replacementTestOutput{
				ID:       "object-1",
				Status:   &replacementTestStatus{Generation: 4},
				Checksum: "recorded",
			},
			want: []string{"status.generation"},
		},
		{
			name: "all reasons are canonical and sorted",
			desiredInput: replacementTestInput{
				Name:     "different",
				Capacity: 9,
				Labels:   baseInput.Labels,
			},
			desiredConfig: &replacementTestConfig{Region: "west", Token: "new"},
			observedOutput: &replacementTestOutput{
				ID:       "object-1",
				Status:   &replacementTestStatus{Generation: 4},
				Checksum: "changed",
			},
			want: []string{"capacity", "name", "region", "status.generation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolved.replacementReasons(
				baseInput,
				tt.desiredInput,
				baseConfig,
				tt.desiredConfig,
				baseOutput,
				tt.observedOutput,
			)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	equal, err := resolved.inputsEqual(
		baseInput,
		replacementTestInput{
			Name:     "EXAMPLE",
			Capacity: 10,
			Labels:   map[string]string{"owner": "runtime"},
		},
	)
	require.NoError(t, err)
	require.True(t, equal)
}

func TestResourceDefinitionResolvesSelectorsOnce(t *testing.T) {
	inputCalls := 0
	configCalls := 0
	outputCalls := 0
	name := InputField(func(v *replacementTestInput) *string {
		inputCalls++
		return &v.Name
	})
	region := ConfigurationField(func(v *replacementTestConfig) *string {
		configCalls++
		return &v.Region
	})
	generation := OutputField(func(v *replacementTestOutput) *int64 {
		outputCalls++
		return &v.Status.Generation
	})

	resolved, err := resolveResourceDefinition(ResourceDefinition[
		replacementTestInput,
		*replacementTestOutput,
		*replacementTestConfig,
	]{
		SchemaVersion: 1,
		Replace: Replacement[
			replacementTestInput,
			*replacementTestOutput,
			*replacementTestConfig,
		]{
			Fields: []AnyInputField[replacementTestInput]{name},
			ConfigurationFields: []AnyConfigurationField[*replacementTestConfig]{
				region,
			},
			Drift: []DriftRule[*replacementTestOutput]{
				ReplaceOnDrift(generation, func(recorded, observed int64) bool {
					return recorded != observed
				}),
			},
		},
		StableID: func(_ replacementTestInput, out *replacementTestOutput) (string, error) {
			return out.ID, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, inputCalls)
	require.Equal(t, 2, configCalls)
	require.Equal(t, 2, outputCalls)

	_, err = resolved.replacementReasons(
		replacementTestInput{Name: "old"},
		replacementTestInput{Name: "new"},
		&replacementTestConfig{Region: "east"},
		&replacementTestConfig{Region: "west"},
		&replacementTestOutput{Status: &replacementTestStatus{Generation: 1}},
		&replacementTestOutput{Status: &replacementTestStatus{Generation: 2}},
	)
	require.NoError(t, err)
	require.Equal(t, 2, inputCalls)
	require.Equal(t, 2, configCalls)
	require.Equal(t, 2, outputCalls)
}

func TestResourceDefinitionRejectsInvalidRules(t *testing.T) {
	name := InputField(func(v *replacementTestInput) *string { return &v.Name })
	network := InputField(func(v *replacementTestInput) **replacementTestNetwork {
		return &v.Network
	})
	subnet := InputField(func(v *replacementTestInput) *string {
		return &v.Network.Subnet
	})
	status := OutputField(func(v *replacementTestOutput) **replacementTestStatus {
		return &v.Status
	})
	generation := OutputField(func(v *replacementTestOutput) *int64 {
		return &v.Status.Generation
	})

	tests := []struct {
		name       string
		definition ResourceDefinition[
			replacementTestInput,
			*replacementTestOutput,
			*replacementTestConfig,
		]
		want string
	}{
		{
			name: "schema version",
			want: "schema version must be greater than zero",
		},
		{
			name: "nil equality callback",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Equality: []InputEqualityRule[replacementTestInput]{
					EqualBy(name, nil),
				},
			},
			want: "equality callback is nil",
		},
		{
			name: "duplicate fields",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{Fields: []AnyInputField[replacementTestInput]{name, name}},
			},
			want: `duplicate replacement field "name"`,
		},
		{
			name: "nil replacement callback",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{
					Rules: []ReplacementRule[replacementTestInput]{
						ReplaceWhen(name, nil),
					},
				},
			},
			want: "replacement callback is nil",
		},
		{
			name: "field and rule select the same input",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{
					Fields: []AnyInputField[replacementTestInput]{name},
					Rules: []ReplacementRule[replacementTestInput]{
						ReplaceWhen(name, func(_, _ string) bool { return true }),
					},
				},
			},
			want: `replacement rule "name" overlaps replacement field "name"`,
		},
		{
			name: "ancestor and descendant inputs",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{Fields: []AnyInputField[replacementTestInput]{network, subnet}},
			},
			want: `replacement fields overlap at "network" and "network.subnet"`,
		},
		{
			name: "ancestor and descendant rules",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{
					Rules: []ReplacementRule[replacementTestInput]{
						ReplaceWhen(network, func(_, _ *replacementTestNetwork) bool {
							return true
						}),
						ReplaceWhen(subnet, func(_, _ string) bool { return true }),
					},
				},
			},
			want: `replacement rules overlap at "network" and "network.subnet"`,
		},
		{
			name: "nil drift callback",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{
					Drift: []DriftRule[*replacementTestOutput]{
						ReplaceOnDrift(generation, nil),
					},
				},
				StableID: func(_ replacementTestInput, out *replacementTestOutput) (
					string,
					error,
				) {
					return out.ID, nil
				},
			},
			want: "drift callback is nil",
		},
		{
			name: "overlapping drift",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{
					Drift: []DriftRule[*replacementTestOutput]{
						ReplaceOnDrift(status, func(_, _ *replacementTestStatus) bool {
							return true
						}),
						ReplaceOnDrift(generation, func(_, _ int64) bool { return true }),
					},
				},
				StableID: func(_ replacementTestInput, out *replacementTestOutput) (
					string,
					error,
				) {
					return out.ID, nil
				},
			},
			want: `drift rules overlap at "status" and "status.generation"`,
		},
		{
			name: "drift requires stable ID",
			definition: ResourceDefinition[
				replacementTestInput,
				*replacementTestOutput,
				*replacementTestConfig,
			]{
				SchemaVersion: 1,
				Replace: Replacement[
					replacementTestInput,
					*replacementTestOutput,
					*replacementTestConfig,
				]{
					Drift: []DriftRule[*replacementTestOutput]{
						ReplaceOnDrift(generation, func(_, _ int64) bool { return true }),
					},
				},
			},
			want: "drift replacement requires a stable ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveResourceDefinition(tt.definition)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestResourceDefinitionRejectsInvalidSelector(t *testing.T) {
	external := "elsewhere"
	field := InputField(func(*replacementTestInput) *string { return &external })
	_, err := resolveResourceDefinition(ResourceDefinition[
		replacementTestInput,
		*replacementTestOutput,
		*replacementTestConfig,
	]{
		SchemaVersion: 1,
		Replace: Replacement[
			replacementTestInput,
			*replacementTestOutput,
			*replacementTestConfig,
		]{Fields: []AnyInputField[replacementTestInput]{field}},
	})
	require.ErrorContains(t, err, "does not select a field")
}
