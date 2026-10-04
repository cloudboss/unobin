package golibrary

import (
	"fmt"
	"go/scanner"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/libraryapi"
)

func TestReadCompatibility(t *testing.T) {
	t.Setenv("PATH", "")
	tests := []struct {
		name     string
		alias    string
		record   string
		required string
		hint     string
	}{
		{
			name: "literal strings", alias: "runtime",
			record:   `RequiredAPI: "1.0", SuggestedUnobinVersion: "v0.12.0",`,
			required: "1.0", hint: "v0.12.0",
		},
		{
			name: "aliased raw strings", alias: "ubruntime",
			record:   "RequiredAPI: `2.3`, SuggestedUnobinVersion: `v0.12.0-rc.1`,",
			required: "2.3", hint: "v0.12.0-rc.1",
		},
		{
			name: "decoded escapes", alias: "runtime", record: `RequiredAPI: "\x31.\u0030",`,
			required: "1.0",
		},
		{
			name: "empty hint", alias: "runtime", record: `RequiredAPI: "1.0", SuggestedUnobinVersion: "",`,
			required: "1.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleRoot := t.TempDir()
			body := fmt.Sprintf(`func init() { panic("must not execute library initialization") }
func Library() *%s.Library {
	return &%s.Library{
		Name: "é", Compatibility: %s.LibraryCompatibility{%s},
		Resources: unknownRegistrationHelper[UnknownLifecycle](),
	}
}`, tt.alias, tt.alias, tt.alias, tt.record)
			packageDir := writeLibraryPackage(t, moduleRoot, "service", runtimePackage(tt.alias), body)
			got, err := ReadCompatibility(moduleRoot, packageDir)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.required, got.RequiredAPI)
			assert.Equal(t, tt.hint, got.SuggestedUnobinVersion)
			assert.Equal(t, filepath.Join(packageDir, "library.go"), got.Path)
			require.NotNil(t, got.RequiredAPISpan)
			raw, err := os.ReadFile(got.Path)
			require.NoError(t, err)
			value := raw[got.RequiredAPISpan.Start.Offset:got.RequiredAPISpan.End.Offset]
			assert.True(t, strings.HasPrefix(string(value), `"`) || strings.HasPrefix(string(value), "`"))
			assertSpanPosition(t, raw, got.RequiredAPISpan.Start)
			assertSpanPosition(t, raw, *got.RequiredAPISpan.End)
			require.NotNil(t, got.Span.End)
			assert.Contains(t, string(raw[got.Span.Start.Offset:got.Span.End.Offset]),
				tt.alias+".LibraryCompatibility")
			if tt.name == "decoded escapes" {
				assert.Equal(t, `"\x31.\u0030"`, string(value))
			}
			if strings.Contains(tt.record, "SuggestedUnobinVersion") {
				require.NotNil(t, got.SuggestedUnobinVersionSpan)
				assertSpanPosition(t, raw, got.SuggestedUnobinVersionSpan.Start)
			} else {
				assert.Nil(t, got.SuggestedUnobinVersionSpan)
			}
		})
	}
}

func TestReadCompatibilityBeforeTypeChecking(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), `
func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "2.0"},
		Resources: map[string]runtime.OldResourceRegistration{
			"server": runtime.OldResource[RemovedLifecycle](),
		},
	}
}`)
	got, err := ReadCompatibility(moduleRoot, packageDir)
	require.NoError(t, err)
	var unsupported *libraryapi.UnsupportedMajorError
	require.ErrorAs(t, libraryapi.Check(got.RequiredAPI, libraryapi.Current()), &unsupported)
	assert.Equal(t, libraryapi.Version{Major: 2}, unsupported.Required)
}

