package syntax

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
)

func lowerFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/lower", name)
}

func lowerInvalidFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadFixture(t, "testdata/ub/lower/invalid/"+name+".ub")
}

func parseFile(t *testing.T, path, src string, kind parse.FileKind) *parse.File {
	t.Helper()
	f, err := lang.ParseSource(path, []byte(src))
	require.NoError(t, err)
	f.Kind = kind
	return f
}

func requireSpan(t *testing.T, span parse.Span) {
	t.Helper()
	require.False(t, span.Start.IsZero())
}

func TestLowerFactoryFile(t *testing.T) {
	f := parseFile(t, "factory.ub", lowerFixture(t, "factory-file"), parse.FileFactory)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	requireSpan(t, got.S)
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	requireSpan(t, got.Factory.S)
	requireSpan(t, got.Factory.Body.S)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)

	require.Len(t, got.Factory.Body.Imports, 1)
	requireSpan(t, got.Factory.Body.Imports[0].S)
	assert.Equal(t, "std", got.Factory.Body.Imports[0].Alias.Name)
	assert.Equal(t, "github.com/cloudboss/unobin-library-std", got.Factory.Body.Imports[0].Ref.Value)

	require.Len(t, got.Factory.Body.Inputs, 1)
	requireSpan(t, got.Factory.Body.Inputs[0].S)
	assert.Equal(t, "message", got.Factory.Body.Inputs[0].Name.Name)
	require.IsType(t, &parse.TypeAtomic{}, got.Factory.Body.Inputs[0].Type)
	assert.Equal(t, "string", got.Factory.Body.Inputs[0].Type.(*parse.TypeAtomic).Name)

	require.Len(t, got.Factory.Body.Locals, 1)
	requireSpan(t, got.Factory.Body.Locals[0].S)
	assert.Equal(t, "path", got.Factory.Body.Locals[0].Name.Name)

	require.Len(t, got.Factory.Body.Constraints, 1)
	requireSpan(t, got.Factory.Body.Constraints[0].S)

	require.Len(t, got.Factory.Body.LibraryConfigs, 1)
	requireSpan(t, got.Factory.Body.LibraryConfigs[0].S)
	assert.Equal(t, "std", got.Factory.Body.LibraryConfigs[0].Alias.Name)
	require.IsType(t, &parse.DotPath{}, got.Factory.Body.LibraryConfigs[0].Value)

	require.Len(t, got.Factory.Body.Resources, 1)
	resource := got.Factory.Body.Resources[0]
	requireSpan(t, resource.S)
	requireSpan(t, resource.Selector.S)
	assert.Equal(t, NodeResource, resource.Kind)
	assert.Equal(t, "hello", resource.Name.Name)
	assert.Equal(t, "std", resource.Selector.Alias.Name)
	assert.Equal(t, "fs-file", resource.Selector.Export.Name)

	require.Len(t, got.Factory.Body.Data, 1)
	requireSpan(t, got.Factory.Body.Data[0].S)
	assert.Equal(t, NodeDataSource, got.Factory.Body.Data[0].Kind)
	assert.Equal(t, "existing", got.Factory.Body.Data[0].Name.Name)

	require.Len(t, got.Factory.Body.Actions, 1)
	requireSpan(t, got.Factory.Body.Actions[0].S)
	assert.Equal(t, NodeAction, got.Factory.Body.Actions[0].Kind)
	assert.Equal(t, "run", got.Factory.Body.Actions[0].Name.Name)

	require.Len(t, got.Factory.Body.Outputs, 1)
	requireSpan(t, got.Factory.Body.Outputs[0].S)
	assert.Equal(t, "path", got.Factory.Body.Outputs[0].Name.Name)
}

func TestLowerInputTypeFieldsUseParsedTypes(t *testing.T) {
	f := parseFile(t, "factory.ub", lowerFixture(t, "input-type-fields"), parse.FileFactory)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Len(t, got.Factory.Body.Inputs, 1)

	input := got.Factory.Body.Inputs[0]
	typeField := input.Body.Fields[0]
	require.Equal(t, "type", typeField.Key.Name)
	require.Same(t, input.Type, typeField.Value)

	obj, ok := input.Type.(*parse.TypeObject)
	require.True(t, ok, "got %T", input.Type)
	require.Len(t, obj.Fields, 1)
	nested := obj.Fields[0]
	require.NotNil(t, nested.Decl)
	nestedTypeField := nested.Decl.Fields[0]
	require.Equal(t, "type", nestedTypeField.Key.Name)
	require.IsType(t, &parse.TypeAtomic{}, nestedTypeField.Value)
}

