package golibrary

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeModulePackage(t *testing.T, moduleRoot, packageRel, src string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(moduleRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleRoot, "go.mod"),
		[]byte("module example.com/lib\n\ngo 1.26\n"), 0o644))
	packageDir := filepath.Join(moduleRoot, filepath.FromSlash(packageRel))
	require.NoError(t, os.MkdirAll(packageDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "library.go"), []byte(src), 0o644))
	return packageDir
}

func runtimePackage(alias string) string {
	if alias == "" {
		return `import "github.com/cloudboss/unobin/pkg/runtime"`
	}
	return `import ` + alias + ` "github.com/cloudboss/unobin/pkg/runtime"`
}

func librarySource(importDecl, body string) string {
	return "package lib\n\n" + importDecl + "\n\n" + body + "\n"
}

func writeLibraryPackage(
	t *testing.T,
	moduleRoot string,
	packageRel string,
	importDecl string,
	body string,
) string {
	t.Helper()
	return writeModulePackage(t, moduleRoot, packageRel, librarySource(importDecl, body))
}

func TestValidatePackageAcceptsResourceLibrary(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(
		t, moduleRoot, ".", runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{
		Resources: map[string]runtime.ResourceRegistration{
			"server": nil,
		},
	}
}`)

	got, err := ValidatePackage(moduleRoot, packageDir)
	require.NoError(t, err)
	require.Equal(t, "example.com/lib", got.ModulePath)
	require.Equal(t, "lib", got.PackageName)
	require.True(t, got.HasResources)
	require.False(t, got.HasData)
	require.False(t, got.HasActions)
	require.False(t, got.HasFunctions)
}

func TestValidatePackageAcceptsAliasedRuntimeImport(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(
		t, moduleRoot, ".", runtimePackage("ubruntime"), `func Library() *ubruntime.Library {
	return &ubruntime.Library{
		Resources: map[string]ubruntime.ResourceRegistration{
			"server": nil,
		},
	}
}`)

	got, err := ValidatePackage(moduleRoot, packageDir)
	require.NoError(t, err)
	require.True(t, got.HasResources)
}

func TestValidatePackageAcceptsRegistrationKinds(t *testing.T) {
	tests := []struct {
		name  string
		field string
		typ   string
		check func(*Validation) bool
	}{
		{
			name:  "data sources",
			field: "DataSources",
			typ:   "DataSourceRegistration",
			check: func(v *Validation) bool { return v.HasData },
		},
		{
			name:  "actions",
			field: "Actions",
			typ:   "ActionRegistration",
			check: func(v *Validation) bool { return v.HasActions },
		},
		{
			name:  "functions",
			field: "Functions",
			typ:   "FunctionType",
			check: func(v *Validation) bool { return v.HasFunctions },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleRoot := t.TempDir()
			body := `func Library() *runtime.Library {
	return &runtime.Library{
		` + tt.field + `: map[string]runtime.` + tt.typ + `{
			"x": {},
		},
	}
}`
			packageDir := writeLibraryPackage(t, moduleRoot, ".", runtimePackage(""), body)

			got, err := ValidatePackage(moduleRoot, packageDir)
			require.NoError(t, err)
			require.True(t, tt.check(got))
		})
	}
}

func TestValidatePackageAcceptsSubpackage(t *testing.T) {
	moduleRoot := t.TempDir()
	packageDir := writeLibraryPackage(
		t, moduleRoot, "sub/lib", runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{
		Actions: map[string]runtime.ActionRegistration{
			"run": {},
		},
	}
}`)

	got, err := ValidatePackage(moduleRoot, packageDir)
	require.NoError(t, err)
	require.Equal(t, "example.com/lib", got.ModulePath)
	require.Equal(t, "lib", got.PackageName)
	require.True(t, got.HasActions)
}

func TestValidatePackageRejectsPackageOutsideModule(t *testing.T) {
	moduleRoot := t.TempDir()
	outside := t.TempDir()
	packageDir := writeLibraryPackage(
		t, outside, ".", runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{Resources: map[string]runtime.ResourceRegistration{"x": {}}}
}`)

	_, err := ValidatePackage(moduleRoot, packageDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not inside module root")
}

func TestValidatePackageRejectsInvalidLibraryForms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "dot import",
			src: librarySource(
				`import . "github.com/cloudboss/unobin/pkg/runtime"`,
				`func Library() *Library {
	return &Library{}
}`,
			),
			want: "dot import",
		},
		{
			name: "receiver method",
			src: librarySource(runtimePackage(""), `type T struct{}

func (T) Library() *runtime.Library {
	return &runtime.Library{}
}`),
			want: "no package-level library function",
		},
		{
			name: "parameters",
			src: librarySource(runtimePackage(""), `func Library(name string) *runtime.Library {
	return &runtime.Library{}
}`),
			want: "must not accept parameters",
		},
		{
			name: "type parameters",
			src: librarySource(runtimePackage(""), `func Library[T any]() *runtime.Library {
	return &runtime.Library{}
}`),
			want: "must not declare type parameters",
		},
		{
			name: "wrong return type",
			src: librarySource(runtimePackage(""), `func Library() any {
	return nil
}`),
			want: "must return *runtime.Library",
		},
		{
			name: "two library functions",
			src: librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{}
}

func Library() *runtime.Library {
	return &runtime.Library{}
}`),
			want: "more than one package-level library function",
		},
		{
			name: "two returns",
			src: librarySource(runtimePackage(""), `func Library() *runtime.Library {
	if true {
		return &runtime.Library{}
	}
	return &runtime.Library{}
}`),
			want: "exactly one return statement",
		},
		{
			name: "helper returned",
			src: librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return makeLibrary()
}

func makeLibrary() *runtime.Library { return &runtime.Library{} }
`),
			want: "must return &runtime.Library",
		},
		{
			name: "metadata only",
			src: librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{
		Name: "test",
		Description: "test",
	}
}`),
			want: "must register at least one",
		},
		{
			name: "configuration only",
			src: librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{
		Configuration: nil,
	}
}`),
			want: "must register at least one",
		},
		{
			name: "composites only",
			src: librarySource(runtimePackage(""), `func Library() *runtime.Library {
	return &runtime.Library{
		ResourceComposites: map[string]*runtime.CompositeType{
			"x": nil,
		},
	}
}`),
			want: "must register at least one",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleRoot := t.TempDir()
			packageDir := writeModulePackage(t, moduleRoot, ".", tt.src)

			_, err := ValidatePackage(moduleRoot, packageDir)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}
