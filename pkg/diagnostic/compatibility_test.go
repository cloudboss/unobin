package diagnostic

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

func TestCompatibilityErrorConversion(t *testing.T) {
	original := compatibilityDiagnostic()
	cause := errors.New("incompatible library")
	err := Context("factory", fmt.Errorf("read failed: %w", WithDiagnostics(cause, original)))
	require.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "factory")
	want := original
	want.Message = "factory: " + want.Message
	want.Path = "mapped/" + want.Path
	details := *want.LibraryCompatibility
	details.Replacement = "mapped/" + details.Replacement
	details.ActualReplacement = "mapped/" + details.ActualReplacement
	want.LibraryCompatibility = &details
	got := FromError(err, ConvertOptions{
		DefaultCode: "unobin.schema", Path: func(path string) string { return "mapped/" + path },
	})
	assert.Equal(t, []Diagnostic{want}, got)
	assert.Equal(t, "library.go", original.Path)
	assert.Equal(t, "local/library", original.LibraryCompatibility.Replacement)
	assert.Nil(t, WithDiagnostics(nil, original))

	typed := compatibilityError{diagnostics: []Diagnostic{original}}
	assert.Equal(t, []Diagnostic{original}, FromError(typed, ConvertOptions{}))
	joined := errors.Join(WithDiagnostics(cause, original), errors.New("disk failed"))
	assert.Equal(t, Merge([]Diagnostic{original}, []Diagnostic{{
		Code: "unobin.error", Severity: SeverityError, Message: "disk failed",
	}}), FromError(joined, ConvertOptions{}))
	notice := Diagnostic{Code: "excluded", Severity: SeverityInfo, Message: "candidate rejected"}
	assert.Equal(t, Merge([]Diagnostic{original}, []Diagnostic{notice}), FromError(
		WithDiagnostics(typed, notice), ConvertOptions{},
	))
	assert.Equal(t, []Diagnostic{{
		Code: "unobin.error", Severity: SeverityError, Message: "incompatible",
	}}, FromError(compatibilityError{}, ConvertOptions{}))
}

func TestCompatibilityDiagnosticsOwnTheirData(t *testing.T) {
	original := compatibilityDiagnostic()
	normalized := Normalize([]Diagnostic{original})
	var collector Collector
	collector.Report(original)
	wrapped := WithDiagnostics(errors.New("incompatible"), original)
	original.LibraryCompatibility.RequiredAPI = "9.0"
	original.LibraryCompatibility.ImplementedAPIs[0] = "9.0"
	original.LibraryCompatibility.RequirementChain[0].Dependency = "changed"
	assert.Equal(t, []Diagnostic{compatibilityDiagnostic()}, normalized)
	assert.Equal(t, []Diagnostic{compatibilityDiagnostic()}, collector.Diagnostics())
	assert.Equal(t, []Diagnostic{compatibilityDiagnostic()}, FromError(wrapped, ConvertOptions{}))
	normalized[0].LibraryCompatibility.ImplementedAPIs[0] = "8.0"
	assert.Equal(t, []Diagnostic{compatibilityDiagnostic()}, collector.Diagnostics())
	converted := FromError(wrapped, ConvertOptions{})
	converted[0].LibraryCompatibility.RequirementChain[0].Dependency = "changed"
	assert.Equal(t, []Diagnostic{compatibilityDiagnostic()}, FromError(wrapped, ConvertOptions{}))
}

func TestCompatibilityDiagnosticOrdering(t *testing.T) {
	base := compatibilityDiagnostic()
	var variants []Diagnostic
	typ := reflect.TypeFor[LibraryCompatibilityDetails]()
	for field := range typ.Fields() {
		changed := cloneDiagnostic(base)
		value := reflect.ValueOf(changed.LibraryCompatibility).Elem().FieldByName(field.Name)
		switch value.Kind() {
		case reflect.String:
			value.SetString("different")
		case reflect.Slice:
			if field.Name == "ImplementedAPIs" {
				changed.LibraryCompatibility.ImplementedAPIs = []string{"2.0"}
			} else {
				changed.LibraryCompatibility.RequirementChain[0].Version = "v0.2.0"
			}
		}
		assert.False(t, diagnosticEqual(base, changed), field.Name)
		assert.NotZero(t, compareDiagnostic(base, changed), field.Name)
		variants = append(variants, changed)
	}
	without := base
	without.LibraryCompatibility = nil
	assert.False(t, diagnosticEqual(base, without))
	assert.NotZero(t, compareDiagnostic(base, without))
	for field := range reflect.TypeFor[LibraryRequirementStep]().Fields() {
		changed := cloneDiagnostic(base)
		step := &changed.LibraryCompatibility.RequirementChain[0]
		reflect.ValueOf(step).Elem().FieldByName(field.Name).SetString("different")
		assert.False(t, diagnosticEqual(base, changed), field.Name)
		assert.NotZero(t, compareDiagnostic(base, changed), field.Name)
	}
	variants = append(variants, base, without, cloneDiagnostic(base))
	want := slices.Clone(variants[:len(variants)-1])
	slices.SortFunc(want, compareDiagnostic)
	assert.Equal(t, want, Normalize(variants))
	slices.Reverse(variants)
	assert.Equal(t, want, Normalize(variants))
}

func TestCompatibilityDiagnosticUBFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/diagnostic/valid", func(
		name string, source []byte,
	) (string, []string) {
		var d Diagnostic
		if err := ub.Unmarshal(source, &d); err != nil {
			return "", []string{err.Error()}
		}
		data, err := ub.MarshalIndent(d, "", "  ")
		if err != nil {
			return "", []string{err.Error()}
		}
		return string(data) + "\n", nil
	}, ubtest.Idempotent())
}

func TestCompatibilityDiagnosticFormats(t *testing.T) {
	diagnostic := compatibilityDiagnostic()
	data, err := json.MarshalIndent(diagnostic, "", "  ")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/library-compatibility.json")
	require.NoError(t, err)
	assert.Equal(t, string(want), string(data)+"\n")
	data, err = ub.MarshalIndent(diagnostic, "", "  ")
	require.NoError(t, err)
	want, err = os.ReadFile("testdata/ub/diagnostic/valid/library-compatibility.ub.out")
	require.NoError(t, err)
	assert.Equal(t, string(want), string(data)+"\n")

	for _, value := range []Diagnostic{{}, {LibraryCompatibility: &LibraryCompatibilityDetails{}}} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		var jsonValue map[string]any
		require.NoError(t, json.Unmarshal(data, &jsonValue))
		data, err = ub.Marshal(value)
		require.NoError(t, err)
		var ubValue map[string]any
		require.NoError(t, ub.Unmarshal(data, &ubValue))
		assert.Equal(t, jsonValue, ubValue)
		if value.LibraryCompatibility == nil {
			assert.NotContains(t, jsonValue, "library-compatibility")
		} else {
			assert.Equal(t, map[string]any{}, jsonValue["library-compatibility"])
		}
	}
}

func compatibilityDiagnostic() Diagnostic {
	return Diagnostic{
		Code: "unobin.library-api.newer-minor", Severity: SeverityError,
		Message: "library requires a newer API", Hint: "upgrade Unobin", Path: "library.go",
		Span: &Span{Start: Position{Line: 7, Column: 16, Offset: 125}, End: &Position{
			Line: 7, Column: 21, Offset: 130,
		}},
		LibraryCompatibility: &LibraryCompatibilityDetails{
			Dependency: "example.com/library", Package: "example.com/library/service",
			ModulePath: "example.com/library", Version: "v0.3.0", Commit: "abc123",
			ActualVersion: "v0.4.0", ActualRequiredAPI: "2.0", CandidateVersion: "v0.2.0",
			Query: "latest", Floor: "v0.1.0", RequiredAPI: "1.1", ImplementedAPIs: []string{"1.0"},
			UnobinVersion: "v0.12.0", SuggestedUnobinVersion: "v0.13.0",
			RequiredCoreVersion: "v0.12.1", MinimumGoVersion: "1.26.2",
			Replacement: "local/library", ActualReplacement: "other/library",
			RequirementChain: []LibraryRequirementStep{
				{Dependency: "project.ub", Requires: "example.com/app", MinimumVersion: "v0.1.0"},
				{
					Dependency: "example.com/app", Version: "v0.1.0",
					Requires: "example.com/library", MinimumVersion: "v0.3.0",
				},
			},
		},
	}
}

type compatibilityError struct {
	diagnostics []Diagnostic
}

func (compatibilityError) Error() string {
	return "incompatible"
}

func (e compatibilityError) Diagnostics() []Diagnostic {
	return e.diagnostics
}