func TestParseSourceUsesTypeParserForInputFields(t *testing.T) {
	src := []byte(lowerFixture(t, "parse-source-type-parser"))

	got, err := ParseSource("factory.ub", src)
	require.NoError(t, err)
	require.Len(t, got.Factory.Body.Inputs, 1)

	typeExpr := got.Factory.Body.Inputs[0].Type
	require.IsType(t, &parse.TypeObject{}, typeExpr)
	assert.Equal(t, 3, typeExpr.Span().Start.Line)
	assert.Equal(t, 22, typeExpr.Span().Start.Column)
}

func TestParseSourceReportsTypeParserErrors(t *testing.T) {
	src := []byte(lowerInvalidFixture(t, "parse-source-type-parser-error"))

	_, err := ParseSource("factory.ub", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `input "bad": unknown atomic type "unknown"`)
	assert.NotContains(t, err.Error(), "rule AtomicType")
}

func TestLowerPreclassifiedStackFileRequiresSourceDeclaration(t *testing.T) {
	f := parseFile(t, "dev.ub", lowerInvalidFixture(t, "preclassified-stack"), parse.FileStack)

	got, errs := LowerFile(f)
	require.NotZero(t, errs.Len())
	require.Equal(t, FileUnknown, got.Kind)
	require.Contains(t, errs.Error(), "cannot determine UB file role from stack")
}

func TestLowerPreclassifiedFactoryFileRequiresSourceDeclaration(t *testing.T) {
	f := parseFile(t, "main.ub", lowerInvalidFixture(t, "preclassified-factory"), parse.FileFactory)

	got, errs := LowerFile(f)
	require.NotZero(t, errs.Len())
	require.Equal(t, FileUnknown, got.Kind)
	require.Contains(t, errs.Error(), "cannot determine UB file role from factory")
}

func TestLowerPreclassifiedProjectFileRequiresSourceDeclaration(t *testing.T) {
	f := parseFile(t, "unobin.project", lowerInvalidFixture(t, "preclassified-project"),
		parse.FileProject)

	got, errs := LowerFile(f)
	require.NotZero(t, errs.Len())
	require.Equal(t, FileUnknown, got.Kind)
	require.Contains(t, errs.Error(), "cannot determine UB file role from project")
}

func TestLowerSourceDeclaredFactoryFile(t *testing.T) {
	f := parseFile(t, "factory.ub", lowerFixture(t, "source-factory"), parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)
	require.Len(t, got.Factory.Body.Resources, 1)
	assert.Equal(t, "hello", got.Factory.Body.Resources[0].Name.Name)
	assert.Equal(t, "std", got.Factory.Body.Resources[0].Selector.Alias.Name)
	assert.Equal(t, "fs-file", got.Factory.Body.Resources[0].Selector.Export.Name)
}

func TestLowerSourceDeclaredStackFile(t *testing.T) {
	f := parseFile(t, "dev.ub", lowerFixture(t, "source-stack"), parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileStack, got.Kind)
	require.NotNil(t, got.Stack)
	require.NotNil(t, got.Stack.Factory)
	require.NotNil(t, got.Stack.Factory.Inputs)
	require.NotNil(t, got.Stack.State)
	assert.Equal(t, "local", got.Stack.State.Selector.Name)
	require.NotNil(t, got.Stack.Encryption)
	assert.Equal(t, "noop", got.Stack.Encryption.Selector.Name)
}

func TestLowerSourceDeclaredProjectFile(t *testing.T) {
	f := parseFile(t, "project.ub", lowerFixture(t, "source-project"), parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileProject, got.Kind)
	require.NotNil(t, got.Project)
	require.NotNil(t, got.Project.UnobinVersion)
	assert.Equal(t, "0.2.0", got.Project.UnobinVersion.Value)
	require.Len(t, got.Project.Requires, 2)
	assert.Equal(t, "github.com/cloudboss/example", got.Project.Requires[0].ID.Value)
	require.NotNil(t, got.Project.Requires[0].Version)
	assert.Equal(t, "v1.2.3", got.Project.Requires[0].Version.Value)
	assert.Nil(t, got.Project.Requires[0].Indirect)
	assert.Equal(t, "github.com/cloudboss/std", got.Project.Requires[1].ID.Value)
	require.NotNil(t, got.Project.Requires[1].Version)
	assert.Equal(t, "v0.2.0", got.Project.Requires[1].Version.Value)
	require.NotNil(t, got.Project.Requires[1].Indirect)
	assert.True(t, got.Project.Requires[1].Indirect.Value)
}

