package lang

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func parseWithKind(t *testing.T, src string, kind FileKind) *File {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	f.Kind = kind
	return f
}

// topLevelDriver checks a fixture's top-level keys as the given file kind.
func topLevelDriver(kind FileKind) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		f.Kind = kind
		return "", ValidateTopLevelKeys(f).Messages()
	}
}

func TestValidateTopLevelKeysFactoryFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/toplevel/factory", topLevelDriver(FileFactory))
}

func TestValidateTopLevelKeysConfigFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/toplevel/config", topLevelDriver(FileConfig))
}

func TestValidateTopLevelKeysUnknownFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/toplevel/unknown", topLevelDriver(FileUnknown))
}

// fileDriver runs ValidateFile over a whole fixture parsed as the given kind,
// reporting positioned diagnostics so the goldens pin file:line:col.
func fileDriver(kind FileKind) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("factory.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		f.Kind = kind
		return "", ValidateFile(f).Strings()
	}
}

func TestValidateFileFactoryFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/file/factory", fileDriver(FileFactory))
}

func TestValidateFileExportedTypeFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/file/exported-type", fileDriver(FileExportedType))
}

func TestValidateFileUnknownFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/file/unknown", fileDriver(FileUnknown))
}

func TestValidateCallsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/calls", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("factory.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		if inputs := TopLevelBlock(f, "inputs"); inputs != nil {
			errs := ValidateInputDeclarations(inputs)
			if errs.Len() > 0 {
				return "", errs.Strings()
			}
		}
		return "", ValidateCalls(f).Strings()
	})
}

func TestValidateComprehensionBindingsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/comprehensions", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("factory.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", ValidateComprehensionBindings(f).Strings()
	})
}

func configDriver(name string, src []byte) (string, []string) {
	f, err := ParseSource("config.ub", src)
	if err != nil {
		return "", []string{err.Error()}
	}
	f.Kind = FileConfig
	return "", ValidateFile(f).Strings()
}

func TestValidateConfigFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/config", configDriver)
}

func parseInputsBlock(t *testing.T, src string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, "inputs", f.Body.Fields[0].Key.Name)
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	require.True(t, ok, "expected `inputs:` to be an object literal")
	return o
}

func TestValidateInputBadType(t *testing.T) {
	src := `
inputs: {
  bad: { type: list(weird-thing) }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Equal(t, ErrType, errs.Errors()[0].Kind)
}

func TestValidateInputDeclarationsStoresParsedTypes(t *testing.T) {
	block := parseInputsBlock(t, `
inputs: {
  cfg: { type: object({ port: { type: integer, default: 8080 } }) }
}
`)

	errs := ValidateInputDeclarations(block)
	require.Equal(t, 0, errs.Len(), errs.Error())

	decl := block.Fields[0].Value.(*ObjectLit)
	typeField := decl.Fields[0]
	require.IsType(t, &TypeObject{}, typeField.Value)

	obj := typeField.Value.(*TypeObject)
	require.Len(t, obj.Fields, 1)
	nested := obj.Fields[0]
	require.NotNil(t, nested.Decl)
	nestedTypeField := nested.Decl.Fields[0]
	require.IsType(t, &TypeAtomic{}, nestedTypeField.Value)
}

func TestValidateInputDeclarationsUsesTypeParserSpans(t *testing.T) {
	block := parseInputsBlock(t, `
inputs: {
  payload: { type: open(object({ kind: string })) }
}
`)

	errs := ValidateInputDeclarations(block)
	require.Equal(t, 0, errs.Len(), errs.Error())

	decl := block.Fields[0].Value.(*ObjectLit)
	typeField := decl.Fields[0]
	require.IsType(t, &TypeObject{}, typeField.Value)
	assert.Equal(t, 3, typeField.Value.Span().Start.Line)
	assert.Equal(t, 20, typeField.Value.Span().Start.Column)
}

func constraintsBlock(f *File) (*ArrayLit, bool) {
	if len(f.Body.Fields) == 0 || f.Body.Fields[0].Key.Name != "constraints" {
		return nil, false
	}
	a, ok := f.Body.Fields[0].Value.(*ArrayLit)
	return a, ok
}

func parseConstraintsBlock(t *testing.T, src string) *ArrayLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	block, ok := constraintsBlock(f)
	require.True(t, ok, "expected `constraints:` to be an array literal")
	return block
}

func TestValidateConstraintsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/constraints", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		block, ok := constraintsBlock(f)
		if !ok {
			return "", []string{"fixture must begin with a constraints: array"}
		}
		return "", ValidateConstraints(block).Messages()
	})
}

func namedObjectBlock(f *File, key string) (*ObjectLit, bool) {
	if len(f.Body.Fields) == 0 || f.Body.Fields[0].Key.Name != key {
		return nil, false
	}
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	return o, ok
}

func parseObjectBlock(t *testing.T, src, key string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	o, ok := namedObjectBlock(f, key)
	require.True(t, ok, "expected `%s:` to be an object literal", key)
	return o
}

// objectBlockDriver runs the named top-level object block through validate,
// reporting its diagnostics as the fixture's expected errors.
func objectBlockDriver(key string, validate func(*ObjectLit) *ErrorList) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		o, ok := namedObjectBlock(f, key)
		if !ok {
			return "", []string{"fixture must begin with a " + key + ": object"}
		}
		return "", validate(o).Messages()
	}
}

func TestValidateImportsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/imports", objectBlockDriver("imports", ValidateImports))
}

func TestValidateOutputsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/outputs", objectBlockDriver("outputs", ValidateOutputs))
}

func TestValidateLocalsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/locals", objectBlockDriver("locals", ValidateLocals))
}

func TestValidateResourcesFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/resources", objectBlockDriver("resources", ValidateResources))
}

func TestValidateDataSourcesFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/data", objectBlockDriver("data", ValidateDataSources))
}

func TestValidateActionsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/actions", objectBlockDriver("actions", ValidateActions))
}

func TestValidateInputsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/inputs", objectBlockDriver("inputs", ValidateInputDeclarations))
}

func TestValidateFactoryConfigurationsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/configurations",
		objectBlockDriver("configurations", ValidateFactoryConfigurations))
}

// TestValidateConstraintReferencesFixtures checks that constraint fields
// resolve against the declared inputs of the same fixture.
func TestValidateConstraintReferencesFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/constraint-refs", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		var inputs *ObjectLit
		var constraints *ArrayLit
		for _, fld := range f.Body.Fields {
			switch fld.Key.Name {
			case "inputs":
				inputs, _ = fld.Value.(*ObjectLit)
			case "constraints":
				constraints, _ = fld.Value.(*ArrayLit)
			}
		}
		if inputs == nil || constraints == nil {
			return "", []string{"fixture needs inputs: and constraints: blocks"}
		}
		return "", ValidateConstraintReferences(constraints, inputs).Strings()
	})
}

// TestValidateBodyMetaKeysFixtures checks the meta keys allowed in a resource,
// data, or action body. The fixture's first key picks which block to validate.
func TestValidateBodyMetaKeysFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/body-meta", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		if len(f.Body.Fields) == 0 {
			return "", []string{"fixture needs a resources, data, or actions block"}
		}
		fld := f.Body.Fields[0]
		block, ok := fld.Value.(*ObjectLit)
		if !ok {
			return "", []string{"block must be an object"}
		}
		switch fld.Key.Name {
		case "resources":
			return "", ValidateResources(block).Messages()
		case "data":
			return "", ValidateDataSources(block).Messages()
		case "actions":
			return "", ValidateActions(block).Messages()
		}
		return "", []string{"unknown block " + fld.Key.Name}
	})
}