func TestReadCompatibilityRejectsInvalidRecords(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		kind   CompatibilityErrorKind
		target string
	}{
		{"pointer", `&runtime.LibraryCompatibility{RequiredAPI: "1.0"}`, InvalidDeclaration, "&runtime"},
		{"type alias", `Compatibility{RequiredAPI: "1.0"}`, InvalidDeclaration, "Compatibility{"},
		{"unkeyed", `runtime.LibraryCompatibility{"1.0", ""}`, InvalidDeclaration, `"1.0"`},
		{"helper", `compatibility()`, InvalidDeclaration, "compatibility()"},
		{
			"constant", `runtime.LibraryCompatibility{RequiredAPI: required}`,
			InvalidDeclaration, "required",
		},
		{
			"concatenation", `runtime.LibraryCompatibility{RequiredAPI: "1." + "0"}`,
			InvalidDeclaration, `"1." + "0"`,
		},
		{
			"call", `runtime.LibraryCompatibility{RequiredAPI: currentAPI()}`,
			InvalidDeclaration, "currentAPI()",
		},
		{
			"non-string", `runtime.LibraryCompatibility{RequiredAPI: 1.0}`,
			InvalidDeclaration, "1.0",
		},
		{
			"empty requirement", `runtime.LibraryCompatibility{RequiredAPI: ""}`,
			InvalidDeclaration, `""`,
		},
		{"missing requirement", `runtime.LibraryCompatibility{}`, InvalidDeclaration, "runtime"},
		{
			"invalid requirement", `runtime.LibraryCompatibility{RequiredAPI: "1.0.0"}`,
			InvalidDeclaration, `"1.0.0"`,
		},
		{
			"duplicate requirement",
			`runtime.LibraryCompatibility{RequiredAPI: "1.0", RequiredAPI: "2.0"}`,
			InvalidDeclaration, "RequiredAPI",
		},
		{
			"duplicate hint",
			`runtime.LibraryCompatibility{RequiredAPI: "1.0",` +
				`SuggestedUnobinVersion: "", SuggestedUnobinVersion: "v0.12.0"}`,
			InvalidDeclaration, "SuggestedUnobinVersion",
		},
		{
			"dynamic hint", `runtime.LibraryCompatibility{RequiredAPI: "1.0", SuggestedUnobinVersion: hint}`,
			InvalidDeclaration, "hint",
		},
		{
			"unsupported field", `runtime.LibraryCompatibility{RequiredAPI: "1.0", MaximumAPI: "1.1"}`,
			UnsupportedField, "MaximumAPI",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleRoot := t.TempDir()
			body := `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: ` + tt.value + `}
}`
			packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), body)
			got, err := ReadCompatibility(moduleRoot, packageDir)
			assert.Nil(t, got)
			var declarationError *CompatibilityError
			require.ErrorAs(t, err, &declarationError)
			assert.Equal(t, tt.kind, declarationError.Kind)
			ds := diagnostic.FromError(diagnostic.Context("read schema", err), diagnostic.ConvertOptions{})
			require.Len(t, ds, 1)
			assert.Equal(t, filepath.Join(packageDir, "library.go"), ds[0].Path)
			assert.Contains(t, ds[0].Message, "read schema:")
			require.NotNil(t, ds[0].Span)
			require.NotNil(t, ds[0].Span.End)
			raw, err := os.ReadFile(ds[0].Path)
			require.NoError(t, err)
			assertSpanPosition(t, raw, ds[0].Span.Start)
			assertSpanPosition(t, raw, *ds[0].Span.End)
			assert.Contains(t, string(raw[ds[0].Span.Start.Offset:ds[0].Span.End.Offset]), tt.target)
			code := "unobin.library-api.invalid-declaration"
			if tt.kind == UnsupportedField {
				code = "unobin.library-api.unsupported-field"
			}
			assert.Equal(t, code, ds[0].Code)
		})
	}
}

func TestReadCompatibilityReleaseHints(t *testing.T) {
	for _, hint := range []string{
		"v0.12.0", "v1.2.3-rc.1", "v1.2.3-0", "v1.2.3-alpha.beta",
	} {
		t.Run(hint, func(t *testing.T) {
			moduleRoot := t.TempDir()
			packageDir := writeCompatibilityHint(t, moduleRoot, hint)
			got, err := ReadCompatibility(moduleRoot, packageDir)
			require.NoError(t, err)
			assert.Equal(t, hint, got.SuggestedUnobinVersion)
		})
	}
	for _, hint := range []string{
		"dev", "v1", "v1.2", "1.2.3", " v1.2.3", "v1.2.3 ", "v1.2.3\n", "v01.2.3",
		"v1.2.3-01", "v1.2.3-", "v1.2.3+build",
	} {
		t.Run(hint, func(t *testing.T) {
			moduleRoot := t.TempDir()
			packageDir := writeCompatibilityHint(t, moduleRoot, hint)
			got, err := ReadCompatibility(moduleRoot, packageDir)
			assert.Nil(t, got)
			var declarationError *CompatibilityError
			require.ErrorAs(t, err, &declarationError)
			assert.Equal(t, InvalidDeclaration, declarationError.Kind)
			assert.Contains(t, err.Error(), "SuggestedUnobinVersion")
		})
	}
}

func TestReadCompatibilityLibraryDeclarations(t *testing.T) {
	tests := []struct {
		name string
		body string
		kind CompatibilityErrorKind
	}{
		{"missing function", `func LibraryConfiguration() {}`, MissingDeclaration},
		{
			"missing record", `func Library() *runtime.Library { return &runtime.Library{} }`,
			MissingDeclaration,
		},
		{
			"duplicate record",
			`func Library() *runtime.Library { return &runtime.Library{
				Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
				Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
			} }`, InvalidDeclaration,
		},
		{"indirect return", `func Library() *runtime.Library { return helper() }`, InvalidDeclaration},
		{"invalid signature", `func Library() int { return 1 }`, InvalidDeclaration},
		{
			"multiple returns", `func Library() *runtime.Library {
				if cond { return &runtime.Library{} }; return &runtime.Library{}
			}`, InvalidDeclaration,
		},
		{
			"unkeyed library", `func Library() *runtime.Library {
				return &runtime.Library{runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
			}`, InvalidDeclaration,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleRoot := t.TempDir()
			packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), tt.body)
			_, err := ReadCompatibility(moduleRoot, packageDir)
			var declarationError *CompatibilityError
			require.ErrorAs(t, err, &declarationError)
			assert.Equal(t, tt.kind, declarationError.Kind)
			if tt.kind == MissingDeclaration {
				ds := declarationError.Diagnostics()
				require.Len(t, ds, 1)
				assert.Equal(t, "unobin.library-api.missing-declaration", ds[0].Code)
				assert.NotEmpty(t, ds[0].Hint)
			}
		})
	}
}