func TestLowerSourceDeclaredProjectLockFile(t *testing.T) {
	f := parseFile(t, "project-lock.ub", lowerFixture(t, "source-project-lock"), parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileProjectLock, got.Kind)
	require.NotNil(t, got.ProjectLock)
	requireSpan(t, got.ProjectLock.S)
	require.NotNil(t, got.ProjectLock.Version)
	assert.Equal(t, int64(1), got.ProjectLock.Version.ParsedInt)
	require.NotNil(t, got.ProjectLock.Toolchain)
	assert.Equal(t, "v0.4.2", got.ProjectLock.Toolchain.UnobinVersion.Value)

	require.Len(t, got.ProjectLock.Deps, 2)
	assert.Equal(t, "github.com/cloudboss/unobin-library-std", got.ProjectLock.Deps[0].ID.Value)
	assert.Equal(t, "go", got.ProjectLock.Deps[0].Kind.Name)
	assert.Equal(t, "v0.1.0", got.ProjectLock.Deps[0].Version.Value)
	assert.Equal(t, "abc123", got.ProjectLock.Deps[0].Commit.Value)
	require.Nil(t, got.ProjectLock.Deps[0].Hash)
	assert.Equal(t, "example.com/ub-lib//network", got.ProjectLock.Deps[1].ID.Value)
	assert.Equal(t, "ub", got.ProjectLock.Deps[1].Kind.Name)
	assert.Equal(t, "sha256:789abc", got.ProjectLock.Deps[1].Hash.Value)
}

func TestLowerSourceDeclaredLibraryFile(t *testing.T) {
	f := parseFile(t, "library.ub", lowerFixture(t, "source-library"), parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileLibrary, got.Kind)
	require.NotNil(t, got.Library)
	require.Len(t, got.Library.Exports, 2)
	assert.Equal(t, NodeResource, got.Library.Exports[0].Kind)
	assert.Equal(t, "greeting", got.Library.Exports[0].Name.Name)
	assert.Equal(t, NodeDataSource, got.Library.Exports[1].Kind)
	assert.Equal(t, "data-source", string(got.Library.Exports[1].Kind))
	assert.Equal(t, "lookup", got.Library.Exports[1].Name.Name)
}

func TestLowerRejectsDataBlock(t *testing.T) {
	f := parseFile(t, "factory.ub", lowerInvalidFixture(t, "factory-data-block"), parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), `"data" is not a valid factory field`)
}

func TestLowerPreclassifiedExportedTypeFileRequiresSourceDeclaration(t *testing.T) {
	f := parseFile(t, "resource-greeting.ub", lowerInvalidFixture(t, "preclassified-exported-type"),
		parse.FileExportedType)

	got, errs := LowerFile(f)
	require.Equal(t, 1, errs.Len(), errs.Error())
	require.Equal(t, FileUnknown, got.Kind)
	assert.Contains(t, errs.Error(), "cannot determine UB file role from exported-type")
}

func TestLowerSelectorBodyFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/valid/selector-body", func(name string, src []byte) (string, []string) {
		kind, path := selectorBodyFixtureKind(name)
		f, err := lang.ParseSource(path, src)
		if err != nil {
			return "", []string{err.Error()}
		}
		f.Kind = kind
		got, errs := LowerFile(f)
		if errs.Len() > 0 {
			return "", errs.Strings()
		}
		return lowerSelectorBodySummary(got), nil
	})
}

func TestLowerInvalidSelectorBodyFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/invalid/selector-body", func(
		name string, src []byte,
	) (string, []string) {
		f, err := lang.ParseSource(invalidSelectorBodyFixturePath(name), src)
		if err != nil {
			return "", []string{err.Error()}
		}
		_, errs := LowerFile(f)
		return "", errs.Messages()
	})
}

func TestLowerDuplicateObjectFieldFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/invalid/duplicate-fields", func(
		name string, src []byte,
	) (string, []string) {
		f, err := lang.ParseSource(duplicateFieldFixturePath(name), src)
		if err != nil {
			return "", []string{err.Error()}
		}
		_, errs := LowerFile(f)
		return "", errs.Messages()
	})
}

func selectorBodyFixtureKind(name string) (parse.FileKind, string) {
	switch name {
	case "factory":
		return parse.FileFactory, "factory.ub"
	case "stack":
		return parse.FileStack, "dev.ub"
	case "library":
		return parse.FileExportedType, "library.ub"
	default:
		return parse.FileUnknown, name + ".ub"
	}
}

func invalidSelectorBodyFixturePath(name string) string {
	switch {
	case strings.HasPrefix(name, "factory-"):
		return "factory.ub"
	case strings.HasPrefix(name, "stack-"):
		return "dev.ub"
	case strings.HasPrefix(name, "library-"):
		return "library.ub"
	default:
		return name + ".ub"
	}
}

func duplicateFieldFixturePath(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return name + ".ub"
	}
	switch parts[0] {
	case "factory":
		return "factory.ub"
	case "stack":
		return "dev.ub"
	case "project":
		return "project.ub"
	case "project-lock":
		return "project-lock.ub"
	case "library":
		return "library.ub"
	default:
		return name + ".ub"
	}
}

func lowerSelectorBodySummary(f *File) string {
	var b strings.Builder
	switch f.Kind {
	case FileFactory:
		writeLibraryConfigDecls(&b, f.Factory.Body.LibraryConfigs)
		writeNodeDecls(&b, f.Factory.Body.Resources)
		writeNodeDecls(&b, f.Factory.Body.Data)
		writeNodeDecls(&b, f.Factory.Body.Actions)
	case FileStack:
		if f.Stack.State != nil {
			fmt.Fprintf(&b, "state -> %s fields=%d\n",
				f.Stack.State.Selector.Name, objectFieldCount(f.Stack.State.Body))
		}
		if f.Stack.Encryption != nil {
			fmt.Fprintf(&b, "encryption -> %s fields=%d\n",
				f.Stack.Encryption.Selector.Name, objectFieldCount(f.Stack.Encryption.Body))
		}
	case FileLibrary:
		for _, export := range f.Library.Exports {
			fmt.Fprintf(&b, "export %s %s outputs=%d\n",
				export.Kind, export.Name.Name, len(export.Body.Outputs))
		}
	}
	return b.String()
}

func writeLibraryConfigDecls(b *strings.Builder, decls []LibraryConfigDecl) {
	for _, decl := range decls {
		fmt.Fprintf(b, "library config %s expr=%s\n", decl.Alias.Name,
			exprSummary(decl.Value))
	}
}

func exprSummary(expr parse.Expr) string {
	switch v := expr.(type) {
	case *parse.DotPath:
		return v.Root.Name
	case *parse.ObjectLit:
		return fmt.Sprintf("object fields=%d", len(v.Fields))
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func writeNodeDecls(b *strings.Builder, nodes []NodeDecl) {
	for _, node := range nodes {
		fmt.Fprintf(b, "%s %s -> %s.%s fields=%d\n",
			node.Kind, node.Name.Name, node.Selector.Alias.Name,
			node.Selector.Export.Name, objectFieldCount(node.Body))
	}
}

func objectFieldCount(obj *parse.ObjectLit) int {
	if obj == nil {
		return 0
	}
	return len(obj.Fields)
}

func TestLowerReportsSchemaErrors(t *testing.T) {
	f := parseFile(t, "factory.ub", lowerInvalidFixture(t, "schema-errors"), parse.FileFactory)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "unknown atomic type")
	assert.Contains(t, errs.Error(), "resource must be written as name: alias.export { ... }")
}

func TestLowerRejectsUnwrappedFactoryFile(t *testing.T) {
	f := parseFile(t, "factory.ub", lowerInvalidFixture(t, "unwrapped-factory"), parse.FileFactory)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "factory.ub must declare factory")
}

func TestLowerReportsUserFacingFileRoleError(t *testing.T) {
	f := parseFile(t, "unknown.ub", lowerInvalidFixture(t, "user-facing-file-role"),
		parse.FileUnknown)

	_, errs := LowerFile(f)
	require.Equal(t, 1, errs.Len())
	got := errs.Error()
	assert.Contains(t, got, "cannot determine UB file role")
	assert.NotContains(t, got, "lower")
}

func TestLowerReportsMixedSourceDeclaredFileRoles(t *testing.T) {
	f := parseFile(t, "mixed.ub", lowerInvalidFixture(t, "mixed-file-roles"), parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "file must not declare both factory and stack")
}

func TestLowerReportsReservedFilenameMismatch(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		fixture string
		want    string
	}{
		{
			name:    "factory file without factory declaration",
			path:    "factory.ub",
			fixture: "reserved-factory-missing-declaration",
			want:    "factory.ub must declare factory",
		},
		{
			name:    "project file with factory declaration",
			path:    "project.ub",
			fixture: "reserved-project-with-factory",
			want:    "project.ub must declare project",
		},
		{
			name:    "project-lock file with project declaration",
			path:    "project-lock.ub",
			fixture: "reserved-project-lock-with-project",
			want:    "project-lock.ub must declare project-lock",
		},
		{
			name:    "factory declaration outside factory file",
			path:    "app.ub",
			fixture: "reserved-factory-outside-factory",
			want:    "factory declaration must be in factory.ub",
		},
		{
			name:    "project declaration outside project file",
			path:    "app.ub",
			fixture: "reserved-project-outside-project",
			want:    "project declaration must be in project.ub",
		},
		{
			name:    "project-lock declaration outside project-lock file",
			path:    "app.ub",
			fixture: "reserved-project-lock-outside-project-lock",
			want:    "project-lock declaration must be in project-lock.ub",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := lowerInvalidFixture(t, c.fixture)
			f := parseFile(t, c.path, src, parse.FileUnknown)

			_, errs := LowerFile(f)
			require.NotEqual(t, 0, errs.Len())
			assert.Contains(t, errs.Error(), c.want)
		})
	}
}

func TestLowerReportsProjectRequireSchemaErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "string value",
			src:  "'github.com/x/y': 'v1.2.3'",
			want: "requires: dependency \"github.com/x/y\": value must be an object",
		},
		{
			name: "missing version",
			src:  "'github.com/x/y': { indirect: true }",
			want: "requires: dependency \"github.com/x/y\": missing version",
		},
		{
			name: "non string version",
			src:  "'github.com/x/y': { version: 12 }",
			want: "require github.com/x/y: version must be a string literal",
		},
		{
			name: "non boolean indirect",
			src:  "'github.com/x/y': { version: 'v1.2.3' indirect: 'yes' }",
			want: "require github.com/x/y: indirect must be a boolean literal",
		},
		{
			name: "unknown field",
			src:  "'github.com/x/y': { version: 'v1.2.3' pinned: true }",
			want: "requires: dependency \"github.com/x/y\": \"pinned\" is not a valid require field",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := "project" + ": { requires: { " + c.src + " } }\n"
			f := parseFile(t, "project.ub", body, parse.FileUnknown)

			_, errs := LowerFile(f)
			require.NotEqual(t, 0, errs.Len())
			assert.Contains(t, errs.Error(), c.want)
		})
	}
}

func TestLowerReportsProjectLockSchemaErrors(t *testing.T) {
	f := parseFile(t, "project-lock.ub", lowerInvalidFixture(t, "project-lock-schema-errors"), parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	got := errs.Error()
	assert.Contains(t, got, "project-lock version must be an integer")
	assert.Contains(t, got, "project-lock: missing toolchain")
	assert.Contains(t, got, "project-lock dependency github.com/cloudboss/example: ub kind requires hash")
	assert.Contains(t, got, "project-lock dependency github.com/cloudboss/example-go: go kind forbids hash")
	assert.Contains(t, got, "project-lock dependency github.com/cloudboss/example-bad: unknown kind")
	assert.Contains(t, got, "project-lock dependency github.com/cloudboss/example-bad: missing version")
}

func TestLowerReportsProjectLockToolchainSchemaErrors(t *testing.T) {
	f := parseFile(t, "project-lock.ub", lowerInvalidFixture(t, "project-lock-toolchain-schema-errors"),
		parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "project-lock toolchain: missing unobin-version")
}