func TestReadCompatibilityUsesDeclaringFile(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(t, moduleRoot, ".", "", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
}`)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "helper.go"),
		[]byte(librarySource(runtimePackage(""), "var _ *runtime.Library")), 0o644))
	_, err := ReadCompatibility(moduleRoot, packageDir)
	var declarationError *CompatibilityError
	require.ErrorAs(t, err, &declarationError)
	assert.Equal(t, InvalidDeclaration, declarationError.Kind)
}

func TestReadCompatibilityRejectsInvalidRuntimeImports(t *testing.T) {
	for _, alias := range []string{"_", "."} {
		t.Run(alias, func(t *testing.T) {
			moduleRoot := t.TempDir()
			packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(alias), `
func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
}`)
			_, err := ReadCompatibility(moduleRoot, packageDir)
			var declarationError *CompatibilityError
			require.ErrorAs(t, err, &declarationError)
			assert.Equal(t, InvalidDeclaration, declarationError.Kind)
		})
	}
}

func TestReadCompatibilityRequiresMatchingRuntimeAlias(t *testing.T) {
	moduleRoot := t.TempDir()
	imports := runtimePackage("") + "\n" + runtimePackage("other")
	packageDir := writeLibraryPackage(t, moduleRoot, ".", imports, `
func Library() *runtime.Library {
	return &runtime.Library{Compatibility: other.LibraryCompatibility{RequiredAPI: "1.0"}}
}`)
	_, err := ReadCompatibility(moduleRoot, packageDir)
	var declarationError *CompatibilityError
	require.ErrorAs(t, err, &declarationError)
	assert.Equal(t, InvalidDeclaration, declarationError.Kind)
	assert.Contains(t, err.Error(), "runtime import alias")
}

func TestReadCompatibilityReportsPhysicalSourceLocation(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), `
//line other.go:900
func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
}`)
	got, err := ReadCompatibility(moduleRoot, packageDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(packageDir, "library.go"), got.Path)
	raw, err := os.ReadFile(got.Path)
	require.NoError(t, err)
	assertSpanPosition(t, raw, got.RequiredAPISpan.Start)
}

func TestValidatePackageAcceptsConfigurationOnlyLibrary(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), `
func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration(),
	}
}`)
	metadata, err := ReadCompatibility(moduleRoot, packageDir)
	require.NoError(t, err)
	assert.Equal(t, "1.0", metadata.RequiredAPI)
	validation, err := ValidatePackage(moduleRoot, packageDir)
	require.NoError(t, err)
	assert.Equal(t, &Validation{
		ModulePath: "example.com/lib", PackageName: "lib", HasConfiguration: true,
	}, validation)
}

func TestReadCompatibilityChecksAllNonTestFiles(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeCompatibilityHint(t, moduleRoot, "")
	other := []byte(librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
}`))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "library_test.go"), other, 0o644))
	_, err := ReadCompatibility(moduleRoot, packageDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "alternative.go"),
		append([]byte("//go:build special\n\n"), other...), 0o644))
	_, err = ReadCompatibility(moduleRoot, packageDir)
	var declarationError *CompatibilityError
	require.ErrorAs(t, err, &declarationError)
	assert.Equal(t, InvalidDeclaration, declarationError.Kind)
	assert.Contains(t, err.Error(), "more than one")
}

func TestReadCompatibilityOperationalAndSyntaxFailures(t *testing.T) {
	moduleRoot := t.TempDir()
	_, err := ReadCompatibility(moduleRoot, filepath.Join(moduleRoot, "absent"))
	require.ErrorIs(t, err, fs.ErrNotExist)
	var declarationError *CompatibilityError
	assert.NotErrorAs(t, err, &declarationError)
	packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), "func Library(")
	_, err = ReadCompatibility(moduleRoot, packageDir)
	var syntaxError scanner.ErrorList
	require.ErrorAs(t, err, &syntaxError)
	assert.NotErrorAs(t, err, &declarationError)
}

func writeCompatibilityHint(t *testing.T, moduleRoot, hint string) string {
	t.Helper()
	body := fmt.Sprintf(`func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{
		RequiredAPI: "1.0", SuggestedUnobinVersion: %q,
	}}
}`, hint)
	return writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), body)
}

func assertSpanPosition(t *testing.T, source []byte, position diagnostic.Position) {
	t.Helper()
	prefix := string(source[:position.Offset])
	assert.Equal(t, strings.Count(prefix, "\n")+1, position.Line)
	assert.Equal(t, len(prefix)-strings.LastIndex(prefix, "\n"), position.Column)
}
